// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/hierarchycodec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func setupCodec() (codec.GeneralCodec, codec.Manager) {
	c := hierarchycodec.NewDefault()
	pre110Codec := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	errs := wrappers.Errs{}

	errs.Add(
		pre110Codec.RegisterType(&BaseTx{}),
		pre110Codec.RegisterType(&CreateAssetTx{}),
		pre110Codec.RegisterType(&OperationTx{}),
		pre110Codec.RegisterType(&ImportTx{}),
		pre110Codec.RegisterType(&ExportTx{}),
		pre110Codec.RegisterType(&secp256k1fx.TransferInput{}),
		pre110Codec.RegisterType(&secp256k1fx.MintOutput{}),
		pre110Codec.RegisterType(&secp256k1fx.TransferOutput{}),
		pre110Codec.RegisterType(&secp256k1fx.MintOperation{}),
		pre110Codec.RegisterType(&secp256k1fx.Credential{}),
		m.RegisterCodec(pre110CodecVersion, pre110Codec),

		c.RegisterType(&BaseTx{}),
		c.RegisterType(&CreateAssetTx{}),
		c.RegisterType(&OperationTx{}),
		c.RegisterType(&ImportTx{}),
		c.RegisterType(&ExportTx{}),
		c.RegisterType(&CreateManagedAssetTx{}),
	)
	c.NextGroup()
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.ManagedAssetStatusOutput{}),
		c.RegisterType(&secp256k1fx.UpdateManagedAssetOperation{}),
		m.RegisterCodec(currentCodecVersion, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	return c, m
}

func TestTxNil(t *testing.T) {
	ctx := NewContext(t)
	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	if err := m.RegisterCodec(currentCodecVersion, c); err != nil {
		t.Fatal(err)
	}

	tx := (*Tx)(nil)
	if err := tx.SyntacticVerify(ctx, m, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
	if err := tx.SemanticVerify(nil, nil); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
}

func TestTxEmpty(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()
	tx := &Tx{}
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
}

func TestTxInvalidCredential(t *testing.T) {
	ctx := NewContext(t)
	c, m := setupCodec()
	if err := c.RegisterType(&avax.TestVerifiable{}); err != nil {
		t.Fatal(err)
	}

	tx := &Tx{
		UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: 20 * units.KiloAvax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			}},
		}},
		Creds: []verify.Verifiable{&avax.TestVerifiable{Err: errors.New("")}},
	}
	if err := tx.SignSECP256K1Fx(m, currentCodecVersion, nil); err != nil {
		t.Fatal(err)
	}

	if err := tx.SyntacticVerify(ctx, m, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid credential")
	}
}

func TestTxInvalidUnsignedTx(t *testing.T) {
	ctx := NewContext(t)
	c, m := setupCodec()
	if err := c.RegisterType(&avax.TestVerifiable{}); err != nil {
		t.Fatal(err)
	}

	tx := &Tx{
		UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{
				{
					UTXOID: avax.UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: avax.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAvax,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
				{
					UTXOID: avax.UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: avax.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAvax,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
			},
		}},
		Creds: []verify.Verifiable{
			&avax.TestVerifiable{},
			&avax.TestVerifiable{},
		},
	}
	if err := tx.SignSECP256K1Fx(m, currentCodecVersion, nil); err != nil {
		t.Fatal(err)
	}

	if err := tx.SyntacticVerify(ctx, m, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid unsigned tx")
	}
}

func TestTxInvalidNumberOfCredentials(t *testing.T) {
	ctx := NewContext(t)
	c, m := setupCodec()
	if err := c.RegisterType(&avax.TestVerifiable{}); err != nil {
		t.Fatal(err)
	}

	tx := &Tx{
		UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{
				{
					UTXOID: avax.UTXOID{TxID: ids.Empty, OutputIndex: 0},
					Asset:  avax.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAvax,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
				{
					UTXOID: avax.UTXOID{TxID: ids.Empty, OutputIndex: 1},
					Asset:  avax.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAvax,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
			},
		}},
		Creds: []verify.Verifiable{&avax.TestVerifiable{}},
	}
	if err := tx.SignSECP256K1Fx(m, currentCodecVersion, nil); err != nil {
		t.Fatal(err)
	}

	if err := tx.SyntacticVerify(ctx, m, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid unsigned tx")
	}
}
