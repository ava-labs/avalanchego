// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodingMarshalJSON(t *testing.T) {
	require := require.New(t)

	enc := Hex
	jsonBytes, err := enc.MarshalJSON()
	require.NoError(err)
	require.JSONEq(`"hex"`, string(jsonBytes))
}

func TestEncodingUnmarshalJSON(t *testing.T) {
	require := require.New(t)

	jsonBytes := []byte(`"hex"`)
	var enc Encoding
	require.NoError(json.Unmarshal(jsonBytes, &enc))
	require.Equal(Hex, enc)

	var serr *json.SyntaxError
	jsonBytes = []byte("")
	require.ErrorAs(json.Unmarshal(jsonBytes, &enc), &serr)

	jsonBytes = []byte(`""`)
	err := json.Unmarshal(jsonBytes, &enc)
	require.ErrorIs(err, errInvalidEncoding)
}

func TestEncodingString(t *testing.T) {
	enc := Hex
	require.Equal(t, "hex", enc.String())
}

// Test encoding bytes to a string and decoding back to bytes
func TestEncodeDecode(t *testing.T) {
	require := require.New(t)

	type test struct {
		encoding Encoding
		bytes    []byte
		str      string
	}

	id := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	tests := []test{
		{
			Hex,
			[]byte{},
			"0x7852b855",
		},
		{
			Hex,
			[]byte{0},
			"0x0017afa01d",
		},
		{
			Hex,
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255},
			"0x00010203040506070809ff4482539c",
		},
		{
			Hex,
			id[:],
			"0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20b7a612c9",
		},
	}

	for _, test := range tests {
		// Encode the bytes
		strResult, err := Encode(test.encoding, test.bytes)
		require.NoError(err)
		// Make sure the string repr. is what we expected
		require.Equal(test.str, strResult)
		// Decode the string
		bytesResult, err := Decode(test.encoding, strResult)
		require.NoError(err)
		// Make sure we got the same bytes back
		require.Equal(test.bytes, bytesResult)
	}
}

// Test that encoding nil bytes works
func TestEncodeNil(t *testing.T) {
	require := require.New(t)

	str, err := Encode(Hex, nil)
	require.NoError(err)
	require.Equal("0x7852b855", str)
}

func TestDecodeHexInvalid(t *testing.T) {
	tests := []struct {
		inputStr    string
		expectedErr error
	}{
		{
			inputStr:    "0",
			expectedErr: errMissingHexPrefix,
		},
		{
			inputStr:    "x",
			expectedErr: errMissingHexPrefix,
		},
		{
			inputStr:    "0xg",
			expectedErr: hex.InvalidByteError('g'),
		},
		{
			inputStr:    "0x0017afa0Zd",
			expectedErr: hex.InvalidByteError('Z'),
		},
		{
			inputStr:    "0xafafafafaf",
			expectedErr: errBadChecksum,
		},
	}
	for _, test := range tests {
		_, err := Decode(Hex, test.inputStr)
		require.ErrorIs(t, err, test.expectedErr)
	}
}

func TestDecodeNil(t *testing.T) {
	require := require.New(t)
	result, err := Decode(Hex, "")
	require.NoError(err)
	require.Empty(result)
}

func FuzzEncodeDecode(f *testing.F) {
	f.Fuzz(func(t *testing.T, bytes []byte) {
		require := require.New(t)

		str, err := Encode(Hex, bytes)
		require.NoError(err)

		decoded, err := Decode(Hex, str)
		require.NoError(err)

		require.Equal(bytes, decoded)
	})
}
