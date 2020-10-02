// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"testing"
)

func TestHexCB58(t *testing.T) {
	addr := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	optWrapper := HexCB58{Bytes: addr}
	resultCB58 := optWrapper.String()
	expectedCB58 := "1NVSVezva3bAtJesnUj"
	if resultCB58 != expectedCB58 {
		t.Fatalf("Expected %s, got %s", expectedCB58, resultCB58)
	}
	optWrapper2 := HexCB58{}
	if err := optWrapper2.FromString(resultCB58); err != nil {
		t.Fatalf("Unexpected error reading CB58 string: %s", err)
	}
	if !bytes.Equal(optWrapper2.Bytes, addr) {
		t.Fatalf("Unexpected bytes value: 0x%x, expected: 0x%x", optWrapper2.Bytes, addr)
	}

	optWrapper.Hex = true
	resultHex := optWrapper.String()
	expectedHex := "0x00010203040506070809ff4482539c"
	if resultHex != expectedHex {
		t.Fatalf("Expected %s, got %s", expectedHex, resultHex)
	}
	optWrapper2 = HexCB58{}
	if err := optWrapper2.FromString(resultHex); err != nil {
		t.Fatalf("Unexpected error reading Hex string: %s", err)
	}
	if !bytes.Equal(optWrapper2.Bytes, addr) {
		t.Fatalf("Unexpected bytes value: 0x%x, expected: 0x%x", optWrapper2.Bytes, addr)
	}
}

func TestHexCB58Single(t *testing.T) {
	addr := []byte{0}
	optWrapper := HexCB58{Bytes: addr}
	resultCB58 := optWrapper.String()
	expectedCB58 := "1c7hwa"
	if resultCB58 != expectedCB58 {
		t.Fatalf("Expected %s, got %s", expectedCB58, resultCB58)
	}
	optWrapper2 := HexCB58{}
	if err := optWrapper2.FromString(resultCB58); err != nil {
		t.Fatalf("Unexpected error reading CB58 string: %s", err)
	}
	if !bytes.Equal(optWrapper2.Bytes, addr) {
		t.Fatalf("Unexpected bytes value: 0x%x, expected: 0x%x", optWrapper2.Bytes, addr)
	}

	optWrapper.Hex = true
	resultHex := optWrapper.String()
	expectedHex := "0x0017afa01d"
	if resultHex != expectedHex {
		t.Fatalf("Expected %s, got %s", expectedHex, resultHex)
	}
	optWrapper2 = HexCB58{}
	if err := optWrapper2.FromString(resultHex); err != nil {
		t.Fatalf("Unexpected error reading Hex string: %s", err)
	}
	if !bytes.Equal(optWrapper2.Bytes, addr) {
		t.Fatalf("Unexpected bytes value: 0x%x, expected: 0x%x", optWrapper2.Bytes, addr)
	}
}

func TestHexCB58UnmarshalJSON(t *testing.T) {
	expected := HexCB58{Bytes: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}}
	optWrapper := HexCB58{}
	err := optWrapper.UnmarshalJSON([]byte("\"0x00010203040506070809ff4482539c\""))
	if err != nil {
		t.Fatalf("HexCB58.UnmarshalJSON unexpected error unmarshalling hex: %s", err)
	} else if !bytes.Equal(optWrapper.Bytes, expected.Bytes) {
		t.Fatalf("HexCB58.UnmarshalJSON got 0x%x, expected 0x%x while unmarshalling hex", optWrapper.Bytes, expected.Bytes)
	}

	err = optWrapper.UnmarshalJSON([]byte("\"1NVSVezva3bAtJesnUj\""))
	if err != nil {
		t.Fatalf("HexCB58.UnmarshalJOSN unexpecteed error unmarshalling CB58: %s", err)
	} else if !bytes.Equal(optWrapper.Bytes, expected.Bytes) {
		t.Fatalf("HexCB58.UnmarshalJSON got 0x%x, expected 0x%x while unmarshalling CB58", optWrapper.Bytes, expected.Bytes)
	}
}

func TestHexCB58UnmarshalJSONNull(t *testing.T) {
	optWrapper := HexCB58{}
	err := optWrapper.UnmarshalJSON([]byte("null"))
	if err != nil {
		t.Fatalf("HexCB58.UnmarshalJSON unexpected error unmarshalling null: %s", err)
	}
}

func TestHexCB58UnmarshalJSONError(t *testing.T) {
	tests := []struct {
		in       string
		expected error
	}{
		{"", errMissingQuotes},
		{"\"0x0017afa01d\"", nil},
		{"\"1NVSVezva3bAtJesnUj\"", nil},
		{"\"0x0017afa01d", errMissingQuotes},
		{"0x0017afa01d\"", errMissingQuotes},
		{"\"1NVSVezva3bAtJesnUj", errMissingQuotes},
		{"1NVSVezva3bAtJesnUj\"", errMissingQuotes},
		{"\"0x0017afa0\"", errBadChecksum},
		{"\"0xabcdfe\"", errMissingChecksum},
	}
	optWrapper := HexCB58{}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			err := optWrapper.UnmarshalJSON([]byte(tt.in))
			if err != tt.expected {
				t.Errorf("got error %q, expected error %q", err, tt.expected)
			}
		})
	}
}

func TestHexCB58FromStringInvalidCharacter(t *testing.T) {
	optWrapper := HexCB58{}
	if err := optWrapper.FromString("0x0017afa0Zd"); err == nil {
		t.Fatalf("Should have errored reading invalid character 'Z', instead produced: 0x%x", optWrapper.Bytes)
	}

	if err := optWrapper.FromString("1NVSVezva3bAt&esnUj"); err == nil {
		t.Fatalf("Should have errored reading invalid character '&', instead produced: 0x%x", optWrapper.Bytes)
	}
}

func TestHexCB58MarshalJSON(t *testing.T) {
	optWrapper := HexCB58{Bytes: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}}
	expectedCB58 := []byte("\"1NVSVezva3bAtJesnUj\"")
	resultCB58, err := optWrapper.MarshalJSON()
	if err != nil {
		t.Fatalf("HexCB58.MarshalJSON unexpected error: %s", err)
	} else if !bytes.Equal(resultCB58, expectedCB58) {
		t.Fatalf("HexCB58.MarshalJSON got %q, expected %q", resultCB58, expectedCB58)
	}

	optWrapper.Hex = true
	expectedHex := []byte("\"0x00010203040506070809ff4482539c\"")
	resultHex, err := optWrapper.MarshalJSON()
	if err != nil {
		t.Fatalf("HexCB58.MarshalJSON unexpected error: %s", err)
	} else if !bytes.Equal(resultHex, expectedHex) {
		t.Fatalf("HexCB58.MarshalJSON got %q, expected %q", resultHex, expectedHex)
	}
}
