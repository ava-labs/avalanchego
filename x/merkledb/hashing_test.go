// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var sha256HashNodeTests = []struct {
	name         string
	n            *node
	expectedHash string
}{
	{
		name:         "empty node",
		n:            newNode(Key{}),
		expectedHash: "rbhtxoQ1DqWHvb6w66BZdVyjmPAneZUSwQq9uKj594qvFSdav",
	},
	{
		name: "has value",
		n: func() *node {
			n := newNode(Key{})
			n.setValue(SHA256Hasher, maybe.Some([]byte("value1")))
			return n
		}(),
		expectedHash: "2vx2xueNdWoH2uB4e8hbMU5jirtZkZ1c3ePCWDhXYaFRHpCbnQ",
	},
	{
		name:         "has key",
		n:            newNode(ToKey([]byte{0, 1, 2, 3, 4, 5, 6, 7})),
		expectedHash: "2vA8ggXajhFEcgiF8zHTXgo8T2ALBFgffp1xfn48JEni1Uj5uK",
	},
	{
		name: "1 child",
		n: func() *node {
			n := newNode(Key{})
			childNode := newNode(ToKey([]byte{255}))
			childNode.setValue(SHA256Hasher, maybe.Some([]byte("value1")))
			n.addChildWithID(childNode, 4, SHA256Hasher.HashNode(childNode))
			return n
		}(),
		expectedHash: "YfJRufqUKBv9ez6xZx6ogpnfDnw9fDsyebhYDaoaH57D3vRu3",
	},
	{
		name: "2 children",
		n: func() *node {
			n := newNode(Key{})

			childNode1 := newNode(ToKey([]byte{255}))
			childNode1.setValue(SHA256Hasher, maybe.Some([]byte("value1")))

			childNode2 := newNode(ToKey([]byte{237}))
			childNode2.setValue(SHA256Hasher, maybe.Some([]byte("value2")))

			n.addChildWithID(childNode1, 4, SHA256Hasher.HashNode(childNode1))
			n.addChildWithID(childNode2, 4, SHA256Hasher.HashNode(childNode2))
			return n
		}(),
		expectedHash: "YVmbx5MZtSKuYhzvHnCqGrswQcxmozAkv7xE1vTA2EiGpWUkv",
	},
	{
		name: "16 children",
		n: func() *node {
			n := newNode(Key{})

			for i := byte(0); i < 16; i++ {
				childNode := newNode(ToKey([]byte{i << 4}))
				childNode.setValue(SHA256Hasher, maybe.Some([]byte("some value")))

				n.addChildWithID(childNode, 4, SHA256Hasher.HashNode(childNode))
			}
			return n
		}(),
		expectedHash: "5YiFLL7QV3f441See9uWePi3wVKsx9fgvX5VPhU8PRxtLqhwY",
	},
}

// Ensure that SHA256.HashNode is deterministic
func Fuzz_SHA256_HashNode(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			randSeed int,
		) {
			require := require.New(t)
			for _, bf := range validBranchFactors { // Create a random node
				r := rand.New(rand.NewSource(int64(randSeed)))

				children := map[byte]*child{}
				numChildren := r.Intn(int(bf))
				for i := 0; i < numChildren; i++ {
					compressedKeyLen := r.Intn(32)
					compressedKeyBytes := make([]byte, compressedKeyLen)
					_, _ = r.Read(compressedKeyBytes)

					children[byte(i)] = &child{
						compressedKey: ToKey(compressedKeyBytes),
						id:            ids.GenerateTestID(),
						hasValue:      r.Intn(2) == 1,
					}
				}

				hasValue := r.Intn(2) == 1
				value := maybe.Nothing[[]byte]()
				if hasValue {
					valueBytes := make([]byte, r.Intn(64))
					_, _ = r.Read(valueBytes)
					value = maybe.Some(valueBytes)
				}

				key := make([]byte, r.Intn(32))
				_, _ = r.Read(key)

				hv := &node{
					key: ToKey(key),
					dbNode: dbNode{
						children: children,
						value:    value,
					},
				}

				// Hash hv multiple times
				hash1 := SHA256Hasher.HashNode(hv)
				hash2 := SHA256Hasher.HashNode(hv)

				// Make sure they're the same
				require.Equal(hash1, hash2)
			}
		},
	)
}

func Test_SHA256_HashNode(t *testing.T) {
	for _, test := range sha256HashNodeTests {
		t.Run(test.name, func(t *testing.T) {
			hash := SHA256Hasher.HashNode(test.n)
			require.Equal(t, test.expectedHash, hash.String())
		})
	}
}

func Benchmark_SHA256_HashNode(b *testing.B) {
	for _, benchmark := range sha256HashNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				SHA256Hasher.HashNode(benchmark.n)
			}
		})
	}
}
