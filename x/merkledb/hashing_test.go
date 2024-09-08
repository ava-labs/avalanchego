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
	name               string
	newNode            func(hasher Hasher) *node
	expectedSHA256Hash string
	expectedBLAKE3Hash string
}{
	{
		name: "empty node",
		newNode: func(Hasher) *node {
			return newNode(Key{})
		},
		expectedSHA256Hash: "rbhtxoQ1DqWHvb6w66BZdVyjmPAneZUSwQq9uKj594qvFSdav",
		expectedBLAKE3Hash: "2713tJzWKeBB4UqkqHzs2kkKAvvnXw16rcaKXehjYraznE3cYC",
	},
	{
		name: "has value",
		newNode: func(hasher Hasher) *node {
			n := newNode(Key{})
			n.setValue(hasher, maybe.Some([]byte("value1")))
			return n
		},
		expectedSHA256Hash: "2vx2xueNdWoH2uB4e8hbMU5jirtZkZ1c3ePCWDhXYaFRHpCbnQ",
		expectedBLAKE3Hash: "7w2q1s8ZrGhuX6cQtBnBCbN9TujSRigEAznXCKLPp91aJq6sx",
	},
	{
		name: "has key",
		newNode: func(Hasher) *node {
			return newNode(ToKey([]byte{0, 1, 2, 3, 4, 5, 6, 7}))
		},
		expectedSHA256Hash: "2vA8ggXajhFEcgiF8zHTXgo8T2ALBFgffp1xfn48JEni1Uj5uK",
		expectedBLAKE3Hash: "jHpUfw75cszxAMSo4aMMa6JAxh6dtKbaVJGiCEbQBoFtUmzQq",
	},
	{
		name: "1 child",
		newNode: func(hasher Hasher) *node {
			childNode := newNode(ToKey([]byte{255}))
			childNode.setValue(hasher, maybe.Some([]byte("value1")))

			n := newNode(Key{})
			n.addChildWithID(childNode, 4, hasher.HashNode(childNode))
			return n
		},
		expectedSHA256Hash: "YfJRufqUKBv9ez6xZx6ogpnfDnw9fDsyebhYDaoaH57D3vRu3",
		expectedBLAKE3Hash: "2ovNiNxAeLh7XHNSjGQDDiLCikVzbHfTa86TDnsSYrSvxoDZp1",
	},
	{
		name: "2 children",
		newNode: func(hasher Hasher) *node {
			childNode1 := newNode(ToKey([]byte{255}))
			childNode1.setValue(hasher, maybe.Some([]byte("value1")))

			childNode2 := newNode(ToKey([]byte{237}))
			childNode2.setValue(hasher, maybe.Some([]byte("value2")))

			n := newNode(Key{})
			n.addChildWithID(childNode1, 4, hasher.HashNode(childNode1))
			n.addChildWithID(childNode2, 4, hasher.HashNode(childNode2))
			return n
		},
		expectedSHA256Hash: "YVmbx5MZtSKuYhzvHnCqGrswQcxmozAkv7xE1vTA2EiGpWUkv",
		expectedBLAKE3Hash: "MmwSZRw7dm34AK5yHpw1n8H4FvWsc6RjCCWrr8h6Q8WrBvgQ9",
	},
	{
		name: "16 children",
		newNode: func(hasher Hasher) *node {
			n := newNode(Key{})
			for i := byte(0); i < 16; i++ {
				childNode := newNode(ToKey([]byte{i << 4}))
				childNode.setValue(hasher, maybe.Some([]byte("some value")))

				n.addChildWithID(childNode, 4, hasher.HashNode(childNode))
			}
			return n
		},
		expectedSHA256Hash: "5YiFLL7QV3f441See9uWePi3wVKsx9fgvX5VPhU8PRxtLqhwY",
		expectedBLAKE3Hash: "ZnCsxyX1qQzAjwocmzQ7gxKQfk6VmB5jk6p6KppYVfr3b6pVe",
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

func Test_SHA256_HashNode(t *testing.T) {
	for _, test := range hashNodeTests {
		t.Run(test.name, func(t *testing.T) {
			hash := SHA256Hasher.HashNode(test.newNode(SHA256Hasher))
			require.Equal(t, test.expectedSHA256Hash, hash.String())
		})
	}
}

func Test_BLAKE3_HashNode(t *testing.T) {
	for _, test := range hashNodeTests {
		t.Run(test.name, func(t *testing.T) {
			hash := BLAKE3Hasher.HashNode(test.newNode(BLAKE3Hasher))
			require.Equal(t, test.expectedBLAKE3Hash, hash.String())
		})
	}
}

func Benchmark_SHA256_HashNode(b *testing.B) {
	for _, benchmark := range hashNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			n := benchmark.newNode(SHA256Hasher)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				SHA256Hasher.HashNode(n)
			}
		})
	}
}

func Benchmark_BLAKE3_HashNode(b *testing.B) {
	for _, benchmark := range hashNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			n := benchmark.newNode(BLAKE3Hasher)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				BLAKE3Hasher.HashNode(n)
			}
		})
	}
}
