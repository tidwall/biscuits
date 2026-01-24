// https://github.com/tidwall/biscuits
//
// Copyright 2026 Joshua J Baker. All rights reserved.
package biscuits

import (
	"errors"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/cespare/xxhash/v2"
)

const validateState = true // validate the state of the structure
const maxItems = 32        // max items per leaf before splitting
const nnodes = 16          // (nnodes, hshift) work together and must be one of
const hshift = 2           // the following: (2, 1) or (16, 2) or (256, 3)

var (
	ErrNotCovered = errors.New("key not covered")
	ErrTxEnded    = errors.New("transaction ended")
	ErrNotFound   = errors.New("not found")
)

const (
	// first 2 bits are the node kind
	kindLeaf      = 0
	kindBranch    = 1
	kindLeafSplit = 2
	// copy on write flags
	kindCloned       = 8  // bit 3
	kindClonedLocked = 16 // bit 4
)

type Tx[T any] struct {
	hashes []uint64
	ended  bool
	m      *Map[T]
}

type item[T any] struct {
	hash  uint64
	key   string
	value T
}

type leafNode[T any] struct {
	items []item[T]
}

type state[T any] struct {
	kind atomic.Int32
	tx   *Tx[T]
	lock sync.Mutex
}

type branchNode[T any] struct {
	states [nnodes]state[T]
	nodes  [nnodes]unsafe.Pointer // either *branchNode[T] or *leafNode[T]
}

type Map[T any] struct {
	root branchNode[T]
}

func hashstr(str string) uint64 {
	return xxhash.Sum64String(str)
}

func (b *branchNode[T]) cow(i int) {
	// clone bit flag set
	kind := b.states[i].kind.Load()
	if kind < 4 {
		return
	}
	if kind&kindClonedLocked == kindClonedLocked {
		// Already in the process of being cloned
		return
	}
	if validateState {
		if kind&kindCloned != kindCloned {
			panic("invalid state")
		}
	}
	if !b.states[i].kind.CompareAndSwap(kind, kind|kindClonedLocked) {
		return
	}
	kind = kind & 3
	if kind == kindBranch {
		b1 := (*branchNode[T])(b.nodes[i])
		b2 := new(branchNode[T])
		b.nodes[i] = unsafe.Pointer(b2)
		for i := range b1.nodes {
			kind := b1.states[i].kind.Load()
			b2.states[i].kind.Store(kind | kindCloned)
			b2.nodes[i] = b1.nodes[i]
		}
	} else {
		l1 := (*leafNode[T])(b.nodes[i])
		if l1 != nil {
			l2 := new(leafNode[T])
			l2.items = append(l2.items, l1.items...)
			b.nodes[i] = unsafe.Pointer(l2)
		}
	}
	b.states[i].kind.Store(kind)
}

func (b *branchNode[T]) lock(hash uint64, tx *Tx[T], depth int) {
	i := (hash >> (depth << hshift)) & (nnodes - 1)
	for {
		kind := b.states[i].kind.Load()
		if kind >= 4 {
			b.cow(int(i))
			runtime.Gosched()
			continue
		}
		if kind == kindBranch {
			(*branchNode[T])(b.nodes[i]).lock(hash, tx, depth+1)
			break
		}
		b.states[i].lock.Lock()
		kind = b.states[i].kind.Load()
		if kind == kindLeaf {
			if validateState {
				if b.states[i].tx != nil {
					panic("invalid state")
				}
				b.states[i].tx = tx
			}
			break
		}
		if validateState {
			if kind == kindLeafSplit {
				panic("invalid state")
			}
		}
		b.states[i].lock.Unlock()
	}
}

func (b *branchNode[T]) unlock(hash uint64, tx *Tx[T], depth int) {
	i := (hash >> (depth << hshift)) & (nnodes - 1)
	kind := b.states[i].kind.Load()
	if kind == kindBranch {
		(*branchNode[T])(b.nodes[i]).unlock(hash, tx, depth+1)
		return
	}
	if validateState {
		if b.states[i].tx != tx {
			panic("invalid state")
		}
	}
	if kind == kindLeafSplit {
		// Leaf was converted to branch due to a split.
		// Switch to a branch before unlocking
		b.states[i].kind.Store(kindBranch)
	}
	if validateState {
		b.states[i].tx = nil
	}
	b.states[i].lock.Unlock()
}

