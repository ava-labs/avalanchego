// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

func FuzzCodecBool(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)

			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := decodeBool(reader)
			if err != nil {
				t.SkipNow()
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			encodeBool(&buf, got)
			bufBytes := buf.Bytes()
			require.Len(bufBytes, numRead)
			require.Equal(b[:numRead], bufBytes)
		},
	)
}

func FuzzCodecInt(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)

			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := decodeUint(reader)
			if err != nil {
				t.SkipNow()
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			encodeUint(&buf, got)
			bufBytes := buf.Bytes()
			require.Len(bufBytes, numRead)
			require.Equal(b[:numRead], bufBytes)
		},
	)
}

func FuzzCodecKey(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)
			got, err := decodeKey(b)
			if err != nil {
				t.SkipNow()
			}

			// Encoding [got] should be the same as [b].
			gotBytes := encodeKey(got)
			require.Equal(b, gotBytes)
		},
	)
}

func FuzzCodecDBNodeCanonical(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)
			node := &dbNode{}
			if err := decodeDBNode(b, node); err != nil {
				t.SkipNow()
			}

			// Encoding [node] should be the same as [b].
			buf := encodeDBNode(node)
			require.Equal(b, buf)
		},
	)
}

func FuzzCodecDBNodeDeterministic(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			randSeed int,
			hasValue bool,
			valueBytes []byte,
		) {
			require := require.New(t)
			for _, bf := range validBranchFactors {
				r := rand.New(rand.NewSource(int64(randSeed))) // #nosec G404

				value := maybe.Nothing[[]byte]()
				if hasValue {
					if len(valueBytes) == 0 {
						// We do this because when we encode a value of []byte{}
						// we will later decode it as nil.
						// Doing this prevents inconsistency when comparing the
						// encoded and decoded values below.
						valueBytes = nil
					}
					value = maybe.Some(valueBytes)
				}

				numChildren := r.Intn(int(bf)) // #nosec G404

				children := map[byte]*child{}
				for i := 0; i < numChildren; i++ {
					var childID ids.ID
					_, _ = r.Read(childID[:]) // #nosec G404

					childKeyBytes := make([]byte, r.Intn(32)) // #nosec G404
					_, _ = r.Read(childKeyBytes)              // #nosec G404

					children[byte(i)] = &child{
						compressedKey: ToKey(childKeyBytes),
						id:            childID,
					}
				}
				node := dbNode{
					value:    value,
					children: children,
				}

				nodeBytes := encodeDBNode(&node)
				require.Len(nodeBytes, encodedDBNodeSize(&node))
				var gotNode dbNode
				require.NoError(decodeDBNode(nodeBytes, &gotNode))
				require.Equal(node, gotNode)

				nodeBytes2 := encodeDBNode(&gotNode)
				require.Equal(nodeBytes, nodeBytes2)
			}
		},
	)
}

func TestCodecDecodeDBNode_TooShort(t *testing.T) {
	require := require.New(t)

	var (
		parsedDBNode  dbNode
		tooShortBytes = make([]byte, minDBNodeLen-1)
	)
	err := decodeDBNode(tooShortBytes, &parsedDBNode)
	require.ErrorIs(err, io.ErrUnexpectedEOF)
}

// Ensure that hashNode is deterministic
func FuzzHashNode(f *testing.F) {
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
				hash1 := hashNode(hv)
				hash2 := hashNode(hv)

				// Make sure they're the same
				require.Equal(hash1, hash2)
			}
		},
	)
}

func TestHashNode(t *testing.T) {
	tests := []struct {
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
				n.addChildWithID(childNode, 4, hashNode(childNode))
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

				n.addChildWithID(childNode1, 4, hashNode(childNode1))
				n.addChildWithID(childNode2, 4, hashNode(childNode2))
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

					n.addChildWithID(childNode, 4, hashNode(childNode))
				}
				return n
			}(),
			expectedHash: "5YiFLL7QV3f441See9uWePi3wVKsx9fgvX5VPhU8PRxtLqhwY",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hash := hashNode(test.n)
			require.Equal(t, test.expectedHash, hash.String())
		})
	}
}

func TestCodecDecodeKeyLengthOverflowRegression(t *testing.T) {
	_, err := decodeKey(binary.AppendUvarint(nil, math.MaxInt))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestUintSize(t *testing.T) {
	// Test lower bound
	expectedSize := uintSize(0)
	actualSize := binary.PutUvarint(make([]byte, binary.MaxVarintLen64), 0)
	require.Equal(t, expectedSize, actualSize)

	// Test upper bound
	expectedSize = uintSize(math.MaxUint64)
	actualSize = binary.PutUvarint(make([]byte, binary.MaxVarintLen64), math.MaxUint64)
	require.Equal(t, expectedSize, actualSize)

	// Test powers of 2
	for power := 0; power < 64; power++ {
		n := uint64(1) << uint(power)
		expectedSize := uintSize(n)
		actualSize := binary.PutUvarint(make([]byte, binary.MaxVarintLen64), n)
		require.Equal(t, expectedSize, actualSize, power)
	}
}

func Benchmark_HashNode(b *testing.B) {
	benchmarks := []struct {
		name string
		n    *node
	}{
		{
			name: "empty node",
			n:    newNode(Key{}),
		},
		{
			name: "has value",
			n: func() *node {
				n := newNode(Key{})
				n.setValue(maybe.Some([]byte("value1")))
				return n
			}(),
		},
		{
			name: "has key",
			n:    newNode(ToKey([]byte{0, 1, 2, 3, 4, 5, 6, 7})),
		},
		{
			name: "1 child",
			n: func() *node {
				n := newNode(Key{})
				childNode := newNode(ToKey([]byte{255}))
				childNode.setValue(maybe.Some([]byte("value1")))
				n.addChildWithID(childNode, 4, hashNode(childNode))
				return n
			}(),
		},
		{
			name: "2 children",
			n: func() *node {
				n := newNode(Key{})

				childNode1 := newNode(ToKey([]byte{255}))
				childNode1.setValue(maybe.Some([]byte("value1")))

				childNode2 := newNode(ToKey([]byte{237}))
				childNode2.setValue(maybe.Some([]byte("value2")))

				n.addChildWithID(childNode1, 4, hashNode(childNode1))
				n.addChildWithID(childNode2, 4, hashNode(childNode2))
				return n
			}(),
		},
		{
			name: "16 children",
			n: func() *node {
				n := newNode(Key{})

				for i := byte(0); i < 16; i++ {
					childNode := newNode(ToKey([]byte{i << 4}))
					childNode.setValue(maybe.Some([]byte("some value")))

					n.addChildWithID(childNode, 4, hashNode(childNode))
				}
				return n
			}(),
		},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				hashNode(benchmark.n)
			}
		})
	}
}

func Benchmark_EncodeUint(b *testing.B) {
	var dst bytes.Buffer
	dst.Grow(binary.MaxVarintLen64)

	for _, v := range []uint64{0, 1, 2, 32, 1024, 32768} {
		b.Run(strconv.FormatUint(v, 10), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				encodeUint(&dst, v)
				dst.Reset()
			}
		})
	}
}
