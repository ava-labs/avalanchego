// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
)

// Ensure transaction verification fails when the transaction has the wrong
func TestVerifyTxWrongChainID(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5})
	// Create a tx with chainID [chainID]
	builder := Builder{
		NetworkID: 0,
		ChainID:   chainID,
	}
	tx, err := builder.NewTx(keys[0], 0, 1, keys[1].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}

	ctx := snow.DefaultContextTest()

	// Ensure that it fails verification when we try to verify it using
	// a different chain ID
	if err := tx.Verify(ctx); err != errWrongChainID {
		t.Fatalf("Should have failed with errWrongChainID")
	}
}

// Ensure transaction verification fails when the transaction has the wrong
func TestVerifyTxCorrectChainID(t *testing.T) {
	ctx := snow.DefaultContextTest()
	// Create a tx with chainID [chainID]
	builder := Builder{
		NetworkID: 0,
		ChainID:   ctx.ChainID,
	}
	tx, err := builder.NewTx(keys[0], 0, 1, keys[1].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}

	// Ensure it passes verification when we use correct chain ID
	if err := tx.Verify(ctx); err != nil {
		t.Fatalf("Should have passed verification")
	}
}
