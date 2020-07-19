// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestTxNil(t *testing.T) {
	c := codec.NewDefault()
	tx := (*Tx)(nil)
	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 1); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
	if err := tx.SemanticVerify(nil, nil); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
}

func setupCodec() codec.Codec {
	c := codec.NewDefault()
	c.RegisterType(&BaseTx{})
	c.RegisterType(&CreateAssetTx{})
	c.RegisterType(&OperationTx{})
	c.RegisterType(&ImportTx{})
	c.RegisterType(&ExportTx{})
	c.RegisterType(&secp256k1fx.TransferInput{})
	c.RegisterType(&secp256k1fx.MintOutput{})
	c.RegisterType(&secp256k1fx.TransferOutput{})
	c.RegisterType(&secp256k1fx.MintOperation{})
	c.RegisterType(&secp256k1fx.Credential{})
	return c
}

func TestTxEmpty(t *testing.T) {
	c := setupCodec()
	tx := &Tx{}
	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 1); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
}

func TestTxInvalidCredential(t *testing.T) {
	c := setupCodec()
	c.RegisterType(&ava.TestVerifiable{})

	tx := &Tx{
		UnsignedTx: &BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*ava.TransferableInput{{
				UTXOID: ava.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 0,
				},
				Asset: ava.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 20 * units.KiloAva,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			}},
		},
		Creds: []verify.Verifiable{&ava.TestVerifiable{Err: errors.New("")}},
	}

	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid credential")
	}
}

func TestTxInvalidUnsignedTx(t *testing.T) {
	c := setupCodec()
	c.RegisterType(&ava.TestVerifiable{})

	tx := &Tx{
		UnsignedTx: &BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*ava.TransferableInput{
				{
					UTXOID: ava.UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: ava.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAva,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
				{
					UTXOID: ava.UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: ava.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAva,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
			},
		},
		Creds: []verify.Verifiable{
			&ava.TestVerifiable{},
			&ava.TestVerifiable{},
		},
	}

	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid unsigned tx")
	}
}

func TestTxInvalidNumberOfCredentials(t *testing.T) {
	c := setupCodec()
	c.RegisterType(&ava.TestVerifiable{})

	tx := &Tx{
		UnsignedTx: &BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*ava.TransferableInput{
				{
					UTXOID: ava.UTXOID{TxID: ids.Empty, OutputIndex: 0},
					Asset:  ava.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAva,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
				{
					UTXOID: ava.UTXOID{TxID: ids.Empty, OutputIndex: 1},
					Asset:  ava.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAva,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
			},
		},
		Creds: []verify.Verifiable{&ava.TestVerifiable{}},
	}

	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid unsigned tx")
	}
}
