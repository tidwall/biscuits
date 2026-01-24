# biscuits

Vertically scalable hashmap for rapidly changing keys.

- Designed for high concurrency and low contention
- Uses a 2PL type locking mechanism
- Works with string keys
- Built with a trie structure under the hood
- O(1) Copy-on-write method
`.

## Example

Set, get, and delete.

```go
// Create a map
var m biscuits.Map[string]

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
var m biscuits.Map[string]

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


