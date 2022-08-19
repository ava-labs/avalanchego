// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trie

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/stretchr/testify/assert"
)

// FillTrie fills a given trie with [numKeys] number of keys, each of size [keySize]
// returns inserted keys and values
// FillTrie reads from [rand] and the caller should call rand.Seed(n) for deterministic results
func FillTrie(t *testing.T, numKeys int, keySize int, testTrie *Trie) ([][]byte, [][]byte) {
	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for i := 0; i < numKeys; i++ {
		key := make([]byte, keySize)
		binary.BigEndian.PutUint64(key[:wrappers.LongLen], uint64(i+1))
		_, err := rand.Read(key[wrappers.LongLen:])
		assert.NoError(t, err)

		value := make([]byte, rand.Intn(128)+128) // min 128 bytes, max 256 bytes
		_, err = rand.Read(value)
		assert.NoError(t, err)

		if err = testTrie.TryUpdate(key, value); err != nil {
			t.Fatal("error updating trie", err)
		}

		keys = append(keys, key)
		values = append(values, value)
	}
	return keys, values
}
