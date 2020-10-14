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

	hex, err := m.GetEncoding("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hex.(*Hex); !ok {
		t.Fatal("Encoding manager returned the wrong default encoding when nothing was specified")
	}

	hex2, err := m.GetEncoding(HexEncoding)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hex2.(*Hex); !ok {
		t.Fatal("Encoding manager returned the wrong default encoding when HexEncoding was specified")
	}

	cb58, err := m.GetEncoding(CB58Encoding)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cb58.(*CB58); !ok {
		t.Fatal("Encoding manager returned the wrong encoding when CB58Encoding was specified")
	}

	if _, err := m.GetEncoding("gibberish"); err == nil {
		t.Fatal("Should have errored getting unknown encoding")
	}
}

func TestEncodingManagerBadDefault(t *testing.T) {
	if _, err := NewEncodingManager("snowflake"); err == nil {
		t.Fatal("Should have errored creating an encoding manager with an unknown default")
	}
}
