// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
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
			n: &dbNode{
				children: make(map[byte]*child),
			},
			expectedBytes: []byte{
				0x00, // value.HasValue()
				0x00, // len(children)
			},
		},
		{
			name: "has value",
			n: &dbNode{
				value:    maybe.Some([]byte("value")),
				children: make(map[byte]*child),
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

			r := codecReader{
				b: b,
			}
			startLen := len(r.b)
			got, err := r.Bool()
			if err != nil {
				t.SkipNow()
			}
			endLen := len(r.b)
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			w := codecWriter{}
			w.Bool(got)
			require.Len(w.b, numRead)
			require.Equal(b[:numRead], w.b)
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

			c := codecReader{
				b: b,
			}
			startLen := len(c.b)
			got, err := c.Uvarint()
			if err != nil {
				t.SkipNow()
			}
			endLen := len(c.b)
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			w := codecWriter{}
			w.Uvarint(got)
			require.Len(w.b, numRead)
			require.Equal(b[:numRead], w.b)
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

				// Enforce that modifying bytes after decodeDBNode doesn't
				// modify the populated struct.
				clear(nodeBytes)
				require.Equal(node, gotNode)
			}
		},
	)
}

func TestCodecDecodeDBNode_TooShort(t *testing.T) {
	require := require.New(t)

	var (
		parsedDBNode  dbNode
		tooShortBytes = make([]byte, 1)
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

func TestDecodeDBNode(t *testing.T) {
	for _, test := range encodeDBNodeTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			var n dbNode
			require.NoError(decodeDBNode(test.expectedBytes, &n))
			require.Equal(test.n, &n)
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

func TestDecodeKey(t *testing.T) {
	for _, test := range encodeKeyTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			key, err := decodeKey(test.expectedBytes)
			require.NoError(err)
			require.Equal(test.key, key)
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

/*
Before:
```
Benchmark_HashNode/empty_node-12         	15459874	        76.00 ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/has_value-12          	13865698	        87.08 ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/has_key-12            	16045542	        75.12 ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/1_child-12            	 8345124	       142.2  ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/2_children-12         	 5880582	       204.8  ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/16_children-12        	 1205710	      1002    ns/op	       0 B/op	       0 allocs/op
```

After:
```
Benchmark_HashNode/empty_node-12         	15400492	        77.47 ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/has_value-12          	14085064	        90.31 ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/has_key-12            	15843788	        75.32 ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/1_child-12            	 8088014	       143.4  ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/2_children-12         	 5744546	       211.9  ns/op	       0 B/op	       0 allocs/op
Benchmark_HashNode/16_children-12        	 1000000	      1015    ns/op	       0 B/op	       0 allocs/op
```
*/

func Benchmark_EncodeDBNode(b *testing.B) {
	for _, benchmark := range encodeDBNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				encodeDBNode(benchmark.n)
			}
		})
	}
}

/*
Before:
```
Benchmark_EncodeDBNode/empty_node-12         	37080334	        32.68 ns/op	       2 B/op	       1 allocs/op
Benchmark_EncodeDBNode/has_value-12          	24886132	        49.37 ns/op	       8 B/op	       1 allocs/op
Benchmark_EncodeDBNode/1_child-12            	 6334178	       190.6  ns/op	      48 B/op	       1 allocs/op
Benchmark_EncodeDBNode/2_children-12         	 4398560	       276.7  ns/op	      96 B/op	       1 allocs/op
Benchmark_EncodeDBNode/16_children-12        	  712402	      1687    ns/op	     640 B/op	       1 allocs/op
```

After:
```
Benchmark_EncodeDBNode/empty_node-12         	45195682	        26.28 ns/op	       2 B/op	       1 allocs/op
Benchmark_EncodeDBNode/has_value-12          	30678374	        37.23 ns/op	       8 B/op	       1 allocs/op
Benchmark_EncodeDBNode/1_child-12            	 7456542	       160.9  ns/op	      48 B/op	       1 allocs/op
Benchmark_EncodeDBNode/2_children-12         	 5145237	       234.2  ns/op	      96 B/op	       1 allocs/op
Benchmark_EncodeDBNode/16_children-12        	  873981	      1357    ns/op	     640 B/op	       1 allocs/op
```
*/

