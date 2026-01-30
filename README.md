# biscuits

Fast concurrent hashmap for rapidly changing keys.

- Designed for high concurrency and low contention
- Uses a 2PL type locking mechanism
- Works with string keys
- Built with a trie structure under the hood
- O(1) Copy-on-write method

## Example

Set, get, and delete.

```go
// Create a map
var m biscuits.Map[string, string]

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
```

Multiple keys

```go
// Create a map
var m biscuits.Map[string, string]

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
tx := m.Begin("512", "961", "348")
tx.Delete("512")
tx.Delete("961")
tx.Delete("348")
tx.End()

// Output:
// Tom
// Janet
```

Perform direct actions on a key.
This is the fastest way to access or modify a key and is available as
an alternative to using a transaction for single key operations.

```go
var m biscuits.Map[string, string]

// Store user 512/Tom
m.Action("512", func(found bool, value string) (string, biscuits.Action) {
    return "Tom", biscuits.Set
})

// Get user
m.Action("512", func(found bool, value string) (string, biscuits.Action) {
    if found {
        println(value)
    }
    return value, biscuits.NoChange
})

// Delete user
m.Action("512", func(found bool, value string) (string, biscuits.Action) {
    return "", biscuits.Delete
})

// Output:
// Tom
```

## Performance 

The following benchmarks compare `biscuits.Map[int, int]` to the built in `sync.Map[int, int]`. 

Benchmarking 5,000,000 integer keys over 16 threads.

Linux, AMD Ryzen 9 5950X 16-Core processor, Go 1.26
Command: `go test -run Perf`

### sync.Map

```
store    5,000,000 ops over 16 threads in 214ms, 23,391,840/sec, 43 ns/op
load     5,000,000 ops over 16 threads in 73ms,  68,770,455/sec, 15 ns/op
delete   5,000,000 ops over 16 threads in 121ms, 41,307,254/sec, 24 ns/op
```

### biscuits.Map (using Tx)

```
set      5,000,000 ops over 16 threads in 186ms, 26,952,701/sec, 37 ns/op
get      5,000,000 ops over 16 threads in 147ms, 34,090,346/sec, 29 ns/op
delete   5,000,000 ops over 16 threads in 166ms, 30,067,943/sec, 33 ns/op
```

### biscuits.Map (using Action)

```
set      5,000,000 ops over 16 threads in 160ms, 31,230,736/sec, 32 ns/op
get      5,000,000 ops over 16 threads in 113ms, 44,399,531/sec, 23 ns/op
delete   5,000,000 ops over 16 threads in 130ms, 38,564,368/sec, 26 ns/op 
```
