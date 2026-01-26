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

type keytype interface {
	~string | ~uint64 | ~int64 | ~int
}

const (
	// first 2 bits are the node kind
	kindLeaf      = 0
	kindBranch    = 1
	kindLeafSplit = 2
	// copy on write flags
	kindCloned       = 8  // bit 3
	kindClonedLocked = 16 // bit 4
)

type Tx[K keytype, V any] struct {
	hashes []uint64
	ended  bool
	m      *Map[K, V]
}

type item[K keytype, V any] struct {
	hash  uint64
	key   K
	value V
}

type leafNode[K keytype, V any] struct {
	items []item[K, V]
}

type state[K keytype, V any] struct {
	kind atomic.Int32
	tx   *Tx[K, V]
	lock sync.Mutex
}

type branchNode[K keytype, V any] struct {
	states [nnodes]state[K, V]
	nodes  [nnodes]unsafe.Pointer // either *branchNode[K, V] or *leafNode[K, V]
}

type Map[K keytype, V any] struct {
	root branchNode[K, V]
}

func hashkey[K keytype](key K) uint64 {
	var num uint64
	switch key := any(key).(type) {
	case string:
		return xxhash.Sum64String(key)
	case int64:
		num = uint64(key)
	case uint64:
		num = uint64(key)
	case int:
		num = uint64(key)
	case uint:
		num = uint64(key)
	}
	return xxhash.Sum64(unsafe.Slice((*byte)(unsafe.Pointer(&num)), 8))
}

func (b *branchNode[K, V]) cow(i int) {
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
		b1 := (*branchNode[K, V])(b.nodes[i])
		b2 := new(branchNode[K, V])
		b.nodes[i] = unsafe.Pointer(b2)
		for i := range b1.nodes {
			kind := b1.states[i].kind.Load()
			b2.states[i].kind.Store(kind | kindCloned)
			b2.nodes[i] = b1.nodes[i]
		}
	} else {
		l1 := (*leafNode[K, V])(b.nodes[i])
		if l1 != nil {
			l2 := new(leafNode[K, V])
			l2.items = append(l2.items, l1.items...)
			b.nodes[i] = unsafe.Pointer(l2)
		}
	}
	b.states[i].kind.Store(kind)
}

