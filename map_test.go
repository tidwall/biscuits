// https://github.com/tidwall/biscuits
//
// Copyright 2026 Joshua J Baker. All rights reserved.
package biscuits

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/lotsa"
)

func TestMapStringKeys(t *testing.T) {
	println("== TEST string keys ==")
	N := 1000000
	T := runtime.GOMAXPROCS(0)
	var m Map[string, int]
	m.validate = true
	lotsa.Output = os.Stdout
	print("set    ")
	lotsa.Ops(N, T, func(i, t int) {
		key := strconv.Itoa(i)
		tx := m.Begin(key)
		_, replaced, _ := tx.Set(key, i)
		tx.End()
		if replaced {
			panic("!bad news")
		}
	})
	m.sane(false)
	print("get    ")
	lotsa.Ops(N, T, func(i, t int) {
		key := strconv.Itoa(i)
		tx := m.Begin(key)
		value, ok, _ := tx.Get(key)
		tx.End()
		if !ok || value != i {
			panic("!bad news")
		}
	})
	m.sane(false)
	print("delete ")
	lotsa.Ops(N, T, func(i, t int) {
		// fmt.Printf("== DELETE %d ==\n", i)
		key := strconv.Itoa(i)
		tx := m.Begin(key)
		value, deleted, err := tx.Delete(key)
		tx.End()
		if !deleted || value != i {
			fmt.Printf("BAD NEWS %v %v %v %v\n", i, value, deleted, err)
			panic("!bad news")
		}
	})
	m.sane(false)
	for i := 0; i < N; i++ {
		key := strconv.Itoa(i)
		tx := m.Begin(key)
		_, ok, _ := tx.Get(key)
		tx.End()
		if ok {
			panic("!bad news")
		}
	}
	m.sane(false)
}

func TestMapIntKeys(t *testing.T) {
	println("== TEST int keys ==")
	N := 1000000
	T := runtime.GOMAXPROCS(0)
	var m Map[int, int]
	m.validate = true
	lotsa.Output = os.Stdout
	print("set    ")
	lotsa.Ops(N, T, func(i, t int) {
		key := i
		tx := m.Begin(key)
		_, replaced, _ := tx.Set(key, i)
		tx.End()
		if replaced {
			panic("!bad news")
		}
	})
	m.sane(false)
	print("get    ")
	lotsa.Ops(N, T, func(i, t int) {
		key := i
		tx := m.Begin(key)
		value, ok, _ := tx.Get(key)
		tx.End()
		if !ok || value != i {
			panic("!bad news")
		}
	})
	m.sane(false)
	print("delete ")
	lotsa.Ops(N, T, func(i, t int) {
		// fmt.Printf("== DELETE %d ==\n", i)
		key := i
		tx := m.Begin(key)
		value, deleted, err := tx.Delete(key)
		tx.End()
		if !deleted || value != i {
			fmt.Printf("BAD NEWS %v %v %v %v\n", i, value, deleted, err)
			panic("!bad news")
		}
	})
	m.sane(false)
	for i := 0; i < N; i++ {
		key := i
		tx := m.Begin(key)
		_, ok, _ := tx.Get(key)
		tx.End()
		if ok {
			panic("!bad news")
		}
	}
	m.sane(false)
}

func TestClone(t *testing.T) {
	N := 1000000
	T := runtime.GOMAXPROCS(0)
	var m1 Map[string, int]
	m1.validate = true
	lotsa.Output = nil
	lotsa.Ops(N/2, T, func(i, t int) {
		key := strconv.Itoa(i)
		tx := m1.Begin(key)
		old, replaced, err := tx.Set(key, i)
		tx.End()
		if err != nil {
			panic(err)
		}
		if replaced {
			panic("replaced")
		}
		if old != 0 {
			panic("bad value")
		}
	})
	m1.sane(false)
	m2 := m1.Clone()
	m1.sane(false)
	lotsa.Ops(N/2, T, func(i, t int) {
		i += N / 2
		key := strconv.Itoa(i)
		tx := m1.Begin(key)
		tx.Set(key, i)
		tx.End()
		key = strconv.Itoa(i)
		tx = m2.Begin(key)
		tx.Set(key, -i)
		tx.End()
	})
	lotsa.Ops(N/2, T, func(i, t int) {
		key := strconv.Itoa(i)
		tx := m1.Begin(key)
		value, ok, err := tx.Get(key)
		tx.End()
		if err != nil {
			panic(err)
		}
		if !ok {
			panic("!ok")
		}
		if value != i {
			panic("!mismatch")
		}
		tx = m2.Begin(key)
		value, ok, err = tx.Get(key)
		tx.End()
		if err != nil {
			panic(err)
		}
		if !ok {
			panic("!ok")
		}
		if value != i {
			panic("!mismatch")
		}
	})
	lotsa.Ops(N/2, T, func(i, t int) {
		i += N / 2
		key := strconv.Itoa(i)
		tx := m1.Begin(key)
		value, ok, err := tx.Get(key)
		tx.End()
		if err != nil {
			panic(err)
		}
		if !ok {
			panic("!ok")
		}
		if value != i {
			panic("!mismatch")
		}
		tx = m2.Begin(key)
		value, ok, err = tx.Get(key)
		tx.End()
		if err != nil {
			panic(err)
		}
		if !ok {
			panic("!ok")
		}
		if value != -i {
			panic("!mismatch")
		}
	})
}

