// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestCB58(t *testing.T) {
	addr := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	result, err := cb58Encoder{}.ConvertBytes(addr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "1NVSVezva3bAtJesnUj"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

func TestCB58Single(t *testing.T) {
	addr := []byte{0}
	result, err := cb58Encoder{}.ConvertBytes(addr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "1c7hwa"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
}

// Ensure we don't get an error when we encode as a string
// a byte slice that is the maximum length, but we do when it's
// greater than the maximum length
func TestCB58TooLarge(t *testing.T) {
	b := make([]byte, maxCB58Size)
	_, err := cb58Encoder{}.ConvertBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	b = make([]byte, maxCB58Size+1)
	_, err = cb58Encoder{}.ConvertBytes(b)
	if err == nil {
		t.Fatal("should have failed due to too large")
	}
}

// Test that we can stringify byte slice whose length is that of an ID
func TestCB58ID(t *testing.T) {
	id := make([]byte, hashing.HashLen)
	_, err := cb58Encoder{}.ConvertBytes(id)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCB58ParseBytes(t *testing.T) {
	ui := "1NVSVezva3bAtJesnUj"
	b, err := cb58Encoder{}.ConvertString(ui)
	if err != nil {
		t.Fatalf("Failed to process %s", ui)
	}
	expected := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}
	if !bytes.Equal(b, expected) {
		t.Fatalf("Expected 0x%x, got 0x%x", expected, b)
	}
}

func TestCB58ParseBytesSingle(t *testing.T) {
	ui := "1c7hwa"
	b, err := cb58Encoder{}.ConvertString(ui)
	if err != nil {
		t.Fatalf("Failed to process %s", ui)
	}
	expected := []byte{0}
	if !bytes.Equal(b, expected) {
		t.Fatalf("Expected 0x%x, got 0x%x", expected, b)
	}
}

func TestCB58ParseBytesError(t *testing.T) {
	ui := "0"
	_, err := cb58Encoder{}.ConvertString(ui)
	if err == nil {
		t.Fatalf("should have errored while parsing %s", ui)
	}

	ui = "13pP7vbI"
	_, err = cb58Encoder{}.ConvertString(ui)
	if err == nil {
		t.Fatalf("should have errored while parsing %s", ui)
	}

	ui = "13"
	_, err = cb58Encoder{}.ConvertString(ui)
	if err == nil {
		t.Fatalf("should have errored while parsing %s", ui)
	}

	ui = "13pP7vb3"
	_, err = cb58Encoder{}.ConvertString(ui)
	if err == nil {
		t.Fatalf("should have errored while parsing %s", ui)
	}
}
