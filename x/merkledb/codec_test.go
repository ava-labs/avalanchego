// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"io"
	"math/rand"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// TODO add more codec tests

func newRandomProofNode(r *rand.Rand) ProofNode {
	key := make([]byte, r.Intn(32)) // #nosec G404
	_, _ = r.Read(key)              // #nosec G404
	serializedKey := newPath(key).Serialize()

	val := make([]byte, r.Intn(64)) // #nosec G404
	_, _ = r.Read(val)              // #nosec G404

	children := map[byte]ids.ID{}
	for j := 0; j < NodeBranchFactor; j++ {
		if r.Float64() < 0.5 {
			var childID ids.ID
			_, _ = r.Read(childID[:]) // #nosec G404
			children[byte(j)] = childID
		}
	}

	hasValue := rand.Intn(2) == 1 // #nosec G404
	var valueOrHash Maybe[[]byte]
	if hasValue {
		// use the hash instead when length is greater than the hash length
		if len(val) >= HashLength {
			val = hashing.ComputeHash256(val)
		} else if len(val) == 0 {
			// We do this because when we encode a value of []byte{} we will later
			// decode it as nil.
			// Doing this prevents inconsistency when comparing the encoded and
			// decoded values.
			// Calling nilEmptySlices doesn't set this because it is a private
			// variable on the struct
			val = nil
		}
		valueOrHash = Some(val)
	}

	return ProofNode{
		KeyPath:     serializedKey,
		ValueOrHash: valueOrHash,
		Children:    children,
	}
}

func nilEmptySlices(dest interface{}) {
	if dest == nil {
		return
	}

	destPtr := reflect.ValueOf(dest)
	if destPtr.Kind() != reflect.Ptr {
		return
	}
	nilEmptySlicesRec(destPtr.Elem())
}

func nilEmptySlicesRec(value reflect.Value) {
	switch value.Kind() {
	case reflect.Slice:
		if value.Len() == 0 {
			newValue := reflect.Zero(value.Type())
			value.Set(newValue)
			return
		}

		for i := 0; i < value.Len(); i++ {
			f := value.Index(i)
			nilEmptySlicesRec(f)
		}
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			f := value.Index(i)
			nilEmptySlicesRec(f)
		}
	case reflect.Interface, reflect.Ptr:
		if value.IsNil() {
			return
		}
		nilEmptySlicesRec(value.Elem())
	case reflect.Struct:
		t := value.Type()
		numFields := value.NumField()
		for i := 0; i < numFields; i++ {
			tField := t.Field(i)
			if tField.IsExported() {
				field := value.Field(i)
				nilEmptySlicesRec(field)
			}
		}
	}
}

func FuzzCodecBool(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)

			codec := Codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodeBool(reader)
			if err != nil {
				return
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			err = codec.encodeBool(&buf, got)
			require.NoError(err)
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

			codec := Codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodeInt(reader)
			if err != nil {
				return
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			err = codec.encodeInt(&buf, got)
			require.NoError(err)
			bufBytes := buf.Bytes()
			require.Len(bufBytes, numRead)
			require.Equal(b[:numRead], bufBytes)
		},
	)
}

func FuzzCodecSerializedPath(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)

			codec := Codec.(*codecImpl)
			reader := bytes.NewReader(b)
			startLen := reader.Len()
			got, err := codec.decodeSerializedPath(reader)
			if err != nil {
				return
			}
			endLen := reader.Len()
			numRead := startLen - endLen

			// Encoding [got] should be the same as [b].
			var buf bytes.Buffer
			err = codec.encodeSerializedPath(got, &buf)
			require.NoError(err)
			bufBytes := buf.Bytes()
			require.Len(bufBytes, numRead)
			require.Equal(b[:numRead], bufBytes)

			clonedGot := got.deserialize().Serialize()
			require.Equal(got, clonedGot)
		},
	)
}

func FuzzCodecProofCanonical(f *testing.F) {
	f.Add(
		[]byte{
			// RootID:
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			// Path:
			// Num proof nodes = 1
			0x02,
			// Key Path:
			// Nibble Length:
			0x00,
			// Value:
			// Has Value = false
			0x00,
			// Num Children = 2
			0x04,
			// Child 0:
			// index = 0
			0x00,
			// childID:
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			// Child 1:
			// index = 0 <- should fail
			0x00,
			// childID:
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			// Key:
			// length = 0
			0x00,
		},
	)
	f.Fuzz(
		func(
			t *testing.T,
			b []byte,
		) {
			require := require.New(t)

			codec := Codec.(*codecImpl)
			proof := &Proof{}
			got, err := codec.DecodeProof(b, proof)
			if err != nil {
				return
			}

			// Encoding [proof] should be the same as [b].
			buf, err := codec.EncodeProof(got, proof)
			require.NoError(err)
			require.Equal(b, buf)
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

			codec := Codec.(*codecImpl)
			node := &dbNode{}
			got, err := codec.decodeDBNode(b, node)
			if err != nil {
				return
			}

			// Encoding [node] should be the same as [b].
			buf, err := codec.encodeDBNode(got, node)
			require.NoError(err)
			require.Equal(b, buf)
		},
	)
}

