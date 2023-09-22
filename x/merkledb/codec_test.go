// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"io"
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

func FuzzCodecPath(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
			branchFactorBit1 bool,
			branchFactorBit2 bool,
		) {
			require := require.New(t)
			branchFactor := BranchFactor2
			switch {
			case !branchFactorBit1 && !branchFactorBit2:
				branchFactor = BranchFactor2
			case !branchFactorBit1 && branchFactorBit2:
				branchFactor = BranchFactor4
			case branchFactorBit1 && !branchFactorBit2:
				branchFactor = BranchFactor16
			case branchFactorBit1 && branchFactorBit2:
				branchFactor = BranchFactor256
			}

			codec := codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodePath(reader, branchFactor)
			if err != nil {
				t.SkipNow()
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			codec.encodePath(&buf, got)
			bufBytes := buf.Bytes()
			require.Len(bufBytes, numRead)
			require.Equal(b[:numRead], bufBytes)
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

			codec := codec.(*codecImpl)
			node := &dbNode{}
			if err := codec.decodeDBNode(b, node); err != nil {
				t.SkipNow()
			}

			// Encoding [node] should be the same as [b].
			buf := codec.encodeDBNode(node)
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

			numChildren := r.Intn(int(BranchFactor16)) // #nosec G404

			children := map[byte]child{}
			for i := 0; i < numChildren; i++ {
				var childID ids.ID
				_, _ = r.Read(childID[:]) // #nosec G404

				childPathBytes := make([]byte, r.Intn(32)) // #nosec G404
				_, _ = r.Read(childPathBytes)              // #nosec G404

				children[byte(i)] = child{
					compressedPath: NewPath(childPathBytes, BranchFactor16),
					id:             childID,
				}
			}
			node := dbNode{
				value:    value,
				children: children,
			}

			nodeBytes := codec.encodeDBNode(&node)

			var gotNode dbNode
			require.NoError(codec.decodeDBNode(nodeBytes, &gotNode))
			require.Equal(node, gotNode)

			nodeBytes2 := codec.encodeDBNode(&gotNode)
			require.Equal(nodeBytes, nodeBytes2)
		},
	)
}

func TestCodecDecodeDBNode(t *testing.T) {
	require := require.New(t)

	var (
		parsedDBNode  dbNode
		tooShortBytes = make([]byte, minDBNodeLen-1)
	)
	err := codec.decodeDBNode(tooShortBytes, &parsedDBNode)
	require.ErrorIs(err, io.ErrUnexpectedEOF)

	proof := dbNode{
		value:    maybe.Some([]byte{1}),
		children: map[byte]child{},
	}

	nodeBytes := codec.encodeDBNode(&proof)
	// Remove num children (0) from end
	nodeBytes = nodeBytes[:len(nodeBytes)-minVarIntLen]
	proofBytesBuf := bytes.NewBuffer(nodeBytes)

	// Put num children NodeBranchFactor+1 at end
	codec.(*codecImpl).encodeUint(proofBytesBuf, uint64(BranchFactor16+1))

	err = codec.decodeDBNode(proofBytesBuf.Bytes(), &parsedDBNode)
	require.ErrorIs(err, errTooManyChildren)
}

// Ensure that encodeHashValues is deterministic
func FuzzEncodeHashValues(f *testing.F) {
	codec1 := newCodec(BranchFactor16)
	codec2 := newCodec(BranchFactor16)

	f.Fuzz(
		func(
			t *testing.T,
			randSeed int,
		) {
			require := require.New(t)

			// Create a random *hashValues
			r := rand.New(rand.NewSource(int64(randSeed))) // #nosec G404

			children := map[byte]child{}
			numChildren := r.Intn(int(BranchFactor16)) // #nosec G404
			for i := 0; i < numChildren; i++ {
				compressedPathLen := r.Intn(32) // #nosec G404
				compressedPathBytes := make([]byte, compressedPathLen)
				_, _ = r.Read(compressedPathBytes) // #nosec G404

				children[byte(i)] = child{
					compressedPath: NewPath(compressedPathBytes, BranchFactor16),
					id:             ids.GenerateTestID(),
					hasValue:       r.Intn(2) == 1, // #nosec G404
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

			hv := &hashValues{
				Children: children,
				Value:    value,
				Key:      NewPath(key, BranchFactor16),
			}

			// Serialize the *hashValues with both codecs
			hvBytes1 := codec1.encodeHashValues(hv)
			hvBytes2 := codec2.encodeHashValues(hv)

			// Make sure they're the same
			require.Equal(hvBytes1, hvBytes2)
		},
	)
}
