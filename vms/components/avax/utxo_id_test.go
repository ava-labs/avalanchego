// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/codec"
)

func TestUTXOIDVerifyNil(t *testing.T) {
	utxoID := (*UTXOID)(nil)

	if err := utxoID.Verify(); err == nil {
		t.Fatalf("Should have errored due to a nil utxo ID")
	}
}

func TestUTXOIDVerifyEmpty(t *testing.T) {
	utxoID := &UTXOID{}

	if err := utxoID.Verify(); err == nil {
		t.Fatalf("Should have errored due to an empty utxo ID")
	}
}

func TestUTXOID(t *testing.T) {
	c := codec.NewDefault()

	utxoID := UTXOID{
		TxID: ids.NewID([32]byte{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
			0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		}),
		OutputIndex: 0x20212223,
	}

	if err := utxoID.Verify(); err != nil {
		t.Fatal(err)
	}

	bytes, err := c.Marshal(&utxoID)
	if err != nil {
		t.Fatal(err)
	}

	newUTXOID := UTXOID{}
	if err := c.Unmarshal(bytes, &newUTXOID); err != nil {
		t.Fatal(err)
	}

	if err := newUTXOID.Verify(); err != nil {
		t.Fatal(err)
	}

	if !utxoID.InputID().Equals(newUTXOID.InputID()) {
		t.Fatalf("Parsing returned the wrong UTXO ID")
	}
}
