// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/json"
	"testing"

	"gotest.tools/assert"
)

func TestEncodingMarshalJSON(t *testing.T) {
	enc := Hex
	jsonBytes, err := enc.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(jsonBytes) != "\"hex\"" {
		t.Fatal("should be 'hex'")
	}

	enc2 := CB58
	jsonBytes, err = enc2.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(jsonBytes) != "\"cb58\"" {
		t.Fatal("should be 'cb58'")
	}
}

func TestEncodingUnmarshalJSON(t *testing.T) {
	jsonBytes := []byte("\"hex\"")
	var enc Encoding
	if err := json.Unmarshal(jsonBytes, &enc); err != nil {
		t.Fatal(err)
	}
	if enc != Hex {
		t.Fatal("should be hex")
	}

	jsonBytes = []byte("\"cb58\"")
	if err := json.Unmarshal(jsonBytes, &enc); err != nil {
		t.Fatal(err)
	}
	if enc != CB58 {
		t.Fatal("should be cb58")
	}

	jsonBytes = []byte("")
	if err := json.Unmarshal(jsonBytes, &enc); err == nil {
		t.Fatal("should have errored due to invalid encoding")
	}

	jsonBytes = []byte("\"\"")
	if err := json.Unmarshal(jsonBytes, &enc); err == nil {
		t.Fatal("should have errored due to invalid encoding")
	}
}

func TestEncodingString(t *testing.T) {
	enc := Hex
	assert.Equal(t, enc.String(), "hex")

	enc2 := CB58
	assert.Equal(t, enc2.String(), "cb58")
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
			CB58,
			[]byte{},
			"45PJLL",
		},
		{
			Hex,
			[]byte{},
			"0x7852b855",
		},
		{
			CB58,
			[]byte{0},
			"1c7hwa",
		},
		{
			Hex,
			[]byte{0},
			"0x0017afa01d",
		},
		{
			CB58,
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255},
			"1NVSVezva3bAtJesnUj",
		},
		{
			Hex,
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255},
			"0x00010203040506070809ff4482539c",
		},
		{
			CB58,
			id[:],
			"SkB92YpWm4Q2ijQHH34cqbKkCZWszsiQgHVjtNeFF2HdvDQU",
		},
		{
			Hex,
			id[:],
			"0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20b7a612c9",
		},
	}

	for _, test := range tests {
		// Encode the bytes
		strResult, err := EncodeWithChecksum(test.encoding, test.bytes)
		if err != nil {
			t.Fatal(err)
		}
		// Make sure the string repr. is what we expected
		assert.Equal(t, test.str, strResult)
		// Decode the string
		bytesResult, err := Decode(test.encoding, strResult)
		if err != nil {
			t.Fatal(err)
		}
		// Make sure we got the same bytes back
		assert.DeepEqual(t, test.bytes, bytesResult)
	}
}

// Test that encoding nil bytes works
func TestEncodeNil(t *testing.T) {
	str, err := EncodeWithChecksum(CB58, nil)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "45PJLL", str)

	str, err = EncodeWithChecksum(Hex, nil)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "0x7852b855", str)
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

/* TODO add this test back when we make [maxCB58Size]
a reasonable size and not math.MaxInt32

func TestEncodeCB58TooLarge(t *testing.T) {
	maxSizeBytes := make([]byte, maxCB58EncodeSize)
	if _, err := Encode(CB58, maxSizeBytes); err != nil {
		t.Fatal("should have been able to encode max size slice")
	}

	tooLargeBytes := make([]byte, maxCB58EncodeSize+1)
	if _, err := Encode(CB58, tooLargeBytes); err == nil {
		t.Fatal("should have errored due to too large")
	}

	if _, err := Encode(Hex, tooLargeBytes); err != nil {
		t.Fatal("size limit shouldn't apply to hex encoding")
	}
}

func TestDecodeCB58TooLarge(t *testing.T) {
	bytes := make([]byte, maxCB58EncodeSize+1)

	checked := make([]byte, len(bytes)+checksumLen)
	copy(checked, bytes)
	copy(checked[len(bytes):], hashing.Checksum(bytes, checksumLen))
	encoded := base58.Encode(checked)

	if _, err := Decode(CB58, encoded); err == nil {
		t.Fatal("should have errored due to too large")
	}
}
*/

func TestDecodeNil(t *testing.T) {
	if result, err := Decode(CB58, ""); result != nil || err != nil {
		t.Fatal("should both be nil")
	}
	if result, err := Decode(Hex, ""); result != nil || err != nil {
		t.Fatal("should both be nil")
	}
}