func testPerfSyncMapStringKeys(N, T int) {
	keys := make([]string, N)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	var m sync.Map
	lotsa.Output = os.Stdout
	print("insert ")
	lotsa.Ops(N, T, func(i, t int) {
		m.Store(keys[i], i)
	})
	m.Range(func(key, value any) bool {
		return true
	})

}
func testPerfBiscuitsMapStringKeys(N, T int) {
	keys := make([]string, N)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	var m Map[string, int]
	lotsa.Output = os.Stdout
	print("insert ")
	lotsa.Ops(N, T, func(i, t int) {
		tx := m.Begin(keys[i])
		tx.Set(keys[i], i)
		tx.End()
	})
	m.Scan(func(key string, value int) bool {
		return true
	})

}

func testPerfSyncMapIntKeys(N, T int) {
	keys := make([]int, N)
	for i := range keys {
		keys[i] = i
	}
	var m sync.Map
	lotsa.Output = os.Stdout
	print("insert ")
	lotsa.Ops(N, T, func(i, t int) {
		m.Store(keys[i], i)
	})
	m.Range(func(key, value any) bool {
		return true
	})

}
func testPerfBiscuitsMapIntKeys(N, T int) {
	keys := make([]int, N)
	for i := range keys {
		keys[i] = i

	}
	var m Map[int, int]
	lotsa.Output = os.Stdout
	print("insert ")
	lotsa.Ops(N, T, func(i, t int) {
		tx := m.Begin(keys[i])
		tx.Set(keys[i], i)
		tx.End()
	})
	m.Scan(func(key int, value int) bool {
		return true
	})

}

func TestPerfStringKeys(t *testing.T) {
	println("== PERF sync.Map string keys ==")
	N := 2500000
	for t := 1; t <= runtime.GOMAXPROCS(0); t++ {
		testPerfSyncMapStringKeys(N, t)
	}

	println("== PERF biscuits.Map string keys ==")
	for t := 1; t <= runtime.GOMAXPROCS(0); t++ {
		testPerfBiscuitsMapStringKeys(N, t)
	}

}
func TestPerfIntKeys(t *testing.T) {
	println("== PERF sync.Map int keys ==")
	N := 2500000
	for t := 1; t <= runtime.GOMAXPROCS(0); t++ {
		testPerfSyncMapIntKeys(N, t)
	}

	println("== PERF biscuits.Map int keys ==")
	for t := 1; t <= runtime.GOMAXPROCS(0); t++ {
		testPerfBiscuitsMapIntKeys(N, t)
	}

}

func TestExample1(t *testing.T) {
	// Create a map
	var m Map[string, string]

	// Store user 512/Tom
	tx := m.Begin("512")
	tx.Set("512", "Tom")
	tx.End()

	// Get user
	tx = m.Begin("512")
	value, _, _ := tx.Get("512")
	tx.End()
	println(value)

	// Delete user
	tx = m.Begin("512")
	tx.Delete("512")
	tx.End()

	// Output:
	// Tom
}

func TestExample2(t *testing.T) {
	// Create a map
	var m Map[string, string]

	// Store users
	tx := m.Begin("512", "961", "348")
	tx.Set("512", "Tom")
	tx.Set("961", "Sally")
	tx.Set("348", "Janet")
	tx.End()

	// Get two users
	tx = m.Begin("512", "348")
	value1, _, _ := tx.Get("512")
	value2, _, _ := tx.Get("348")
	tx.End()
	println(value1)
	println(value2)

	// Delete users
	tx = m.Begin("512", "961", "348")
	tx.Delete("512")
	tx.Delete("961")
	tx.Delete("348")
	tx.End()

	// Output:
	// Tom
	// Janet
}

func (b *branchNode[K, V]) sane(print bool, hash uint64, depth int,
	validate bool,
) {
	for i := range b.nodes {
		hash = hash << (64 - (depth << hshift)) >> (64 - (depth << hshift))
		hash |= (uint64(i) << (depth << hshift))
		if print {
			fmt.Printf("%s%02d ", strings.Repeat("  ", depth), i)
			fmt.Printf("0x%08x ", hash)
		}
		kind := b.states[i].kind.Load()
		var cloned, locked bool
		if kind >= 4 {
			cloned = kind&kindCloned == kindCloned
			locked = kind&kindClonedLocked == kindClonedLocked
			kind &= 3
		}
		if kind == kindBranch {
			if print {
				fmt.Printf("branch")
				if cloned || locked {
					fmt.Printf("[")
					if cloned {
						fmt.Printf("C")
					}
					if locked {
						fmt.Printf("L")
					}
					fmt.Printf("]")
				}
				fmt.Printf("\n")
			}
			if validate {
				if b.states[i].txid != 0 {
					panic("invalid state")
				}
			}
			(*branchNode[K, V])(b.nodes[i]).sane(print, hash, depth+1, validate)
		} else {
			if kind != kindLeaf {
				panic("invalid kind")
			}
			leaf := (*leafNode[K, V])(b.nodes[i])
			if print {
				fmt.Printf("leaf")
				if cloned || locked {
					fmt.Printf("[")
					if cloned {
						fmt.Printf("C")
					}
					if locked {
						fmt.Printf("L")
					}
					fmt.Printf("]")
				}
			}
			if leaf == nil {
				if print {
					fmt.Printf("\n")
				}
			} else {
				if print {
					fmt.Printf(" ( ")
				}
				for i := range leaf.items {
					ihash := hashkey(leaf.items[i].key)
					if ihash != leaf.items[i].hash {
						panic("invalid hash")
					}
					if ihash&hash != hash {
						panic("invalid hash mask")
					}
					if print {
						fmt.Printf("%v ", leaf.items[i].key)
					}
				}
				if print {
					fmt.Printf(")\n")
				}
			}
		}
	}
}

func (m *Map[K, V]) sane(print bool) {
	if print {
		fmt.Printf("== SANE ==\n")
	}
	m.root.sane(print, 0, 0, m.validate)
}
