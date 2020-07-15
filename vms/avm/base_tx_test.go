// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"math"
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestBaseTxSerialization(t *testing.T) {
	expected := []byte{
		// txID:
		0x00, 0x00, 0x00, 0x00,
		// networkID:
		0x00, 0x00, 0xa8, 0x66,
		// blockchainID:
		0x05, 0x04, 0x03, 0x02, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// number of outs:
		0x00, 0x00, 0x00, 0x01,
		// output[0]:
		// assetID:
		0x01, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// fxID:
		0x00, 0x00, 0x00, 0x07,
		// secp256k1 Transferable Output:
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x39,
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// address[0]
		0xfc, 0xed, 0xa8, 0xf9, 0x0f, 0xcb, 0x5d, 0x30,
		0x61, 0x4b, 0x99, 0xd7, 0x9f, 0xc4, 0xba, 0xa2,
		0x93, 0x07, 0x76, 0x26,
		// number of inputs:
		0x00, 0x00, 0x00, 0x01,
		// txID:
		0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
		0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
		0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
		0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
		// utxo index:
		0x00, 0x00, 0x00, 0x01,
		// assetID:
		0x01, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// fxID:
		0x00, 0x00, 0x00, 0x05,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xd4, 0x31,
		// number of signatures:
		0x00, 0x00, 0x00, 0x01,
		// signature index[0]:
		0x00, 0x00, 0x00, 0x02,
	}

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	c := setupCodec()

	b, err := c.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	result := tx.Bytes()
	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}
}

func TestBaseTxGetters(t *testing.T) {
	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	txID := tx.ID()

	if assets := tx.AssetIDs(); assets.Len() != 1 {
		t.Fatalf("Wrong number of assets returned")
	} else if !assets.Contains(asset) {
		t.Fatalf("Wrong asset returned")
	} else if assets := tx.ConsumedAssetIDs(); assets.Len() != 1 {
		t.Fatalf("Wrong number of consumed assets returned")
	} else if !assets.Contains(asset) {
		t.Fatalf("Wrong consumed asset returned")
	} else if utxos := tx.UTXOs(); len(utxos) != 1 {
		t.Fatalf("Wrong number of utxos returned")
	} else if utxo := utxos[0]; !utxo.TxID.Equals(txID) {
		t.Fatalf("Wrong tx ID returned")
	} else if utxoIndex := utxo.OutputIndex; utxoIndex != 0 {
		t.Fatalf("Wrong output index returned")
	} else if assetID := utxo.AssetID(); !assetID.Equals(asset) {
		t.Fatalf("Wrong asset ID returned")
	}
}

func TestBaseTxSyntacticVerify(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
}

func TestBaseTxSyntacticVerifyNil(t *testing.T) {
	c := setupCodec()

	tx := (*BaseTx)(nil)
	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Nil BaseTx should have errored")
	}
}

func TestBaseTxSyntacticVerifyWrongNetworkID(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: 0,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Wrong networkID should have errored")
	}
}

func TestBaseTxSyntacticVerifyWrongChainID(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  ids.Empty,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Wrong chain ID should have errored")
	}
}

func TestBaseTxSyntacticVerifyInvalidOutput(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs:  []*ava.TransferableOutput{nil},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Invalid output should have errored")
	}
}

func TestBaseTxSyntacticVerifyUnsortedOutputs(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{
			{
				Asset: ava.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
			{
				Asset: ava.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
		Ins: []*ava.TransferableInput{
			{
				UTXOID: ava.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 1,
				},
				Asset: ava.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			},
		},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Unsorted outputs should have errored")
	}
}

func TestBaseTxSyntacticVerifyInvalidInput(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{nil},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Invalid input should have errored")
	}
}

func TestBaseTxSyntacticVerifyInputOverflow(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{
			{
				UTXOID: ava.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: ava.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: math.MaxUint64,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			},
			{
				UTXOID: ava.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 1,
				},
				Asset: ava.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			},
		},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Input overflow should have errored")
	}
}

