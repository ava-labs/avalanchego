package trie

import "sync"

// Root is the root hash of the trie holding the given leaves: keys in ascending order, vals
// the leaf value items (StorageVal, AccountVal). Used to hash a whole small trie at once.
func (h *Hasher) Root(keys []*[32]byte, vals [][]byte) [32]byte {
	kvs := make([]kv, len(keys))
	for i := range keys {
		kvs[i] = kv{key: keys[i], val: vals[i]}
	}
	h.Begin()
	var p Pos
	h.Add(kvs, 0, &p)
	h.Finish()
	return h.k.RootHash(&p)
}

// Root16 is Root over leaves split by the first nibble of their key: bucket i holds the keys
// starting with nibble i, each bucket in ascending order. The 16 subtries hash in parallel.
func Root16(keys *[16][]*[32]byte, vals *[16][][]byte) [32]byte {
	n := 0
	for i := range keys {
		if len(keys[i]) > 0 {
			n++
		}
	}
	if n < 2 { // the root is not a branch over the first nibble: hash it as one trie
		var all []*[32]byte
		var vs [][]byte
		for i := range keys {
			all, vs = append(all, keys[i]...), append(vs, vals[i]...)
		}
		return NewHasher().Root(all, vs)
	}
	var refs [16]Pos
	var wg sync.WaitGroup
	for i := range keys {
		if len(keys[i]) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			kvs := make([]kv, len(keys[i]))
			for j := range kvs {
				kvs[j] = kv{key: keys[i][j], val: vals[i][j]}
			}
			h := NewHasher()
			h.Begin()
			h.Add(kvs, 1, &refs[i])
			h.Finish()
		}()
	}
	wg.Wait()
	var kids [16][]byte
	for i := range refs {
		if len(keys[i]) > 0 {
			kids[i] = refs[i].ref()
		}
	}
	return NewKeccaker().Sum(branchEnc(nil, &kids))
}
