// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"testing"
)

func TestEncodingManager(t *testing.T) {
	m, err := NewEncodingManager(HexEncoding)
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

	hex2, err := m.GetEncoder(HexEncoding)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hex2.(*Hex); !ok {
		t.Fatal("Encoding manager returned the wrong default encoding when HexEncoding was specified")
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