func (b *branchNode[T]) setAfterSplit(depth int, leaf *leafNode[T]) {
	for j := 0; j < len(leaf.items); j++ {
		item := leaf.items[j]
		i := (item.hash >> (depth << hshift)) & (nnodes - 1)
		leaf := (*leafNode[T])(b.nodes[i])
		if leaf == nil {
			leaf = new(leafNode[T])
			b.nodes[i] = unsafe.Pointer(leaf)
		}
		leaf.items = append(leaf.items, item)
	}
}

func (b *branchNode[T]) set(item item[T], tx *Tx[T], split bool, depth int,
) (old T, replaced bool) {
	i := (item.hash >> (depth << hshift)) & (nnodes - 1)
	kind := b.states[i].kind.Load()
	if kind == kindBranch || kind == kindLeafSplit {
		var split2 bool
		if kind == kindLeafSplit {
			if validateState {
				if b.states[i].tx != tx {
					panic("invalid state")
				}
				if b.states[i].lock.TryLock() {
					panic("invalid state")
				}
			}
			split2 = true
		}
		child := (*branchNode[T])(b.nodes[i])
		return child.set(item, tx, split2, depth+1)
	}
	if validateState {
		if b.states[i].tx != tx {
			panic("invalid state")
		}
		if b.states[i].lock.TryLock() {
			panic("invalid state")
		}
	}
	leaf := (*leafNode[T])(b.nodes[i])
	if leaf == nil {
		leaf = new(leafNode[T])
		b.nodes[i] = unsafe.Pointer(leaf)
	}
	for i := 0; i < len(leaf.items); i++ {
		if leaf.items[i].hash == item.hash && leaf.items[i].key == item.key {
			old = leaf.items[i].value
			leaf.items[i].value = item.value
			return old, true
		}
	}
	leaf.items = append(leaf.items, item)
	if !split && len(leaf.items) >= maxItems {
		// Split leaf. Convert to branch
		branch2 := new(branchNode[T])
		b.states[i].kind.Store(kindLeafSplit)
		b.nodes[i] = unsafe.Pointer(branch2)
		branch2.setAfterSplit(depth+1, leaf)
	}
	return old, false
}

func (b *branchNode[T]) get(hash uint64, key string, tx *Tx[T], depth int,
) (value T, replaced bool) {
	i := (hash >> (depth << hshift)) & (nnodes - 1)
	kind := b.states[i].kind.Load()
	if kind == kindBranch || kind == kindLeafSplit {
		if kind == kindLeafSplit {
			if validateState {
				if b.states[i].tx != tx {
					panic("invalid state")
				}
				if b.states[i].lock.TryLock() {
					panic("invalid state")
				}
			}
		}
		child := (*branchNode[T])(b.nodes[i])
		return child.get(hash, key, tx, depth+1)
	}
	if validateState {
		if b.states[i].tx != tx {
			panic("invalid state")
		}
		if b.states[i].lock.TryLock() {
			panic("invalid state")
		}
	}
	leaf := (*leafNode[T])(b.nodes[i])
	if leaf != nil {
		for i := 0; i < len(leaf.items); i++ {
			if leaf.items[i].hash == hash && leaf.items[i].key == key {
				return leaf.items[i].value, true
			}
		}
	}
	return value, false
}

