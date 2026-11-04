// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// utxoBackend is a [SignerBackend] that serves UTXOs by their ID.
type utxoBackend map[ids.ID]*avax.UTXO

func (b utxoBackend) GetUTXO(_ context.Context, _, utxoID ids.ID) (*avax.UTXO, error) {
	utxo, ok := b[utxoID]
	if !ok {
		return nil, database.ErrNotFound
	}
	return utxo, nil
}

// TestSignRefreshesIDAndBytes verifies that adding signatures to a tx is
// reflected by [tx.Tx.ID] and [tx.Tx.Bytes] even if they were called before
// signing, as happens when a tx is signed by multiple parties.
func TestSignRefreshesIDAndBytes(t *testing.T) {
	var (
		keys = []*secp256k1fx.Keychain{
			secp256k1fx.NewKeychain(txtest.NewKey(t)),
			secp256k1fx.NewKeychain(txtest.NewKey(t)),
		}
		assetID = ids.GenerateTestID()
		utxos   = make(utxoBackend)
		imports []*avax.TransferableInput
		exports []tx.Input
	)
	for _, kc := range keys {
		utxo := txtest.NewUTXO(1, assetID, kc.Keys[0].Address())
		utxos[utxo.InputID()] = utxo
		imports = append(imports, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: 1,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
			},
		})
		exports = append(exports, tx.Input{
			Address: kc.Keys[0].EthAddress(),
			Amount:  1,
			AssetID: assetID,
		})
	}

	tests := []struct {
		name     string
		unsigned tx.Unsigned
	}{
		{
			name: "import",
			unsigned: &tx.Import{
				ImportedInputs: imports,
			},
		},
		{
			name: "export",
			unsigned: &tx.Export{
				Ins: exports,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stx := &tx.Tx{Unsigned: test.unsigned}
			// Each signer only holds one of the required keys, so the first
			// signs the tx partially and the second completes it.
			for _, kc := range keys {
				before := stx.ID()
				signer := NewSigner(kc, kc, utxos)
				require.NoErrorf(t, signer.Sign(t.Context(), stx), "%T.Sign()", signer)

				uncached := &tx.Tx{
					Unsigned: stx.Unsigned,
					Creds:    stx.Creds,
				}
				want, err := uncached.Bytes()
				require.NoErrorf(t, err, "%T.Bytes()", uncached)
				got, err := stx.Bytes()
				require.NoErrorf(t, err, "%T.Bytes()", stx)
				assert.Equalf(t, want, got, "%T.Bytes()", stx)
				assert.Equalf(t, uncached.ID(), stx.ID(), "%T.ID()", stx)
				assert.NotEqualf(t, before, stx.ID(), "%T.ID() did not change after signing", stx)
			}
		})
	}
}
