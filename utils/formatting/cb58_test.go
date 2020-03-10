// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"testing"
)

func TestCB58(t *testing.T) {
	addr := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	result := CB58{addr}.String()
	expected := "1NVSVezva3bAtJesnUj"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestCB58Single(t *testing.T) {
	addr := []byte{0}
	result := CB58{addr}.String()
	expected := "1c7hwa"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestCB58ParseBytes(t *testing.T) {
	ui := "1NVSVezva3bAtJesnUj"
	cb58 := CB58{}
	err := cb58.FromString(ui)
	if err != nil {
		t.Fatalf("Failed to process %s", ui)
	}
	expected := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	if !bytes.Equal(cb58.Bytes, expected) {
		t.Fatalf("Expected 0x%x, got 0x%x", expected, cb58.Bytes)
	}
}

func TestCB58ParseBytesSingle(t *testing.T) {
	ui := "1c7hwa"
	cb58 := CB58{}
	err := cb58.FromString(ui)
	if err != nil {
		t.Fatalf("Failed to process %s", ui)
	}
	expected := []byte{0}
	if !bytes.Equal(cb58.Bytes, expected) {
		t.Fatalf("Expected 0x%x, got 0x%x", expected, cb58.Bytes)
	}
}

func TestCB58ParseBytesError(t *testing.T) {
	ui := "0"
	cb58 := CB58{}
	err := cb58.FromString(ui)
	if err == nil {
		t.Fatalf("Incorrectly parsed %s", ui)
	}

	ui = "13pP7vbI"
	err = cb58.FromString(ui)
	if err == nil {
		t.Fatalf("Incorrectly parsed %s", ui)
	}

	ui = "13"
	err = cb58.FromString(ui)
	if err == nil {
		t.Fatalf("Incorrectly parsed %s", ui)
	}

	ui = "13pP7vb3"
	err = cb58.FromString(ui)
	if err == nil {
		t.Fatalf("Incorrectly parsed %s", ui)
	}
}
