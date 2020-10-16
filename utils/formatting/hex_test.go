// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"testing"
)

func TestHex(t *testing.T) {
	addr := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	result := Hex{addr}.String()
	expected := "0x00010203040506070809ff4482539c"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestHexSingle(t *testing.T) {
	addr := []byte{0}
	result := Hex{addr}.String()
	expected := "0x0017afa01d"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestHexUnmarshalJSON(t *testing.T) {
	expected := Hex{[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}}
	hexWrapper := Hex{}
	err := hexWrapper.UnmarshalJSON([]byte("\"0x00010203040506070809ff4482539c\""))
	if err != nil {
		t.Fatalf("Hex.UnmarshalJSON unexpected error unmarshalling: %s", err)
	} else if !bytes.Equal(hexWrapper.Bytes, expected.Bytes) {
		t.Fatalf("Hex.UnmarshalJSON got 0x%x, expected 0x%x", hexWrapper, expected)
	}
}

func TestHexUnmarshalJSONNull(t *testing.T) {
	hexWrapper := Hex{}
	err := hexWrapper.UnmarshalJSON([]byte("null"))
	if err != nil {
		t.Fatalf("Hex.UnmarshalJSON unexpected error unmarshalling null: %s", err)
	}
}

func TestHexUnmarshalJSONError(t *testing.T) {
	tests := []struct {
		in       string
		expected error
	}{
		{"", errMissingQuotes},
		{"\"0x0017afa01d\"", nil},
		{"\"0x0017afa01d", errMissingQuotes},
		{"0x0017afa01d\"", errMissingQuotes},
		{"\"0017afa01d\"", errMissingHexPrefix},
		{"\"0x0017afa0\"", errBadChecksum},
		{"\"0xabcdfe\"", errMissingChecksum},
	}
	hexWrapper := Hex{}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			err := hexWrapper.UnmarshalJSON([]byte(tt.in))
			if err != tt.expected {
				t.Errorf("got error %q, expected error %q", err, tt.expected)
			}
		})
	}
}

func TestHexFromStringInvalidCharacter(t *testing.T) {
	hexWrapper := Hex{}
	err := hexWrapper.FromString("0x0017afa0Zd") // Contains invalid character Z
	if err == nil {
		t.Fatalf("Should have errored reading invalid character 'Z', instead produced: 0x%x", hexWrapper.Bytes)
	}
}

func TestHexMarshalJSONError(t *testing.T) {
	hexWrapper := Hex{[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}}
	expected := []byte("\"0x00010203040506070809ff4482539c\"")
	result, err := hexWrapper.MarshalJSON()
	if err != nil {
		t.Fatalf("Hex.MarshalJSON unexpected error: %s", err)
	} else if !bytes.Equal(result, expected) {
		t.Fatalf("Hex.MarshalJSON got %q, expected %q", result, expected)
	}
}

func TestHexParseBytes(t *testing.T) {
	ui := "0x00010203040506070809ff4482539c"
	hexWrapper := Hex{}
	err := hexWrapper.FromString(ui)
	if err != nil {
		t.Fatalf("Failed to process %s", ui)
	}
	expected := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	if !bytes.Equal(hexWrapper.Bytes, expected) {
		t.Fatalf("Expected 0x%x, got 0x%x", expected, hexWrapper.Bytes)
	}
}
