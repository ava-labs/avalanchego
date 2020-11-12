// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"testing"
)

func TestHex(t *testing.T) {
	addr := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	result, err := Hex{}.ConvertBytes(addr)
	if err != nil {
		t.Fatal()
	}
	expected := "0x00010203040506070809ff4482539c"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestHexSingle(t *testing.T) {
	addr := []byte{0}
	result, err := Hex{}.ConvertBytes(addr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "0x0017afa01d"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestHexFromStringInvalidCharacter(t *testing.T) {
	hexWrapper := Hex{}
	bytes, err := hexWrapper.ConvertString("0x0017afa0Zd") // Contains invalid character Z
	if err == nil {
		t.Fatalf("Should have errored reading invalid character 'Z', instead produced: 0x%x", bytes)
	}
}

func TestHexParseBytes(t *testing.T) {
	ui := "0x00010203040506070809ff4482539c"
	hexWrapper := Hex{}
	b, err := hexWrapper.ConvertString(ui)
	if err != nil {
		t.Fatalf("Failed to process %s", ui)
	}
	expected := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	if !bytes.Equal(b, expected) {
		t.Fatalf("Expected 0x%x, got 0x%x", expected, b)
	}
}