func Benchmark_DecodeDBNode(b *testing.B) {
	for _, benchmark := range encodeDBNodeTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				var n dbNode
				err := decodeDBNode(benchmark.expectedBytes, &n)
				require.NoError(b, err)
			}
		})
	}
}

/*
Before:
```
Benchmark_DecodeDBNode/empty_node-12         	 3798781	       319.1 ns/op	      96 B/op	       2 allocs/op
Benchmark_DecodeDBNode/has_value-12          	 3377623	       354.7 ns/op	     101 B/op	       3 allocs/op
Benchmark_DecodeDBNode/1_child-12            	 1868557	       636.2 ns/op	     296 B/op	       7 allocs/op
Benchmark_DecodeDBNode/2_children-12         	 1424244	       834.9 ns/op	     400 B/op	      11 allocs/op
Benchmark_DecodeDBNode/16_children-12        	  322118	      3703   ns/op	    2008 B/op	      52 allocs/op
```

After:
```
Benchmark_DecodeDBNode/empty_node-12         	 4664726	       258.2 ns/op	      48 B/op	       1 allocs/op
Benchmark_DecodeDBNode/has_value-12          	 4133185	       294.4 ns/op	      56 B/op	       2 allocs/op
Benchmark_DecodeDBNode/1_child-12            	 2417180	       504.6 ns/op	     216 B/op	       4 allocs/op
Benchmark_DecodeDBNode/2_children-12         	 1914547	       623.8 ns/op	     288 B/op	       6 allocs/op
Benchmark_DecodeDBNode/16_children-12        	  543964	      2535   ns/op	    1434 B/op	      19 allocs/op
```
*/

func Benchmark_EncodeKey(b *testing.B) {
	for _, benchmark := range encodeKeyTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				encodeKey(benchmark.key)
			}
		})
	}
}

/*
Before:
```
Benchmark_EncodeKey/empty-12         	46250646	        25.21 ns/op	       1 B/op	       1 allocs/op
Benchmark_EncodeKey/1_byte-12        	44681088	        27.93 ns/op	       2 B/op	       1 allocs/op
Benchmark_EncodeKey/2_bytes-12       	44105148	        28.67 ns/op	       3 B/op	       1 allocs/op
Benchmark_EncodeKey/4_bytes-12       	41435211	        29.48 ns/op	       5 B/op	       1 allocs/op
Benchmark_EncodeKey/8_bytes-12       	32166406	        38.54 ns/op	      16 B/op	       1 allocs/op
Benchmark_EncodeKey/32_bytes-12      	18818768	        59.47 ns/op	      48 B/op	       1 allocs/op
Benchmark_EncodeKey/64_bytes-12      	12397254	        91.52 ns/op	      80 B/op	       1 allocs/op
Benchmark_EncodeKey/1024_bytes-12    	 1252498	       943.9  ns/op	    1152 B/op	       1 allocs/op
```

After:
```
Benchmark_EncodeKey/empty-12         	64515549	        18.19 ns/op	       1 B/op	       1 allocs/op
Benchmark_EncodeKey/1_byte-12        	57745863	        20.25 ns/op	       2 B/op	       1 allocs/op
Benchmark_EncodeKey/2_bytes-12       	60071084	        21.66 ns/op	       3 B/op	       1 allocs/op
Benchmark_EncodeKey/4_bytes-12       	54326704	        22.40 ns/op	       5 B/op	       1 allocs/op
Benchmark_EncodeKey/8_bytes-12       	39456540	        31.78 ns/op	      16 B/op	       1 allocs/op
Benchmark_EncodeKey/32_bytes-12      	22716714	        54.88 ns/op	      48 B/op	       1 allocs/op
Benchmark_EncodeKey/64_bytes-12      	15396968	        78.62 ns/op	      80 B/op	       1 allocs/op
Benchmark_EncodeKey/1024_bytes-12    	 1500788	       818.5  ns/op	    1152 B/op	       1 allocs/op
```
*/

