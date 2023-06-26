// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodingMarshalJSON(t *testing.T) {
	enc := Hex
	jsonBytes, err := enc.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(jsonBytes) != `"hex"` {
		t.Fatal("should be 'hex'")
	}
}

func TestEncodingUnmarshalJSON(t *testing.T) {
	jsonBytes := []byte(`"hex"`)
	var enc Encoding
	if err := json.Unmarshal(jsonBytes, &enc); err != nil {
		t.Fatal(err)
	}
	if enc != Hex {
		t.Fatal("should be hex")
	}

	jsonBytes = []byte("")
	if err := json.Unmarshal(jsonBytes, &enc); err == nil {
		t.Fatal("should have erred due to invalid encoding")
	}

	jsonBytes = []byte(`""`)
	if err := json.Unmarshal(jsonBytes, &enc); err == nil {
		t.Fatal("should have erred due to invalid encoding")
	}
}

func TestEncodingString(t *testing.T) {
	enc := Hex
	require.Equal(t, enc.String(), "hex")
}

// Test encoding bytes to a string and decoding back to bytes
func TestEncodeDecode(t *testing.T) {
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
		if err != nil {
			t.Fatal(err)
		}
		// Make sure the string repr. is what we expected
		require.Equal(t, test.str, strResult)
		// Decode the string
		bytesResult, err := Decode(test.encoding, strResult)
		if err != nil {
			t.Fatal(err)
		}
		// Make sure we got the same bytes back
		require.Equal(t, test.bytes, bytesResult)
	}
}

// Test that encoding nil bytes works
func TestEncodeNil(t *testing.T) {
	str, err := Encode(Hex, nil)
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, "0x7852b855", str)
}

func TestDecodeHexInvalid(t *testing.T) {
	invalidHex := []string{"0", "x", "0xg", "0x0017afa0Zd", "0xafafafafaf"}
	for _, str := range invalidHex {
		_, err := Decode(Hex, str)
		if err == nil {
			t.Fatalf("should have failed to decode invalid hex '%s'", str)
		}
	}
}

func TestDecodeNil(t *testing.T) {
	if result, err := Decode(Hex, ""); err != nil || len(result) != 0 {
		t.Fatal("decoding the empty string should return an empty byte slice")
	}
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