func (b *branchNode[T]) delete(hash uint64, key string, tx *Tx[T], depth int,
) (value T, deleted bool) {
	i := (hash >> (depth << hshift)) & (nnodes - 1)
	kind := b.states[i].kind.Load()
	if kind == kindBranch || kind == kindLeafSplit {
		if kind == kindLeafSplit {
			if validateState {
				if b.states[i].tx != tx {
					panic("invalid state")
				}
				if b.states[i].lock.TryLock() {
					panic("invalid state")
				}
			}
		}
		child := (*branchNode[T])(b.nodes[i])
		return child.delete(hash, key, tx, depth+1)
	}
	if validateState {
		if b.states[i].tx != tx {
			panic("invalid state")
		}
		if b.states[i].lock.TryLock() {
			panic("invalid state")
		}
	}
	leaf := (*leafNode[T])(b.nodes[i])
	if leaf == nil {
		return value, false
	}
	for j := 0; j < len(leaf.items); j++ {
		if leaf.items[j].hash == hash && leaf.items[j].key == key {
			var empty item[T]
			value = leaf.items[j].value
			leaf.items[j] = leaf.items[len(leaf.items)-1]
			leaf.items[len(leaf.items)-1] = empty
			leaf.items = leaf.items[:len(leaf.items)-1]
			if len(leaf.items) == 0 {
				b.nodes[i] = nil
			}
			return value, true
		}
	}
	return value, false
}

func (tx *Tx[T]) validate(hash uint64) error {
	if tx.ended {
		return ErrTxEnded
	}
	if !slices.Contains(tx.hashes, hash) {
		return ErrNotCovered
	}
	return nil
}

func (m *Map[T]) Begin(keys ...string) *Tx[T] {
	tx := &Tx[T]{m: m, hashes: make([]uint64, len(keys))}
	for i, key := range keys {
		tx.hashes[i] = hashstr(key)
	}
	slices.Sort(tx.hashes)
	for _, hash := range tx.hashes {
		tx.m.root.lock(hash, tx, 0)
	}
	return tx
}

func (tx *Tx[T]) Set(key string, value T) (old T, replaced bool, err error) {
	hash := hashstr(key)
	if err := tx.validate(hash); err != nil {
		return old, false, err
	}
	old, replaced = tx.m.root.set(item[T]{hash, key, value}, tx, false, 0)
	return old, replaced, nil
}

func (tx *Tx[T]) Get(key string) (value T, found bool, err error) {
	hash := hashstr(key)
	if err := tx.validate(hash); err != nil {
		return value, false, err
	}
	value, found = tx.m.root.get(hash, key, tx, 0)
	return value, found, nil
}

func (tx *Tx[T]) Delete(key string) (value T, deleted bool, err error) {
	hash := hashstr(key)
	if err := tx.validate(hash); err != nil {
		return value, false, err
	}
	value, deleted = tx.m.root.delete(hash, key, tx, 0)
	return value, deleted, nil
}

func (tx *Tx[T]) End() error {
	if tx.ended {
		return ErrTxEnded
	}
	for _, hash := range tx.hashes {
		tx.m.root.unlock(hash, tx, 0)
	}
	tx.ended = true
	return nil
}

// Clone of the map.
// This is an O(1) Copy-on-write.
// WARNING: This operation requires exclusive access to the map. Do not call
// while other transactions are sharing the same map.
// It's your responsibility to manage access using a lock, such as with a
// sync.RWLock.
func (m *Map[T]) Clone() *Map[T] {
	m2 := new(Map[T])
	for i := range m.root.nodes {
		kind := m.root.states[i].kind.Load()
		m.root.states[i].kind.Store(kind | kindCloned)
		m2.root.states[i].kind.Store(kind | kindCloned)
		m2.root.nodes[i] = m.root.nodes[i]
	}
	return m2
}

func (b *branchNode[T]) scan(iter func(key string, value T) bool) bool {
	for i := range b.nodes {
		kind := b.states[i].kind.Load() & 3
		if kind == kindBranch {
			if !(*branchNode[T])(b.nodes[i]).scan(iter) {
				return false
			}
		} else {
			if validateState {
				if kind != kindLeaf {
					panic("invalid state")
				}
			}
			leaf := (*leafNode[T])(b.nodes[i])
			if leaf != nil {
				for i := range leaf.items {
					if !iter(leaf.items[i].key, leaf.items[i].value) {
						return false
					}
				}
			}
		}
	}
	return true
}

// Scan the map, iterating over all keys and values.
// WARNING: This operation requires exclusive access to the map. Do not call
// while other transactions are sharing the same map.
// It's your responsibility to manage access using a lock, such as with a
// sync.RWLock.
func (m *Map[T]) Scan(iter func(key string, value T) bool) {
	m.root.scan(iter)
}
