// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
