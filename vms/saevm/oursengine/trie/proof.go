package trie

import (
	"bytes"
	"errors"
	"fmt"
)

// Proof is the Merkle proof of key (a hashed trie key: keccak256 of an address or of a state
// key) in the account trie (owner nil) or in the storage trie of owner (keccak256 of the
// account's address), at the top's current state: the RLP of every node on the path from the
// root, root first, as geth's trie.Prove returns it (the root and every node referenced by
// hash; nodes embedded in their parent are not listed). For a key the trie does not hold the
// path ends at the node where the key leaves the trie (an absence proof); an empty trie gives
// no nodes. The top must be clean (after Root, as ApplyTier0 leaves it).
func (t *Top) Proof(owner *[32]byte, key [32]byte) ([][]byte, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.E.Proof(owner, key)
}

// Proof is Top.Proof on the engine.
func (e *Engine) Proof(owner *[32]byte, key [32]byte) (proof [][]byte, err error) {
	if s, a := e.Dirty(); s != 0 || a != 0 {
		return nil, errors.New("trie: proof of a dirty engine (call Root first)")
	}
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(BaseError)
			if !ok {
				panic(r)
			}
			err = be
		}
	}()
	t, storage := e.acct, false
	if owner != nil {
		if t, storage = e.trie(owner), true; t == nil {
			return nil, nil // no storage
		}
	}
	w := &worker{h: NewHasher()}
	j := 0
	for l := 0; l <= int(t.depth); l++ {
		p := t.at(levelOff(l) + j)
		if p.N == 0 { // empty: only the root can be (a child of a stored branch is never visited empty)
			return proof, nil
		}
		if l < int(t.depth) && p.Kind == kBranch {
			var kids [16][]byte
			for i := range kids {
				if k := t.at(levelOff(l+1) + 16*j + i); k.N != 0 {
					kids[i] = k.ref()
				}
			}
			enc := branchEnc(nil, &kids)
			if err := checkRef(w.h.k, p, enc); err != nil {
				return nil, fmt.Errorf("trie: stored node level %d: %w", l, err)
			}
			proof = addNode(proof, enc, l == 0)
			n := nib(&key, l)
			if kids[n] == nil {
				return proof, nil
			}
			j = 16*j + int(n)
			continue
		}
		// a single leaf or an extension (or the cut itself): rebuild the subtree from the leaves
		// of every cut under this position
		sh := 4 * (int(t.depth) - l)
		w.kvs, w.arena = w.kvs[:0], w.arena[:0]
		if storage {
			e.storageRun(w, t, uint32(j)<<sh, uint32(j+1)<<sh)
		} else {
			e.accountRun(w, t, uint32(j)<<sh, uint32(j+1)<<sh)
		}
		if len(w.kvs) == 0 {
			return nil, fmt.Errorf("trie: position level %d holds a node but its cuts no leaves", l)
		}
		enc := subtreeEnc(w.kvs, l)
		if err := checkRef(w.h.k, p, enc); err != nil {
			return nil, fmt.Errorf("trie: rebuilt node level %d: %w", l, err)
		}
		return walk(proof, w.kvs, l, &key, l == 0), nil
	}
	return nil, errors.New("trie: proof walked past the cut depth")
}

// checkRef checks that enc is the node position p references.
func checkRef(k *Keccaker, p *Pos, enc []byte) error {
	var q Pos
	k.refOf(&q, enc, 0)
	if q.N != p.N || !bytes.Equal(q.ref(), p.ref()) {
		return errors.New("node differs from its stored reference")
	}
	return nil
}

// addNode appends a node's encoding when its parent references it by hash (or it is the root).
func addNode(proof [][]byte, enc []byte, root bool) [][]byte {
	if root || len(enc) >= 32 {
		proof = append(proof, bytes.Clone(enc))
	}
	return proof
}

// refBytes is the reference a parent holds for an encoding.
func refBytes(k *Keccaker, enc []byte) []byte {
	if len(enc) < 32 {
		return enc
	}
	h := k.Sum(enc)
	return h[:]
}

// subtreeEnc is the encoding of the node at slot depth d over kvs (sorted, sharing nibbles
// [0, d)): a leaf, or a branch, with an extension over it when the keys share more nibbles.
func subtreeEnc(kvs []kv, d int) []byte {
	if len(kvs) == 1 {
		return leafEnc(nil, kvs[0].key, d, kvs[0].val)
	}
	c := lcpNibs(kvs[0].key, kvs[len(kvs)-1].key, d)
	br := branchOf(kvs, c)
	if c == d {
		return br
	}
	return extEnc(nil, kvs[0].key, d, c, refBytes(NewKeccaker(), br))
}

// branchOf encodes the branch at depth c over kvs (keys sharing nibbles [0, c), not all the
// same nibble c).
func branchOf(kvs []kv, c int) []byte {
	var kids [16][]byte
	k := NewKeccaker()
	for lo := 0; lo < len(kvs); {
		n := nib(kvs[lo].key, c)
		hi := lo + 1
		for hi < len(kvs) && nib(kvs[hi].key, c) == n {
			hi++
		}
		kids[n] = refBytes(k, subtreeEnc(kvs[lo:hi], c+1))
		lo = hi
	}
	return branchEnc(nil, &kids)
}

// walk appends the proof nodes of key in the subtree at slot depth d over kvs.
func walk(proof [][]byte, kvs []kv, d int, key *[32]byte, root bool) [][]byte {
	for {
		if len(kvs) == 1 { // a leaf: the end of the path, whether it holds key or not
			return addNode(proof, leafEnc(nil, kvs[0].key, d, kvs[0].val), root)
		}
		c := lcpNibs(kvs[0].key, kvs[len(kvs)-1].key, d)
		br := branchOf(kvs, c)
		if c > d {
			proof = addNode(proof, extEnc(nil, kvs[0].key, d, c, refBytes(NewKeccaker(), br)), root)
			root = false
			if lcpNibs(kvs[0].key, key, d) < c { // key leaves the trie inside the extension
				return proof
			}
		}
		proof = addNode(proof, br, root)
		root = false
		n := nib(key, c)
		lo := 0
		for lo < len(kvs) && nib(kvs[lo].key, c) != n {
			lo++
		}
		if lo == len(kvs) { // no child for the key's nibble
			return proof
		}
		hi := lo + 1
		for hi < len(kvs) && nib(kvs[hi].key, c) == n {
			hi++
		}
		kvs, d = kvs[lo:hi], c+1
	}
}
