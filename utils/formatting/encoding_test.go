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

func TestEncodingManager(t *testing.T) {
	m, err := NewEncodingManager(Hex)
	if err != nil {
		t.Fatal(err)
	}

	/* TODO do we need this
	hex, err := m.GetEncoder("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hex.(*Hex); !ok {
		t.Fatal("Encoding manager returned the wrong default encoding when nothing was specified")
	}
	*/

	hex2, err := m.GetEncoder(Hex)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hex2.(*hexEncoder); !ok {
		t.Fatal("Encoding manager returned the wrong default encoder when Hex was specified")
	}

	encoder, err := m.GetEncoder(CB58)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := encoder.(*cb58Encoder); !ok {
		t.Fatal("Encoding manager returned the wrong encoding when CB58 was specified")
	}

	/* TODO do we need this
	if _, err := m.GetEncoder("gibberish"); err == nil {
		t.Fatal("Should have errored getting unknown encoding")
	}
	*/
}

/* TODO do we need this
func TestEncodingManagerBadDefault(t *testing.T) {
	if _, err := NewEncodingManager("snowflake"); err == nil {
		t.Fatal("Should have errored creating an encoding manager with an unknown default")
	}
}
*/
