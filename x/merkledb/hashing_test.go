// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var hashNodeTests = []struct {
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
			n.setValue(maybe.Some([]byte("value1")))
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
			childNode.setValue(maybe.Some([]byte("value1")))
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
			childNode1.setValue(maybe.Some([]byte("value1")))

			childNode2 := newNode(ToKey([]byte{237}))
			childNode2.setValue(maybe.Some([]byte("value2")))

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
				childNode.setValue(maybe.Some([]byte("some value")))

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
				r := rand.New(rand.NewSource(int64(randSeed))) // #nosec G404

				children := map[byte]*child{}
				numChildren := r.Intn(int(bf)) // #nosec G404
				for i := 0; i < numChildren; i++ {
					compressedKeyLen := r.Intn(32) // #nosec G404
					compressedKeyBytes := make([]byte, compressedKeyLen)
					_, _ = r.Read(compressedKeyBytes) // #nosec G404

					children[byte(i)] = &child{
						compressedKey: ToKey(compressedKeyBytes),
						id:            ids.GenerateTestID(),
						hasValue:      r.Intn(2) == 1, // #nosec G404
					}
				}

				hasValue := r.Intn(2) == 1 // #nosec G404
				value := maybe.Nothing[[]byte]()
				if hasValue {
					valueBytes := make([]byte, r.Intn(64)) // #nosec G404
					_, _ = r.Read(valueBytes)              // #nosec G404
					value = maybe.Some(valueBytes)
				}

				key := make([]byte, r.Intn(32)) // #nosec G404
				_, _ = r.Read(key)              // #nosec G404

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

func TestHashNode(t *testing.T) {
	for _, test := range hashNodeTests {
		t.Run(test.name, func(t *testing.T) {
			hash := SHA256Hasher.HashNode(test.n)
			require.Equal(t, test.expectedHash, hash.String())
		})
	}
}

func Benchmark_HashNode(b *testing.B) {
	for _, benchmark := range hashNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				SHA256Hasher.HashNode(benchmark.n)
			}
		})
	}
}

// Benchmark_HashNode/empty_node-12         	15512535	        76.75 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/has_value-12          	13994202	        85.44 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/has_key-12            	16025658	        73.92 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/1_child-12            	 8413833	       142.3 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/2_children-12         	 5875944	       204.5 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/16_children-12        	 1206897	       988.0 ns/op	       0 B/op	       0 allocs/op

// Benchmark_HashNode/empty_node-12         	13344910	        81.39 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/has_value-12          	13953663	        87.54 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/has_key-12            	15724867	        76.84 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/1_child-12            	 8251053	       145.0 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/2_children-12         	 5770500	       208.3 ns/op	       0 B/op	       0 allocs/op
// Benchmark_HashNode/16_children-12        	 1201893	       997.4 ns/op	       0 B/op	       0 allocs/op
