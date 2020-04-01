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

func TestCB58UnmarshalJSON(t *testing.T) {
	expected := CB58{[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}}
	cb58 := CB58{}
	err := cb58.UnmarshalJSON([]byte("\"1NVSVezva3bAtJesnUj\""))
	if err != nil {
		t.Fatalf("CB58.UnmarshalJSON unexpected error unmarshalling: %s", err)
	} else if !bytes.Equal(cb58.Bytes, expected.Bytes) {
		t.Fatalf("CB58.UnmarshalJSON got 0x%x, expected 0x%x", cb58, expected)
	}
}

func TestCB58UnmarshalJSONNull(t *testing.T) {
	cb58 := CB58{}
	err := cb58.UnmarshalJSON([]byte("null"))
	if err != nil {
		t.Fatalf("CB58.UnmarshalJSON unexpected error unmarshalling null: %s", err)
	}
}

func TestCB58UnmarshalJSONError(t *testing.T) {
	tests := []struct {
		in       string
		expected error
	}{
		{"", errMissingQuotes},
		{"\"foo", errMissingQuotes},
		{"foo", errMissingQuotes},
		{"foo\"", errMissingQuotes},
		{"\"foo\"", errMissingChecksum},
		{"\"foobar\"", errBadChecksum},
	}
	cb58 := CB58{}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			err := cb58.UnmarshalJSON([]byte(tt.in))
			if err != tt.expected {
				t.Errorf("got error %q, expected error %q", err, tt.expected)
			}
		})
	}
}

func TestCB58MarshalJSONError(t *testing.T) {
	cb58 := CB58{[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255}}
	expected := []byte("\"1NVSVezva3bAtJesnUj\"")
	result, err := cb58.MarshalJSON()
	if err != nil {
		t.Fatalf("CB58.MarshalJSON unexpected error: %s", err)
	} else if !bytes.Equal(result, expected) {
		t.Fatalf("CB58.MarshalJSON got %q, expected %q", result, expected)
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