func FuzzCodecProofDeterministic(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			randSeed int,
			key []byte,
			numProofNodes uint,
		) {
			require := require.New(t)

			r := rand.New(rand.NewSource(int64(randSeed))) // #nosec G404

			proofNodes := make([]ProofNode, numProofNodes)
			for i := range proofNodes {
				proofNodes[i] = newRandomProofNode(r)
			}

			proof := Proof{
				Path: proofNodes,
				Key:  key,
			}

			proofBytes, err := Codec.EncodeProof(Version, &proof)
			require.NoError(err)

			var gotProof Proof
			gotVersion, err := Codec.DecodeProof(proofBytes, &gotProof)
			require.NoError(err)
			require.Equal(Version, gotVersion)

			nilEmptySlices(&proof)
			nilEmptySlices(&gotProof)
			require.Equal(proof, gotProof)

			proofBytes2, err := Codec.EncodeProof(Version, &gotProof)
			require.NoError(err)
			require.Equal(proofBytes, proofBytes2)
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

			value := Nothing[[]byte]()
			if hasValue {
				if len(valueBytes) == 0 {
					// We do this because when we encode a value of []byte{}
					// we will later decode it as nil.
					// Doing this prevents inconsistency when comparing the
					// encoded and decoded values below.
					// Calling nilEmptySlices doesn't set this because it is a
					// private variable on the struct
					valueBytes = nil
				}
				value = Some(valueBytes)
			}

			numChildren := r.Intn(NodeBranchFactor) // #nosec G404

			children := map[byte]child{}
			for i := 0; i < numChildren; i++ {
				var childID ids.ID
				_, _ = r.Read(childID[:]) // #nosec G404

				childPathBytes := make([]byte, r.Intn(32)) // #nosec G404
				_, _ = r.Read(childPathBytes)              // #nosec G404

				children[byte(i)] = child{
					compressedPath: newPath(childPathBytes),
					id:             childID,
				}
			}
			node := dbNode{
				value:    value,
				children: children,
			}

			nodeBytes, err := Codec.encodeDBNode(Version, &node)
			require.NoError(err)

			var gotNode dbNode
			gotVersion, err := Codec.decodeDBNode(nodeBytes, &gotNode)
			require.NoError(err)
			require.Equal(Version, gotVersion)

			nilEmptySlices(&node)
			nilEmptySlices(&gotNode)
			require.Equal(node, gotNode)

			nodeBytes2, err := Codec.encodeDBNode(Version, &gotNode)
			require.NoError(err)
			require.Equal(nodeBytes, nodeBytes2)
		},
	)
}

func TestCodec_DecodeProof(t *testing.T) {
	require := require.New(t)

	_, err := Codec.DecodeProof([]byte{1}, nil)
	require.ErrorIs(err, errDecodeNil)

	var (
		proof         Proof
		tooShortBytes = make([]byte, minProofLen-1)
	)
	_, err = Codec.DecodeProof(tooShortBytes, &proof)
	require.ErrorIs(err, io.ErrUnexpectedEOF)
}

func TestCodec_DecodeDBNode(t *testing.T) {
	require := require.New(t)

	_, err := Codec.decodeDBNode([]byte{1}, nil)
	require.ErrorIs(err, errDecodeNil)

	var (
		parsedDBNode  dbNode
		tooShortBytes = make([]byte, minDBNodeLen-1)
	)
	_, err = Codec.decodeDBNode(tooShortBytes, &parsedDBNode)
	require.ErrorIs(err, io.ErrUnexpectedEOF)

	proof := dbNode{
		value:    Some([]byte{1}),
		children: map[byte]child{},
	}

	nodeBytes, err := Codec.encodeDBNode(Version, &proof)
	require.NoError(err)

	// Remove num children (0) from end
	nodeBytes = nodeBytes[:len(nodeBytes)-minVarIntLen]
	proofBytesBuf := bytes.NewBuffer(nodeBytes)
	// Put num children -1 at end
	err = Codec.(*codecImpl).encodeInt(proofBytesBuf, -1)
	require.NoError(err)

	_, err = Codec.decodeDBNode(proofBytesBuf.Bytes(), &parsedDBNode)
	require.ErrorIs(err, errNegativeNumChildren)

	// Remove num children from end
	nodeBytes = proofBytesBuf.Bytes()
	nodeBytes = nodeBytes[:len(nodeBytes)-minVarIntLen]
	proofBytesBuf = bytes.NewBuffer(nodeBytes)
	// Put num children NodeBranchFactor+1 at end
	err = Codec.(*codecImpl).encodeInt(proofBytesBuf, NodeBranchFactor+1)
	require.NoError(err)

	_, err = Codec.decodeDBNode(proofBytesBuf.Bytes(), &parsedDBNode)
	require.ErrorIs(err, errTooManyChildren)
}