func (b *branchNode[K, V]) lock(hash uint64, tx *Tx[K, V], depth int) {
	i := (hash >> (depth << hshift)) & (nnodes - 1)
	for {
		kind := b.states[i].kind.Load()
		if kind >= 4 {
			b.cow(int(i))
			runtime.Gosched()
			continue
		}
		if kind == kindBranch {
			(*branchNode[K, V])(b.nodes[i]).lock(hash, tx, depth+1)
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

func (b *branchNode[K, V]) unlock(hash uint64, tx *Tx[K, V], depth int) {
	i := (hash >> (depth << hshift)) & (nnodes - 1)
	kind := b.states[i].kind.Load()
	if kind == kindBranch {
		(*branchNode[K, V])(b.nodes[i]).unlock(hash, tx, depth+1)
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

func (b *branchNode[K, V]) setAfterSplit(depth int, leaf *leafNode[K, V]) {
	for j := 0; j < len(leaf.items); j++ {
		item := leaf.items[j]
		i := (item.hash >> (depth << hshift)) & (nnodes - 1)
		leaf := (*leafNode[K, V])(b.nodes[i])
		if leaf == nil {
			leaf = new(leafNode[K, V])
			b.nodes[i] = unsafe.Pointer(leaf)
		}
		leaf.items = append(leaf.items, item)
	}
}

func (b *branchNode[K, V]) set(item item[K, V], tx *Tx[K, V], split bool,
	depth int,
) (old V, replaced bool) {
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
		child := (*branchNode[K, V])(b.nodes[i])
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
	leaf := (*leafNode[K, V])(b.nodes[i])
	if leaf == nil {
		leaf = new(leafNode[K, V])
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
		branch2 := new(branchNode[K, V])
		b.states[i].kind.Store(kindLeafSplit)
		b.nodes[i] = unsafe.Pointer(branch2)
		branch2.setAfterSplit(depth+1, leaf)
	}
	return old, false
}

func (b *branchNode[K, V]) get(hash uint64, key K, tx *Tx[K, V], depth int,
) (value V, replaced bool) {
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
		child := (*branchNode[K, V])(b.nodes[i])
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
	leaf := (*leafNode[K, V])(b.nodes[i])
	if leaf != nil {
		for i := 0; i < len(leaf.items); i++ {
			if leaf.items[i].hash == hash && leaf.items[i].key == key {
				return leaf.items[i].value, true
			}
		}
	}
	return value, false
}

func (b *branchNode[K, V]) delete(hash uint64, key K, tx *Tx[K, V], depth int,
) (value V, deleted bool) {
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
		child := (*branchNode[K, V])(b.nodes[i])
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
	leaf := (*leafNode[K, V])(b.nodes[i])
	if leaf == nil {
		return value, false
	}
	for j := 0; j < len(leaf.items); j++ {
		if leaf.items[j].hash == hash && leaf.items[j].key == key {
			var empty item[K, V]
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

func (tx *Tx[K, V]) validate(hash uint64) error {
	if tx.ended {
		return ErrTxEnded
	}
	if !slices.Contains(tx.hashes, hash) {
		return ErrNotCovered
	}
	return nil
}

func (m *Map[K, V]) Begin(keys ...K) *Tx[K, V] {
	tx := &Tx[K, V]{m: m, hashes: make([]uint64, len(keys))}
	for i, key := range keys {
		tx.hashes[i] = hashkey(key)
	}
	slices.Sort(tx.hashes)
	for _, hash := range tx.hashes {
		tx.m.root.lock(hash, tx, 0)
	}
	return tx
}

func (tx *Tx[K, V]) Set(key K, value V) (old V, replaced bool, err error) {
	hash := hashkey(key)
	if err := tx.validate(hash); err != nil {
		return old, false, err
	}
	old, replaced = tx.m.root.set(item[K, V]{hash, key, value}, tx, false, 0)
	return old, replaced, nil
}

func (tx *Tx[K, V]) Get(key K) (value V, found bool, err error) {
	hash := hashkey(key)
	if err := tx.validate(hash); err != nil {
		return value, false, err
	}
	value, found = tx.m.root.get(hash, key, tx, 0)
	return value, found, nil
}

func (tx *Tx[K, V]) Delete(key K) (value V, deleted bool, err error) {
	hash := hashkey(key)
	if err := tx.validate(hash); err != nil {
		return value, false, err
	}
	value, deleted = tx.m.root.delete(hash, key, tx, 0)
	return value, deleted, nil
}

func (tx *Tx[K, V]) End() error {
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
func (m *Map[K, V]) Clone() *Map[K, V] {
	m2 := new(Map[K, V])
	for i := range m.root.nodes {
		kind := m.root.states[i].kind.Load()
		m.root.states[i].kind.Store(kind | kindCloned)
		m2.root.states[i].kind.Store(kind | kindCloned)
		m2.root.nodes[i] = m.root.nodes[i]
	}
	return m2
}

func (b *branchNode[K, V]) scan(iter func(key K, value V) bool) bool {
	for i := range b.nodes {
		kind := b.states[i].kind.Load() & 3
		if kind == kindBranch {
			if !(*branchNode[K, V])(b.nodes[i]).scan(iter) {
				return false
			}
		} else {
			if validateState {
				if kind != kindLeaf {
					panic("invalid state")
				}
			}
			leaf := (*leafNode[K, V])(b.nodes[i])
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
func (m *Map[K, V]) Scan(iter func(key K, value V) bool) {
	m.root.scan(iter)
}