func Benchmark_DecodeKey(b *testing.B) {
	for _, benchmark := range encodeKeyTests {
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err := decodeKey(benchmark.expectedBytes)
				require.NoError(b, err)
			}
		})
	}
}

/*
Before:
```
Benchmark_DecodeKey/empty-12         	 4698616	       254.6 ns/op	      48 B/op	       1 allocs/op
Benchmark_DecodeKey/1_byte-12        	 4528935	       269.5 ns/op	      49 B/op	       2 allocs/op
Benchmark_DecodeKey/2_bytes-12       	 4270620	       281.5 ns/op	      52 B/op	       3 allocs/op
Benchmark_DecodeKey/4_bytes-12       	 4181996	       289.0 ns/op	      56 B/op	       3 allocs/op
Benchmark_DecodeKey/8_bytes-12       	 4075734	       297.0 ns/op	      64 B/op	       3 allocs/op
Benchmark_DecodeKey/32_bytes-12      	 3519122	       343.1 ns/op	     112 B/op	       3 allocs/op
Benchmark_DecodeKey/64_bytes-12      	 3218776	       374.2 ns/op	     176 B/op	       3 allocs/op
Benchmark_DecodeKey/1024_bytes-12    	  840715	      1628   ns/op	    2096 B/op	       3 allocs/op
```

After:
```
Benchmark_DecodeKey/empty-12         	 6134431	       193.4 ns/op	       0 B/op	       0 allocs/op
Benchmark_DecodeKey/1_byte-12        	 6164746	       195.0 ns/op	       0 B/op	       0 allocs/op
Benchmark_DecodeKey/2_bytes-12       	 5763130	       208.3 ns/op	       2 B/op	       1 allocs/op
Benchmark_DecodeKey/4_bytes-12       	 5699397	       210.0 ns/op	       4 B/op	       1 allocs/op
Benchmark_DecodeKey/8_bytes-12       	 5605557	       214.6 ns/op	       8 B/op	       1 allocs/op
Benchmark_DecodeKey/32_bytes-12      	 5206342	       230.3 ns/op	      32 B/op	       1 allocs/op
Benchmark_DecodeKey/64_bytes-12      	 4850040	       247.1 ns/op	      64 B/op	       1 allocs/op
Benchmark_DecodeKey/1024_bytes-12    	 1431144	       867.9 ns/op	    1024 B/op	       1 allocs/op
```
*/

func Benchmark_EncodeUint(b *testing.B) {
	w := codecWriter{
		b: make([]byte, 0, binary.MaxVarintLen64),
	}

	for _, v := range []uint64{0, 1, 2, 32, 1024, 32768} {
		b.Run(strconv.FormatUint(v, 10), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				w.Uvarint(v)
				w.b = w.b[:0]
			}
		})
	}
}

/*
Before:
```
Benchmark_EncodeUint/0-12  				181291690	         6.659 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/1-12  				179944891	         6.607 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/2-12  				184202994	         6.509 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/32-12 				185757315	         6.527 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/1024-12         	184936735	         6.668 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/32768-12        	167698995	         7.095 ns/op	       0 B/op	       0 allocs/op
```

After:
```
Benchmark_EncodeUint/0-12  				552061826	         2.160 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/1-12  				559716720	         2.148 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/2-12  				553571922	         2.163 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/32-12 				560677795	         2.138 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/1024-12         	469645950	         2.546 ns/op	       0 B/op	       0 allocs/op
Benchmark_EncodeUint/32768-12        	399744884	         3.039 ns/op	       0 B/op	       0 allocs/op
```
*/
