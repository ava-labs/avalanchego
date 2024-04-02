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

var (
	hashNodeTests = []struct {
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
	encodeDBNodeTests = []struct {
		name          string
		n             *dbNode
		expectedBytes []byte
	}{
		{
			name: "empty node",
			n:    &dbNode{},
			expectedBytes: []byte{
				0x00, // value.HasValue()
				0x00, // len(children)
			},
		},
		{
			name: "has value",
			n: &dbNode{
				value: maybe.Some([]byte("value")),
			},
			expectedBytes: []byte{
				0x01,                    // value.HasValue()
				0x05,                    // len(value.Value())
				'v', 'a', 'l', 'u', 'e', // value.Value()
				0x00, // len(children)
			},
		},
		{
			name: "1 child",
			n: &dbNode{
				value: maybe.Some([]byte("value")),
				children: map[byte]*child{
					0: {
						compressedKey: ToKey([]byte{0}),
						id: ids.ID{
							0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
							0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
							0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
							0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
						},
						hasValue: true,
					},
				},
			},
			expectedBytes: []byte{
				0x01,                    // value.HasValue()
				0x05,                    // len(value.Value())
				'v', 'a', 'l', 'u', 'e', // value.Value()
				0x01, // len(children)
				0x00, // children[0].index
				0x08, // len(children[0].compressedKey)
				0x00, // children[0].compressedKey
				// children[0].id
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x01, // children[0].hasValue
			},
		},
		{
			name: "2 children",
			n: &dbNode{
				value: maybe.Some([]byte("value")),
				children: map[byte]*child{
					0: {
						compressedKey: ToKey([]byte{0}),
						id: ids.ID{
							0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
							0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
							0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
							0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
						},
						hasValue: true,
					},
					1: {
						compressedKey: ToKey([]byte{1, 2, 3}),
						id: ids.ID{
							0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
							0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
							0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
							0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
						},
						hasValue: false,
					},
				},
			},
			expectedBytes: []byte{
				0x01,                    // value.HasValue()
				0x05,                    // len(value.Value())
				'v', 'a', 'l', 'u', 'e', // value.Value()
				0x02, // len(children)
				0x00, // children[0].index
				0x08, // len(children[0].compressedKey)
				0x00, // children[0].compressedKey
				// children[0].id
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x01,             // children[0].hasValue
				0x01,             // children[1].index
				0x18,             // len(children[1].compressedKey)
				0x01, 0x02, 0x03, // children[1].compressedKey
				// children[1].id
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
				0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
				0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
				0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
				0x00, // children[1].hasValue
			},
		},
		{
			name: "16 children",
			n: func() *dbNode {
				n := &dbNode{
					value:    maybe.Some([]byte("value")),
					children: make(map[byte]*child),
				}
				for i := byte(0); i < 16; i++ {
					n.children[i] = &child{
						compressedKey: ToKey([]byte{i}),
						id: ids.ID{
							0x00 + i, 0x01 + i, 0x02 + i, 0x03 + i,
							0x04 + i, 0x05 + i, 0x06 + i, 0x07 + i,
							0x08 + i, 0x09 + i, 0x0a + i, 0x0b + i,
							0x0c + i, 0x0d + i, 0x0e + i, 0x0f + i,
							0x10 + i, 0x11 + i, 0x12 + i, 0x13 + i,
							0x14 + i, 0x15 + i, 0x16 + i, 0x17 + i,
							0x18 + i, 0x19 + i, 0x1a + i, 0x1b + i,
							0x1c + i, 0x1d + i, 0x1e + i, 0x1f + i,
						},
						hasValue: i%2 == 0,
					}
				}
				return n
			}(),
			expectedBytes: []byte{
				0x01,                    // value.HasValue()
				0x05,                    // len(value.Value())
				'v', 'a', 'l', 'u', 'e', // value.Value()
				0x10, // len(children)
				0x00, // children[0].index
				0x08, // len(children[0].compressedKey)
				0x00, // children[0].compressedKey
				// children[0].id
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x01, // children[0].hasValue
				0x01, // children[1].index
				0x08, // len(children[1].compressedKey)
				0x01, // children[1].compressedKey
				// children[1].id
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				0x00, // children[1].hasValue
				0x02, // children[2].index
				0x08, // len(children[2].compressedKey)
				0x02, // children[2].compressedKey
				// children[2].id
				0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
				0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11,
				0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19,
				0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21,
				0x01, // children[2].hasValue
				0x03, // children[3].index
				0x08, // len(children[3].compressedKey)
				0x03, // children[3].compressedKey
				// children[3].id
				0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
				0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12,
				0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a,
				0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22,
				0x00, // children[3].hasValue
				0x04, // children[4].index
				0x08, // len(children[4].compressedKey)
				0x04, // children[4].compressedKey
				// children[4].id
				0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
				0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
				0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b,
				0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23,
				0x01, // children[4].hasValue
				0x05, // children[5].index
				0x08, // len(children[5].compressedKey)
				0x05, // children[5].compressedKey
				// children[5].id
				0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c,
				0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14,
				0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c,
				0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24,
				0x00, // children[5].hasValue
				0x06, // children[6].index
				0x08, // len(children[6].compressedKey)
				0x06, // children[6].compressedKey
				// children[6].id
				0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d,
				0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15,
				0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
				0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25,
				0x01, // children[6].hasValue
				0x07, // children[7].index
				0x08, // len(children[7].compressedKey)
				0x07, // children[7].compressedKey
				// children[7].id
				0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e,
				0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16,
				0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e,
				0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26,
				0x00, // children[7].hasValue
				0x08, // children[8].index
				0x08, // len(children[8].compressedKey)
				0x08, // children[8].compressedKey
				// children[8].id
				0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
				0x01, // children[8].hasValue
				0x09, // children[9].index
				0x08, // len(children[9].compressedKey)
				0x09, // children[9].compressedKey
				// children[9].id
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x00, // children[9].hasValue
				0x0a, // children[10].index
				0x08, // len(children[10].compressedKey)
				0x0a, // children[10].compressedKey
				// children[10].id
				0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11,
				0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19,
				0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21,
				0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29,
				0x01, // children[10].hasValue
				0x0b, // children[11].index
				0x08, // len(children[11].compressedKey)
				0x0b, // children[11].compressedKey
				// children[11].id
				0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12,
				0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a,
				0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22,
				0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a,
				0x00, // children[11].hasValue
				0x0c, // children[12].index
				0x08, // len(children[12].compressedKey)
				0x0c, // children[12].compressedKey
				// children[12].id
				0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
				0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b,
				0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23,
				0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b,
				0x01, // children[12].hasValue
				0x0d, // children[13].index
				0x08, // len(children[13].compressedKey)
				0x0d, // children[13].compressedKey
				// children[13].id
				0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14,
				0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c,
				0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24,
				0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c,
				0x00, // children[13].hasValue
				0x0e, // children[14].index
				0x08, // len(children[14].compressedKey)
				0x0e, // children[14].compressedKey
				// children[14].id
				0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15,
				0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
				0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25,
				0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d,
				0x01, // children[14].hasValue
				0x0f, // children[15].index
				0x08, // len(children[15].compressedKey)
				0x0f, // children[15].compressedKey
				// children[15].id
				0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16,
				0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e,
				0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26,
				0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e,
				0x00, // children[15].hasValue
			},
		},
	}
	encodeKeyTests = []struct {
		name          string
		key           Key
		expectedBytes []byte
	}{
		{
			name: "empty",
			key:  ToKey([]byte{}),
			expectedBytes: []byte{
				0x00, // length
			},
		},
		{
			name: "1 byte",
			key:  ToKey([]byte{0}),
			expectedBytes: []byte{
				0x08, // length
				0x00, // key
			},
		},
		{
			name: "2 bytes",
			key:  ToKey([]byte{0, 1}),
			expectedBytes: []byte{
				0x10,       // length
				0x00, 0x01, // key
			},
		},
		{
			name: "4 bytes",
			key:  ToKey([]byte{0, 1, 2, 3}),
			expectedBytes: []byte{
				0x20,                   // length
				0x00, 0x01, 0x02, 0x03, // key
			},
		},
		{
			name: "8 bytes",
			key:  ToKey([]byte{0, 1, 2, 3, 4, 5, 6, 7}),
			expectedBytes: []byte{
				0x40,                                           // length
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, // key
			},
		},
		{
			name: "32 bytes",
			key:  ToKey(make([]byte, 32)),
			expectedBytes: append(
				[]byte{
					0x80, 0x02, // length
				},
				make([]byte, 32)..., // key
			),
		},
		{
			name: "64 bytes",
			key:  ToKey(make([]byte, 64)),
			expectedBytes: append(
				[]byte{
					0x80, 0x04, // length
				},
				make([]byte, 64)..., // key
			),
		},
		{
			name: "1024 bytes",
			key:  ToKey(make([]byte, 1024)),
			expectedBytes: append(
				[]byte{
					0x80, 0x40, // length
				},
				make([]byte, 1024)..., // key
			),
		},
	}
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
	for _, test := range hashNodeTests {
		t.Run(test.name, func(t *testing.T) {
			hash := hashNode(test.n)
			require.Equal(t, test.expectedHash, hash.String())
		})
	}
}

func TestEncodeDBNode(t *testing.T) {
	for _, test := range encodeDBNodeTests {
		t.Run(test.name, func(t *testing.T) {
			bytes := encodeDBNode(test.n)
			require.Equal(t, test.expectedBytes, bytes)
		})
	}
}

func TestEncodeKey(t *testing.T) {
	for _, test := range encodeKeyTests {
		t.Run(test.name, func(t *testing.T) {
			bytes := encodeKey(test.key)
			require.Equal(t, test.expectedBytes, bytes)
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
	for _, benchmark := range hashNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				hashNode(benchmark.n)
			}
		})
	}
}

func Benchmark_EncodeDBNode(b *testing.B) {
	for _, benchmark := range encodeDBNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				encodeDBNode(benchmark.n)
			}
		})
	}
}

func Benchmark_EncodeKey(b *testing.B) {
	for _, benchmark := range encodeKeyTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				encodeKey(benchmark.key)
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
