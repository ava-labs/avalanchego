// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestBaseTxSerialization(t *testing.T) {
	expected := []byte{
		// Codec version:
		0x00, 0x00,
		// txID:
		0x00, 0x00, 0x00, 0x00,
		// networkID:
		0x00, 0x00, 0x00, 0x0a,
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
		// Memo length:
		0x00, 0x00, 0x00, 0x04,
		// Memo:
		0x00, 0x01, 0x02, 0x03,
		// Number of credentials
		0x00, 0x00, 0x00, 0x00,
	}

	tx := &Tx{Unsigned: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
		Memo: []byte{0x00, 0x01, 0x02, 0x03},
	}}}

	c := setupCodec()
	if err := tx.Initialize(c); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, tx.ID().String(), "zeqT8FTnRAxes7QQQYkaWhNkHavd9d6aCdH8TQu2Mx5KEydEz")
	result := tx.Bytes()
	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}

	credBytes := []byte{
		// type id
		0x00, 0x00, 0x00, 0x09,

		// there are two signers (thus two signatures)
		0x00, 0x00, 0x00, 0x02,

		// 65 bytes
		0x7d, 0x89, 0x8e, 0xe9, 0x8a, 0xf8, 0x33, 0x5d, 0x37, 0xe6,
		0xfa, 0xda, 0x0c, 0xbb, 0x44, 0xa1, 0x44, 0x05, 0xd3, 0xbb,
		0x94, 0x0d, 0xfc, 0x0d, 0x99, 0xa6, 0xd3, 0xff, 0x5c, 0x71,
		0x5a, 0xff, 0x26, 0xd1, 0x84, 0x84, 0xf2, 0x9b, 0x28, 0x96,
		0x44, 0x96, 0x8f, 0xed, 0xff, 0xeb, 0x23, 0xe0, 0x30, 0x66,
		0x5d, 0x73, 0x6d, 0x94, 0xfc, 0x80, 0xbc, 0x73, 0x5f, 0x51,
		0xc8, 0x06, 0xd7, 0x43, 0x00,

		// 65 bytes
		0x7d, 0x89, 0x8e, 0xe9, 0x8a, 0xf8, 0x33, 0x5d, 0x37, 0xe6,
		0xfa, 0xda, 0x0c, 0xbb, 0x44, 0xa1, 0x44, 0x05, 0xd3, 0xbb,
		0x94, 0x0d, 0xfc, 0x0d, 0x99, 0xa6, 0xd3, 0xff, 0x5c, 0x71,
		0x5a, 0xff, 0x26, 0xd1, 0x84, 0x84, 0xf2, 0x9b, 0x28, 0x96,
		0x44, 0x96, 0x8f, 0xed, 0xff, 0xeb, 0x23, 0xe0, 0x30, 0x66,
		0x5d, 0x73, 0x6d, 0x94, 0xfc, 0x80, 0xbc, 0x73, 0x5f, 0x51,
		0xc8, 0x06, 0xd7, 0x43, 0x00,

		// type id
		0x00, 0x00, 0x00, 0x09,

		// there are two signers (thus two signatures)
		0x00, 0x00, 0x00, 0x02,

		// 65 bytes
		0x7d, 0x89, 0x8e, 0xe9, 0x8a, 0xf8, 0x33, 0x5d, 0x37, 0xe6,
		0xfa, 0xda, 0x0c, 0xbb, 0x44, 0xa1, 0x44, 0x05, 0xd3, 0xbb,
		0x94, 0x0d, 0xfc, 0x0d, 0x99, 0xa6, 0xd3, 0xff, 0x5c, 0x71,
		0x5a, 0xff, 0x26, 0xd1, 0x84, 0x84, 0xf2, 0x9b, 0x28, 0x96,
		0x44, 0x96, 0x8f, 0xed, 0xff, 0xeb, 0x23, 0xe0, 0x30, 0x66,
		0x5d, 0x73, 0x6d, 0x94, 0xfc, 0x80, 0xbc, 0x73, 0x5f, 0x51,
		0xc8, 0x06, 0xd7, 0x43, 0x00,

		// 65 bytes
		0x7d, 0x89, 0x8e, 0xe9, 0x8a, 0xf8, 0x33, 0x5d, 0x37, 0xe6,
		0xfa, 0xda, 0x0c, 0xbb, 0x44, 0xa1, 0x44, 0x05, 0xd3, 0xbb,
		0x94, 0x0d, 0xfc, 0x0d, 0x99, 0xa6, 0xd3, 0xff, 0x5c, 0x71,
		0x5a, 0xff, 0x26, 0xd1, 0x84, 0x84, 0xf2, 0x9b, 0x28, 0x96,
		0x44, 0x96, 0x8f, 0xed, 0xff, 0xeb, 0x23, 0xe0, 0x30, 0x66,
		0x5d, 0x73, 0x6d, 0x94, 0xfc, 0x80, 0xbc, 0x73, 0x5f, 0x51,
		0xc8, 0x06, 0xd7, 0x43, 0x00,
	}
	if err := tx.SignSECP256K1Fx(c, [][]*crypto.PrivateKeySECP256K1R{{keys[0], keys[0]}, {keys[0], keys[0]}}); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, tx.ID().String(), "QnTUuie2qe6BKyYrC2jqd73bJ828QNhYnZbdA2HWsnVRPjBfV")
	result = tx.Bytes()

	// there are two credentials
	expected[len(expected)-1] = 0x02
	expected = append(expected, credBytes...)
	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}
}

