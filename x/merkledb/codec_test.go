// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"math/rand"
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

			codec := codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodeBool(reader)
			if err != nil {
				t.SkipNow()
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			codec.encodeBool(&buf, got)
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

			codec := codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodeUint(reader)
			if err != nil {
				t.SkipNow()
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			codec.encodeUint(&buf, got)
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
			codec := codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodeKey(reader)
			if err != nil {
				t.SkipNow()
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			codec.encodeKey(&buf, got)
			bufBytes := buf.Bytes()
			require.Len(bufBytes, numRead)
			require.Equal(b[:numRead], bufBytes)
		},
	)
}

// Ensure that encodeHashValues is deterministic
func FuzzEncodeHashValues(f *testing.F) {
	codec1 := newCodec()
	codec2 := newCodec()

	f.Fuzz(
		func(
			t *testing.T,
			randSeed int,
		) {
			require := require.New(t)
			for _, bf := range validBranchFactors { // Create a random *hashValues
				r := rand.New(rand.NewSource(int64(randSeed))) // #nosec G404

				children := map[byte]child{}
				numChildren := r.Intn(int(bf)) // #nosec G404
				for i := 0; i < numChildren; i++ {
					compressedKeyLen := r.Intn(32) // #nosec G404
					compressedKeyBytes := make([]byte, compressedKeyLen)
					_, _ = r.Read(compressedKeyBytes) // #nosec G404

					children[byte(i)] = child{
						compressedKey: ToKey(compressedKeyBytes),
						id:            ids.GenerateTestID(),
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

				// Serialize the *hashValues with both codecs
				hvBytes1 := codec1.encodeHashValues(ToKey(key), children, value)
				hvBytes2 := codec2.encodeHashValues(ToKey(key), children, value)

				// Make sure they're the same
				require.Equal(hvBytes1, hvBytes2)
			}
		},
	)
}

func TestCodecDecodeKeyLengthOverflowRegression(t *testing.T) {
	codec := codec.(*codecImpl)
	bytes := bytes.NewReader(binary.AppendUvarint(nil, math.MaxInt))
	_, err := codec.decodeKey(bytes)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