func TestBaseTxSyntacticVerifyOutputOverflow(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{
			{
				Asset: ava.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
			{
				Asset: ava.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: math.MaxUint64,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 1,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Output overflow should have errored")
	}
}

func TestBaseTxSyntacticVerifyInsufficientFunds(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: math.MaxUint64,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 1,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Insufficient funds should have errored")
	}
}

func TestBaseTxSyntacticVerifyUninitialized(t *testing.T) {
	c := setupCodec()

	tx := &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID: ids.NewID([32]byte{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				}),
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}

	if err := tx.SyntacticVerify(ctx, c, 0); err == nil {
		t.Fatalf("Uninitialized tx should have errored")
	}
}

func TestBaseTxSemanticVerify(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err != nil {
		t.Fatal(err)
	}
}

func TestBaseTxSemanticVerifyUnknownFx(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	vm.codec.RegisterType(&ava.TestVerifiable{})

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	tx.Creds = append(tx.Creds, &ava.TestVerifiable{})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("should have errored due to an unknown feature extension")
	}
}

func TestBaseTxSemanticVerifyWrongAssetID(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	vm.codec.RegisterType(&ava.TestVerifiable{})

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("should have errored due to an asset ID mismatch")
	}
}

func TestBaseTxSemanticVerifyUnauthorizedFx(t *testing.T) {
	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	err := vm.Initialize(
		ctx,
		memdb.New(),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty,
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.NewID([32]byte{1}),
				Fx: &testFx{},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	err = vm.Bootstrapping()
	if err != nil {
		t.Fatal(err)
	}

	err = vm.Bootstrapped()
	if err != nil {
		t.Fatal(err)
	}

	cr := codecRegistry{
		index:         1,
		typeToFxIndex: vm.typeToFxIndex,
		codec:         vm.codec,
	}

	cr.RegisterType(&ava.TestTransferable{})

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In:    &ava.TestTransferable{},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("should have errored due to an unsupported fx")
	}
}

func TestBaseTxSemanticVerifyInvalidSignature(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{{}},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Invalid credential should have failed verification")
	}
}

func TestBaseTxSemanticVerifyMissingUTXO(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Unknown UTXO should have failed verification")
	}
}

func TestBaseTxSemanticVerifyInvalidUTXO(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: math.MaxUint32,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Invalid UTXO should have failed verification")
	}
}