func TestBaseTxGetters(t *testing.T) {
	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if assets := tx.AssetIDs(); assets.Len() != 1 {
		t.Fatalf("Wrong number of assets returned")
	} else if !assets.Contains(assetID) {
		t.Fatalf("Wrong asset returned")
	} else if assets := tx.ConsumedAssetIDs(); assets.Len() != 1 {
		t.Fatalf("Wrong number of consumed assets returned")
	} else if !assets.Contains(assetID) {
		t.Fatalf("Wrong consumed asset returned")
	}
}

func TestBaseTxSyntacticVerify(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestBaseTxSyntacticVerifyMemoTooLarge(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
		Memo: make([]byte, avax.MaxMemoSize+1),
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatal("should have failed because memo is too large")
	}
}

func TestBaseTxSyntacticVerifyNil(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := (*BaseTx)(nil)
	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Nil BaseTx should have erred")
	}
}

func TestBaseTxSyntacticVerifyWrongNetworkID(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID + 1,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Wrong networkID should have erred")
	}
}

func TestBaseTxSyntacticVerifyWrongChainID(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID.Prefix(0),
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Wrong chain ID should have erred")
	}
}

func TestBaseTxSyntacticVerifyInvalidOutput(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs:         []*avax.TransferableOutput{nil},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 54321,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Invalid output should have erred")
	}
}

func TestBaseTxSyntacticVerifyUnsortedOutputs(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
			{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
		Ins: []*avax.TransferableInput{
			{
				UTXOID: avax.UTXOID{
					TxID: ids.ID{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					},
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			},
		},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Unsorted outputs should have erred")
	}
}

func TestBaseTxSyntacticVerifyInvalidInput(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{nil},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Invalid input should have erred")
	}
}

func TestBaseTxSyntacticVerifyInputOverflow(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{
			{
				UTXOID: avax.UTXOID{
					TxID: ids.ID{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					},
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: math.MaxUint64,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			},
			{
				UTXOID: avax.UTXOID{
					TxID: ids.ID{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					},
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			},
		},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Input overflow should have erred")
	}
}

func TestBaseTxSyntacticVerifyOutputOverflow(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
			{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: math.MaxUint64,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 1,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Output overflow should have erred")
	}
}

func TestBaseTxSyntacticVerifyInsufficientFunds(t *testing.T) {
	ctx := NewContext(t)
	c := setupCodec()

	tx := &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: math.MaxUint64,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 1,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{2},
				},
			},
		}},
	}}

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0, 0); err == nil {
		t.Fatalf("Insufficient funds should have erred")
	}
}

func TestBaseTxNotState(t *testing.T) {
	intf := interface{}(&BaseTx{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