func TestBaseTxSemanticVerifyPendingInvalidUTXO(t *testing.T) {
	genesisBytes, issuer, vm := GenesisVM(t)

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	pendingTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&pendingTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	pendingTx.Creds = append(pendingTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(pendingTx)
	if err != nil {
		t.Fatal(err)
	}

	txID, err := vm.IssueTx(b, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()

	<-issuer

	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	vm.PendingTxs()

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: 2,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err = vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	sig, err = key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig = [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err = vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Invalid UTXO should have failed verification")
	}
}

func TestBaseTxSemanticVerifyPendingWrongAssetID(t *testing.T) {
	genesisBytes, issuer, vm := GenesisVM(t)

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	pendingTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&pendingTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	pendingTx.Creds = append(pendingTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(pendingTx)
	if err != nil {
		t.Fatal(err)
	}

	txID, err := vm.IssueTx(b, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()

	<-issuer

	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	vm.PendingTxs()

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err = vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	sig, err = key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig = [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err = vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Wrong asset ID should have failed verification")
	}
}

func TestBaseTxSemanticVerifyPendingUnauthorizedFx(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)

	ctx.Lock.Lock()

	vm := &VM{}
	err := vm.Initialize(
		ctx,
		memdb.New(),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.NewID([32]byte{1}),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty,
				Fx: &testFx{},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	err = vm.Bootstrapping()
	if err != nil {
		t.Fatal(err)
	}

	err = vm.Bootstrapped()
	if err != nil {
		t.Fatal(err)
	}

	cr := codecRegistry{
		index:         1,
		typeToFxIndex: vm.typeToFxIndex,
		codec:         vm.codec,
	}

	cr.RegisterType(&ava.TestVerifiable{})

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	pendingTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&pendingTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	pendingTx.Creds = append(pendingTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(pendingTx)
	if err != nil {
		t.Fatal(err)
	}

	txID, err := vm.IssueTx(b, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()

	<-issuer

	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	vm.PendingTxs()

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	tx.Creds = append(tx.Creds, &ava.TestVerifiable{})

	b, err = vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Unsupported feature extension should have failed verification")
	}
}

func TestBaseTxSemanticVerifyPendingInvalidSignature(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)

	ctx.Lock.Lock()

	vm := &VM{}
	err := vm.Initialize(
		ctx,
		memdb.New(),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.NewID([32]byte{1}),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty,
				Fx: &testFx{},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	err = vm.Bootstrapping()
	if err != nil {
		t.Fatal(err)
	}

	err = vm.Bootstrapped()
	if err != nil {
		t.Fatal(err)
	}

	cr := codecRegistry{
		index:         1,
		typeToFxIndex: vm.typeToFxIndex,
		codec:         vm.codec,
	}

	cr.RegisterType(&ava.TestVerifiable{})

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	pendingTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&pendingTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	pendingTx.Creds = append(pendingTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(pendingTx)
	if err != nil {
		t.Fatal(err)
	}

	txID, err := vm.IssueTx(b, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()

	<-issuer

	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	vm.PendingTxs()

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{{}},
	})

	b, err = vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	uTx := &UniqueTx{
		TxState: &TxState{
			Tx: tx,
		},
		vm:   vm,
		txID: tx.ID(),
	}

	if err := tx.UnsignedTx.SemanticVerify(vm, uTx, tx.Creds); err == nil {
		t.Fatalf("Invalid signature should have failed verification")
	}
}

func TestBaseTxSemanticVerifyMalformedOutput(t *testing.T) {
	_, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	txBytes := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xa8, 0x66,
		0x05, 0x04, 0x03, 0x02, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x70, 0xae, 0x33, 0xb5,
		0x60, 0x9c, 0xd8, 0x9a, 0x72, 0x92, 0x4f, 0xa2,
		0x88, 0x3f, 0x9b, 0xf1, 0xc6, 0xd8, 0x9f, 0x07,
		0x09, 0x9b, 0x2a, 0xd7, 0x1b, 0xe1, 0x7c, 0x5d,
		0x44, 0x93, 0x23, 0xdb, 0x00, 0x00, 0x00, 0x05,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc3, 0x50,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x70, 0xae, 0x33, 0xb5, 0x60, 0x9c, 0xd8, 0x9a,
		0x72, 0x92, 0x4f, 0xa2, 0x88, 0x3f, 0x9b, 0xf1,
		0xc6, 0xd8, 0x9f, 0x07, 0x09, 0x9b, 0x2a, 0xd7,
		0x1b, 0xe1, 0x7c, 0x5d, 0x44, 0x93, 0x23, 0xdb,
		0x00, 0x00, 0x00, 0x01, 0x70, 0xae, 0x33, 0xb5,
		0x60, 0x9c, 0xd8, 0x9a, 0x72, 0x92, 0x4f, 0xa2,
		0x88, 0x3f, 0x9b, 0xf1, 0xc6, 0xd8, 0x9f, 0x07,
		0x09, 0x9b, 0x2a, 0xd7, 0x1b, 0xe1, 0x7c, 0x5d,
		0x44, 0x93, 0x23, 0xdb, 0x00, 0x00, 0x00, 0x05,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc3, 0x50,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
		0x00, 0x00, 0x00, 0x01, 0x50, 0x6b, 0xd9, 0x2d,
		0xe5, 0xeb, 0xc2, 0xbf, 0x8f, 0xaa, 0xf1, 0x7d,
		0xbb, 0xae, 0xb3, 0xf3, 0x13, 0x9e, 0xae, 0xb4,
		0xad, 0x32, 0x95, 0x6e, 0x92, 0x74, 0xf9, 0x53,
		0x0e, 0xcc, 0x03, 0xd8, 0x02, 0xab, 0x1c, 0x16,
		0x52, 0xd0, 0xe3, 0xfc, 0xe5, 0x93, 0xa9, 0x8e,
		0x96, 0x1e, 0x83, 0xf0, 0x12, 0x27, 0x66, 0x9f,
		0x03, 0x56, 0x9f, 0x17, 0x1b, 0xd1, 0x22, 0x90,
		0xfd, 0x64, 0xf5, 0x73, 0x01,
	}

	tx := &Tx{}
	if err := vm.codec.Unmarshal(txBytes, tx); err == nil {
		t.Fatalf("should have failed to unmarshal the tx")
	}
}

func TestBaseTxNotState(t *testing.T) {
	intf := interface{}(&BaseTx{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
