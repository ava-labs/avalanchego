// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

var (
	nameTooLong          = "LLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLL"
	symbolTooLong        = "LLLLL"
	illegalNameCharacter = "h8*32"
	invalidASCIIStr      = "ÉÎ"
	invalidWhitespaceStr = " HAT"
	denominationTooLarge = byte(maxDenomination + 1)
)

func validCreateAssetTx(t *testing.T) (*CreateAssetTx, codec.Manager, *snow.Context) {
	_, c := setupCodec()
	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
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
		}},
		Name:         "NormalName",
		Symbol:       "TICK",
		Denomination: byte(2),
		States: []*InitialState{
			{
				FxID: 0,
				Outs: []verify.State{
					&secp256k1fx.TransferOutput{
						Amt: 12345,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
			},
		},
	}

	unsignedBytes, err := c.Marshal(currentCodecVersion, tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(unsignedBytes, unsignedBytes)

	ctx := NewContext(t)
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err != nil {
		t.Fatalf("Valid CreateAssetTx failed syntactic verification due to: %s", err)
	}
	return tx, c, ctx
}

func TestCreateAssetTxSerialization(t *testing.T) {
	currentCodecExpected := []byte{
		// Codec version:
		0x00, 0x01,
		// txID:
		0x00, 0x00, 0x00, 0x01,
		// networkID:
		0x00, 0x00, 0x00, 0x02,
		// blockchainID:
		0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
		0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
		0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
		0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
		// number of outs:
		0x00, 0x00, 0x00, 0x01,
		// output[0]:
		// assetID:
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		// output:
		0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x30, 0x39, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xd4, 0x31, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x02, 0x51, 0x02, 0x5c, 0x61,
		0xfb, 0xcf, 0xc0, 0x78, 0xf6, 0x93, 0x34, 0xf8,
		0x34, 0xbe, 0x6d, 0xd2, 0x6d, 0x55, 0xa9, 0x55,
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
		// number of inputs:
		0x00, 0x00, 0x00, 0x01,
		// txID:
		0xf1, 0xe1, 0xd1, 0xc1, 0xb1, 0xa1, 0x91, 0x81,
		0x71, 0x61, 0x51, 0x41, 0x31, 0x21, 0x11, 0x01,
		0xf0, 0xe0, 0xd0, 0xc0, 0xb0, 0xa0, 0x90, 0x80,
		0x70, 0x60, 0x50, 0x40, 0x30, 0x20, 0x10, 0x00,
		// utxoIndex:
		0x00, 0x00, 0x00, 0x05,
		// assetID:
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		// input:
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x07, 0x5b, 0xcd, 0x15, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x07,
		// Memo length:
		0x00, 0x00, 0x00, 0x04,
		// Memo:
		0x00, 0x01, 0x02, 0x03,
		// name:
		0x00, 0x10, 0x56, 0x6f, 0x6c, 0x61, 0x74, 0x69,
		0x6c, 0x69, 0x74, 0x79, 0x20, 0x49, 0x6e, 0x64,
		0x65, 0x78,
		// symbol:
		0x00, 0x03, 0x56, 0x49, 0x58,
		// denomination:
		0x02,
		// number of InitialStates:
		0x00, 0x00, 0x00, 0x01,
		// InitialStates[0]:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x30, 0x39, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xd4, 0x31, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x02, 0x51, 0x02, 0x5c, 0x61,
		0xfb, 0xcf, 0xc0, 0x78, 0xf6, 0x93, 0x34, 0xf8,
		0x34, 0xbe, 0x6d, 0xd2, 0x6d, 0x55, 0xa9, 0x55,
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
		// number of credentials:
		0x00, 0x00, 0x00, 0x00,
	}

	oldCodecExpected := []byte{
		// Codec version:
		0x00, 0x00,
		// txID:
		0x00, 0x00, 0x00, 0x01,
		// networkID:
		0x00, 0x00, 0x00, 0x02,
		// blockchainID:
		0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
		0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
		0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
		0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
		// number of outs:
		0x00, 0x00, 0x00, 0x01,
		// output[0]:
		// assetID:
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		// output:
		0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x30, 0x39, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xd4, 0x31, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x02, 0x51, 0x02, 0x5c, 0x61,
		0xfb, 0xcf, 0xc0, 0x78, 0xf6, 0x93, 0x34, 0xf8,
		0x34, 0xbe, 0x6d, 0xd2, 0x6d, 0x55, 0xa9, 0x55,
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
		// number of inputs:
		0x00, 0x00, 0x00, 0x01,
		// txID:
		0xf1, 0xe1, 0xd1, 0xc1, 0xb1, 0xa1, 0x91, 0x81,
		0x71, 0x61, 0x51, 0x41, 0x31, 0x21, 0x11, 0x01,
		0xf0, 0xe0, 0xd0, 0xc0, 0xb0, 0xa0, 0x90, 0x80,
		0x70, 0x60, 0x50, 0x40, 0x30, 0x20, 0x10, 0x00,
		// utxoIndex:
		0x00, 0x00, 0x00, 0x05,
		// assetID:
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		// input:
		0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x00,
		0x07, 0x5b, 0xcd, 0x15, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x07,
		// Memo length:
		0x00, 0x00, 0x00, 0x04,
		// Memo:
		0x00, 0x01, 0x02, 0x03,
		// name:
		0x00, 0x10, 0x56, 0x6f, 0x6c, 0x61, 0x74, 0x69,
		0x6c, 0x69, 0x74, 0x79, 0x20, 0x49, 0x6e, 0x64,
		0x65, 0x78,
		// symbol:
		0x00, 0x03, 0x56, 0x49, 0x58,
		// denomination:
		0x02,
		// number of InitialStates:
		0x00, 0x00, 0x00, 0x01,
		// InitialStates[0]:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x30, 0x39, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xd4, 0x31, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x02, 0x51, 0x02, 0x5c, 0x61,
		0xfb, 0xcf, 0xc0, 0x78, 0xf6, 0x93, 0x34, 0xf8,
		0x34, 0xbe, 0x6d, 0xd2, 0x6d, 0x55, 0xa9, 0x55,
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
		// number of credentials:
		0x00, 0x00, 0x00, 0x00,
	}

	tx := &Tx{UnsignedTx: &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID: 2,
			BlockchainID: ids.ID{
				0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
				0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
				0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
				0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
			},
			Memo: []byte{0x00, 0x01, 0x02, 0x03},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{
					ID: ids.ID{
						0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
						0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
						0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
						0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					},
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 12345,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  54321,
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.NewShortID([20]byte{
								0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
								0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
								0x6d, 0x55, 0xa9, 0x55,
							}),
							ids.NewShortID([20]byte{
								0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
								0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
								0x43, 0xab, 0x08, 0x59,
							}),
						},
					},
				},
			}},
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.ID{
						0xf1, 0xe1, 0xd1, 0xc1, 0xb1, 0xa1, 0x91, 0x81,
						0x71, 0x61, 0x51, 0x41, 0x31, 0x21, 0x11, 0x01,
						0xf0, 0xe0, 0xd0, 0xc0, 0xb0, 0xa0, 0x90, 0x80,
						0x70, 0x60, 0x50, 0x40, 0x30, 0x20, 0x10, 0x00,
					},
					OutputIndex: 5,
				},
				Asset: avax.Asset{
					ID: ids.ID{
						0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
						0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
						0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
						0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					},
				},
				In: &secp256k1fx.TransferInput{
					Amt: 123456789,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{3, 7},
					},
				},
			}},
		}},
		Name:         "Volatility Index",
		Symbol:       "VIX",
		Denomination: 2,
		States: []*InitialState{
			{
				FxID: 0,
				Outs: []verify.State{
					&secp256k1fx.TransferOutput{
						Amt: 12345,
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  54321,
							Threshold: 1,
							Addrs: []ids.ShortID{
								ids.NewShortID([20]byte{
									0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
									0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
									0x6d, 0x55, 0xa9, 0x55,
								}),
								ids.NewShortID([20]byte{
									0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
									0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
									0x43, 0xab, 0x08, 0x59,
								}),
							},
						},
					},
				},
			},
		},
	}}

	_, c := setupCodec()

	if err := tx.SignSECP256K1Fx(c, currentCodecVersion, nil); err != nil {
		t.Fatal(err)
	}
	result := tx.Bytes()
	if !bytes.Equal(currentCodecExpected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", currentCodecExpected, result)
	}

	if err := tx.SignSECP256K1Fx(c, pre110CodecVersion, nil); err != nil {
		t.Fatal(err)
	}
	result = tx.Bytes()
	if !bytes.Equal(oldCodecExpected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", oldCodecExpected, result)
	}
}

func TestCreateAssetTxGetters(t *testing.T) {
	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if initialStates := tx.InitialStates(); len(initialStates) != 1 {
		t.Fatalf("Wrong number of assets returned")
	} else if initialState := initialStates[0]; initialState.FxID != 0 {
		t.Fatalf("Wrong fxID returned")
	} else if len(initialState.Outs) != 0 {
		t.Fatalf("Wrong number of outs returned")
	} else if utxos := tx.UTXOs(); len(utxos) != 0 {
		t.Fatalf("Wrong number of utxos returned")
	}
}

func TestCreateAssetTxSyntacticVerify(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAssetTxSyntacticVerifyNil(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := (*CreateAssetTx)(nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Nil CreateAssetTx should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyNameTooShort(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Too short name should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyNameTooLong(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name: "BRADY WINSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS" +
			"SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS" +
			"SSS",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Too long name should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifySymbolTooShort(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Too short symbol should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifySymbolTooLong(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "TOM",
		Symbol:       "BRADY",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Too long symbol should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyNoFxs(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: 0,
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("No Fxs should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyDenominationTooLong(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: denominationTooLarge,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Too large denomination should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyNameWithWhitespace(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY ",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Whitespace at the end of the name should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyNameWithInvalidCharacter(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY!",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Name with an invalid character should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyNameWithUnicodeCharacter(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         illegalNameCharacter,
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Name with an invalid character should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifySymbolWithInvalidCharacter(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM!",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Symbol with an invalid character should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyInvalidBaseTx(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID + 1,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 0,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Invalid BaseTx should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyInvalidInitialState(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 1,
		}},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 1); err == nil {
		t.Fatalf("Invalid InitialState should have errored")
	}
}

func TestCreateAssetTxSyntacticVerifyUnsortedInitialStates(t *testing.T) {
	ctx := NewContext(t)
	_, c := setupCodec()

	tx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "BRADY",
		Symbol:       "TOM",
		Denomination: 0,
		States: []*InitialState{
			{
				FxID: 1,
			},
			{
				FxID: 0,
			},
		},
	}
	tx.Initialize(nil, nil)

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, ids.Empty, 0, 0, 2); err == nil {
		t.Fatalf("Unsorted InitialStates should have errored")
	}
}

func TestCreateAssetTxNotState(t *testing.T) {
	intf := interface{}(&CreateAssetTx{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}

func TestCreateAssetTxSyntacticVerifyName(t *testing.T) {
	tx, c, ctx := validCreateAssetTx(t)

	// String of Length 129 should fail SyntacticVerify
	tx.Name = nameTooLong

	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to name too long")
	}

	tx.Name = invalidWhitespaceStr
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to invalid whitespace in name")
	}

	tx.Name = invalidASCIIStr
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to invalid ASCII character in name")
	}
}

func TestCreateAssetTxSyntacticVerifySymbol(t *testing.T) {
	tx, c, ctx := validCreateAssetTx(t)

	tx.Symbol = symbolTooLong
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to symbol too long")
	}

	tx.Symbol = " F"
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to invalid whitespace in symbol")
	}

	tx.Symbol = "É"
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to invalid ASCII character in symbol")
	}
}

func TestCreateAssetTxSyntacticVerifyInvalidDenomination(t *testing.T) {
	tx, c, ctx := validCreateAssetTx(t)

	tx.Denomination = byte(33)
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to denomination too large")
	}
}

func TestCreateAssetTxSyntacticVerifyInitialStates(t *testing.T) {
	tx, c, ctx := validCreateAssetTx(t)

	tx.States = []*InitialState{}
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to no Initial States")
	}

	tx.States = []*InitialState{
		{
			FxID: 5, // Invalid FxID
			Outs: []verify.State{
				&secp256k1fx.TransferOutput{
					Amt: 12345,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
	}

	// NumFxs is 1, so FxID 5 should cause an error
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 1); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to invalid Fx")
	}

	uniqueStates := []*InitialState{
		{
			FxID: 0,
			Outs: []verify.State{
				&secp256k1fx.TransferOutput{
					Amt: 12345,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
		{
			FxID: 1,
			Outs: []verify.State{
				&secp256k1fx.TransferOutput{
					Amt: 12345,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
		{
			FxID: 2,
			Outs: []verify.State{
				&secp256k1fx.TransferOutput{
					Amt: 12345,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
	}

	sortInitialStates(uniqueStates)

	// Put states in unsorted order
	tx.States = []*InitialState{
		uniqueStates[2],
		uniqueStates[0],
	}
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 3); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to non-sorted initial states")
	}

	tx.States = []*InitialState{
		uniqueStates[0],
		uniqueStates[0],
	}
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 3); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to non-unique initial states")
	}

}

func TestCreateAssetTxSyntacticVerifyBaseTx(t *testing.T) {
	tx, c, ctx := validCreateAssetTx(t)
	var baseTx BaseTx
	tx.BaseTx = baseTx
	if err := tx.SyntacticVerify(ctx, c, currentCodecVersion, assetID, 0, 0, 2); err == nil {
		t.Fatal("CreateAssetTx should have failed syntactic verification due to invalid BaseTx (nil)")
	}
}

// Test managed asset functionality
func TestManagedAsset(t *testing.T) {
	type create struct {
		originalFrozen  bool
		originalManager secp256k1fx.OutputOwners
	}

	type transfer struct {
		amt  uint64
		from ids.ShortID
		to   ids.ShortID
		keys []*crypto.PrivateKeySECP256K1R
	}

	type updateStatus struct {
		manager      secp256k1fx.OutputOwners
		frozen, mint bool
		mintAmt      uint64
		mintTo       ids.ShortID
		keys         []*crypto.PrivateKeySECP256K1R
	}

	type step struct {
		op               interface{} // create, transfer, mint or updateStatus
		shouldFailVerify bool
	}

	type test struct {
		description string
		create      create
		steps       []step
	}

	tests := []test{
		{
			"create, wrong key mint",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[1]}, // not manager key
					},
					true,
				},
			},
		},
		{
			"create, mint, transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false,
				},
			},
		},
		{
			"create, mint, wrong transfer key",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // use wrong key to try to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					true,
				},
			},
		},
		{
			"create, mint, asset manager transfers",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // Note that the asset manager, not keys[1], is spending
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
			},
		},
		{
			"create, change manager, old manager mint fails, mint, transfer, old manager transfer fails, transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // Change owner to keys[1]
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
						frozen: false,
						mint:   false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // old manager tries to mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[1]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]}, // old key
					},
					true,
				},
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[1]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[2].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false,
				},
				{ // old manager tries to transfer; fails
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
			},
		},
		{
			"create, mint, freeze, transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // freeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					true, // Should error because asset is frozen
				},
			},
		},
		{
			"create, mint, freeze and change manager, manager transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // freeze and change owner to keys[2]
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[2].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // old manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true, // asset is frozen
				},
				{ // manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					true, // asset is frozen
				},
				{ // unfreeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[2].PublicKey().Address()},
						},
						frozen: false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false,
				},
				{ // old manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true, // keys[0] is not the owner or the manager
				},
				{ // manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false, // keys[2] is the owner
				},
			},
		},
		{
			"create, mint, multiple freeze/unfreeze",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // freeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					true, // Should error because asset is frozen
				},
				{ // transfer should fail even if it's manager
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true, // Should error because asset is frozen
				},
				{ // unfreeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
						frozen: false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false, // Should not error because asset is unfrozen
				},
				{ // freeze and change manager
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					true, // Should error because asset is frozen
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					true, // Should error because asset is frozen
				},
				{ // unfreeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
						frozen: false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false, // Should not error because asset is unfrozen
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			// Initialize the VM
			genesisBytes, _, vm, _ := GenesisVM(t)
			ctx := vm.ctx
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				ctx.Lock.Unlock()
			}()

			genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)
			avaxID := genesisTx.ID()

			// Create a create a managed asset
			assetStatusOutput := &secp256k1fx.ManagedAssetStatusOutput{
				Frozen:  test.create.originalFrozen,
				Manager: test.create.originalManager,
			}
			unsignedCreateManagedAssetTx := &CreateAssetTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    networkID,
					BlockchainID: chainID,
					Outs: []*avax.TransferableOutput{{
						Asset: avax.Asset{ID: avaxID},
						Out: &secp256k1fx.TransferOutput{
							Amt: startBalance - testTxFee,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
							},
						},
					}},
					Ins: []*avax.TransferableInput{{
						// Creation tx paid for by keys[0] genesis balance
						UTXOID: avax.UTXOID{
							TxID:        avaxID,
							OutputIndex: 2,
						},
						Asset: avax.Asset{ID: avaxID},
						In: &secp256k1fx.TransferInput{
							Amt: startBalance,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					}},
				}},
				Name:         "NormalName",
				Symbol:       "TICK",
				Denomination: byte(2),
				States: []*InitialState{
					{
						FxID: 0,
						Outs: []verify.State{
							assetStatusOutput,
						},
					},
				},
			}
			createManagedAssetTx := Tx{
				UnsignedTx: unsignedCreateManagedAssetTx,
			}

			// Sign/initialize the transaction
			feeSigner := []*crypto.PrivateKeySECP256K1R{keys[0]}
			err := createManagedAssetTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner})
			require.NoError(t, err)

			// Verify and accept the transaction
			uniqueCreateManagedAssetTx, err := vm.parseTx(createManagedAssetTx.Bytes())
			require.NoError(t, err)
			err = uniqueCreateManagedAssetTx.Verify()
			require.NoError(t, err)
			err = uniqueCreateManagedAssetTx.Accept()
			require.NoError(t, err)
			// The asset has been created
			managedAssetID := uniqueCreateManagedAssetTx.ID()

			//updateStatusOutput := assetStatusOutput
			updateStatusUtxoID := avax.UTXOID{TxID: managedAssetID, OutputIndex: 1}

			avaxFundedUtxoID := &avax.UTXOID{TxID: managedAssetID, OutputIndex: 0}
			avaxFundedAmt := startBalance - testTxFee

			// Address --> UTXO containing the managed asset owned by that address
			managedAssetFundedUtxoIDs := map[[20]byte]avax.UTXOID{}

			// Address --> Balance of managed asset owned by that address
			managedAssetFundedAmt := map[[20]byte]uint64{}

			for _, step := range test.steps {
				switch op := step.op.(type) {
				case transfer:
					// Transfer some units of the managed asset
					transferTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    networkID,
						BlockchainID: chainID,
						Ins: []*avax.TransferableInput{
							{ // This input is for the tx fee
								UTXOID: *avaxFundedUtxoID,
								Asset:  avax.Asset{ID: avaxID},
								In: &secp256k1fx.TransferInput{
									Amt: avaxFundedAmt,
									Input: secp256k1fx.Input{
										SigIndices: []uint32{0},
									},
								},
							},
							{ // This input is to transfer the asset
								UTXOID: managedAssetFundedUtxoIDs[op.from.Key()],
								Asset:  avax.Asset{ID: managedAssetID},
								In: &secp256k1fx.TransferInput{
									Amt: managedAssetFundedAmt[op.from.Key()],
									Input: secp256k1fx.Input{
										SigIndices: []uint32{0},
									},
								},
							},
						},
						Outs: []*avax.TransferableOutput{
							{ // Send AVAX change back to keys[0]
								Asset: avax.Asset{ID: genesisTx.ID()},
								Out: &secp256k1fx.TransferOutput{
									Amt: avaxFundedAmt - testTxFee,
									OutputOwners: secp256k1fx.OutputOwners{
										Threshold: 1,
										Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
									},
								},
							},
							{ // Transfer the managed asset
								Asset: avax.Asset{ID: managedAssetID},
								Out: &secp256k1fx.TransferOutput{
									Amt: op.amt,
									OutputOwners: secp256k1fx.OutputOwners{
										Locktime:  0,
										Threshold: 1,
										Addrs:     []ids.ShortID{op.to},
									},
								},
							},
						},
					}}}

					avax.SortTransferableInputs(transferTx.UnsignedTx.(*BaseTx).Ins)
					avax.SortTransferableOutputs(transferTx.UnsignedTx.(*BaseTx).Outs, vm.codec, currentCodecVersion)

					// One signature to spend the tx fee, one signature to transfer the managed asset
					if transferTx.UnsignedTx.(*BaseTx).Ins[0].AssetID() == avaxID {
						err = transferTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner, op.keys})
					} else {
						err = transferTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{op.keys, feeSigner})
					}
					require.NoError(t, err)

					// Verify and accept the transaction
					uniqueTransferTx, err := vm.parseTx(transferTx.Bytes())
					require.NoError(t, err)
					err = uniqueTransferTx.Verify()
					if !step.shouldFailVerify {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						continue
					}
					err = uniqueTransferTx.Accept()
					require.NoError(t, err)

					avaxOutputIndex := uint32(0)
					if transferTx.UnsignedTx.(*BaseTx).Outs[0].AssetID() == managedAssetID {
						avaxOutputIndex = uint32(1)
					}

					// Update test data
					avaxFundedAmt -= testTxFee
					avaxFundedUtxoID = &avax.UTXOID{TxID: uniqueTransferTx.txID, OutputIndex: avaxOutputIndex}

					managedAssetFundedAmt[op.to.Key()] = op.amt
					managedAssetFundedAmt[op.from.Key()] -= op.amt
					// TODO update from
					managedAssetFundedUtxoIDs[op.to.Key()] = avax.UTXOID{TxID: uniqueTransferTx.txID, OutputIndex: 1 - avaxOutputIndex}
				case updateStatus:
					unsignedTx := &OperationTx{
						BaseTx: BaseTx{
							avax.BaseTx{
								NetworkID:    networkID,
								BlockchainID: chainID,
								Outs: []*avax.TransferableOutput{{
									Asset: avax.Asset{ID: avaxID},
									Out: &secp256k1fx.TransferOutput{
										Amt: avaxFundedAmt - testTxFee,
										OutputOwners: secp256k1fx.OutputOwners{
											Threshold: 1,
											Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
										},
									},
								}},
								Ins: []*avax.TransferableInput{
									{ // This input is for the transaction fee
										UTXOID: *avaxFundedUtxoID,
										Asset:  avax.Asset{ID: avaxID},
										In: &secp256k1fx.TransferInput{
											Amt: avaxFundedAmt,
											Input: secp256k1fx.Input{
												SigIndices: []uint32{0},
											},
										},
									},
								},
							},
						},
						Ops: []*Operation{
							{
								Asset:   avax.Asset{ID: managedAssetID},
								UTXOIDs: []*avax.UTXOID{&updateStatusUtxoID},
								Op: &secp256k1fx.UpdateManagedAssetOperation{
									Input: secp256k1fx.Input{
										SigIndices: []uint32{0},
									},
									ManagedAssetStatusOutput: secp256k1fx.ManagedAssetStatusOutput{
										Frozen:  op.frozen,
										Manager: op.manager,
									},
								},
							},
						},
					}

					if op.mint {
						unsignedTx.Ops[0].Op.(*secp256k1fx.UpdateManagedAssetOperation).Mint = true
						transferOut := secp256k1fx.TransferOutput{
							Amt: op.mintAmt,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{op.mintTo},
							},
						}
						unsignedTx.Ops[0].Op.(*secp256k1fx.UpdateManagedAssetOperation).TransferOutput = transferOut
					}

					updateStatusTx := &Tx{
						UnsignedTx: unsignedTx,
					}

					// One signature to spend the tx fee, one signature to transfer the managed asset
					err = updateStatusTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner, op.keys})
					require.NoError(t, err)

					// Verify and accept the transaction
					uniqueUpdateStatusTx, err := vm.parseTx(updateStatusTx.Bytes())
					require.NoError(t, err)
					err = uniqueUpdateStatusTx.Verify()
					if !step.shouldFailVerify {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						continue
					}
					err = uniqueUpdateStatusTx.Accept()
					require.NoError(t, err)

					avaxFundedAmt -= testTxFee
					avaxFundedUtxoID = &avax.UTXOID{TxID: uniqueUpdateStatusTx.ID(), OutputIndex: 0}

					updateStatusUtxoID = avax.UTXOID{TxID: uniqueUpdateStatusTx.ID(), OutputIndex: 1}
					if op.mint {
						managedAssetFundedAmt[op.mintTo.Key()] += op.mintAmt
						managedAssetFundedUtxoIDs[op.mintTo.Key()] = avax.UTXOID{TxID: updateStatusTx.ID(), OutputIndex: 2}
					}
				}
			}
		})
	}
}

// Ensure that if an asset has a manager, it has no additional minters, and that
// an asset has at most one manager
func TestManagedAssetInitialState(t *testing.T) {
	// Setup; Initialize the VM
	genesisBytes, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()

	baseTx := CreateAssetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: avaxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - testTxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			}},
			Ins: []*avax.TransferableInput{{
				// Creation tx paid for by keys[0] genesis balance
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt: startBalance,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			}},
		}},
		Name:         "NormalName",
		Symbol:       "TICK",
		Denomination: byte(2),
		States: []*InitialState{
			{
				FxID: 0,
				Outs: []verify.State{},
			},
		},
	}

	type test struct {
		description string
		states      []*InitialState
		shouldErr   bool
	}

	assetStatusOutput := &secp256k1fx.ManagedAssetStatusOutput{
		Frozen: false,
		Manager: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
	}
	mintOutput := &secp256k1fx.MintOutput{
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
	}

	// Give tx various values for States and make sure only valid ones pass verification
	tests := []test{
		{
			"nil states",
			nil,
			true,
		},
		{
			"empty states",
			[]*InitialState{},
			true,
		},
		{
			"two managers",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput, assetStatusOutput},
				},
			},
			true,
		},
		{
			"two managers spread over initial states",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput},
				},
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput},
				},
			},
			true,
		},
		{
			"mint output and manager output",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{mintOutput, assetStatusOutput},
				},
			},
			true,
		},
		{
			"mint output and manager output 2",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput, mintOutput},
				},
			},
			true,
		},
		{
			"mint output and manager output 3",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput},
				},
				{
					FxID: 0,
					Outs: []verify.State{mintOutput},
				},
			},
			true,
		},
		{
			"mint output and manager output 4",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{mintOutput},
				},
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput},
				},
			},
			true,
		},
		{
			"valid",
			[]*InitialState{
				{
					FxID: 0,
					Outs: []verify.State{assetStatusOutput},
				},
			},
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			unsignedTx := baseTx            // Copy
			unsignedTx.States = test.states // Set states

			// Sign/initialize the transaction
			tx := Tx{
				UnsignedTx: &unsignedTx,
			}
			feeSigner := []*crypto.PrivateKeySECP256K1R{keys[0]}
			err := tx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner})
			require.NoError(t, err)

			// Verify the transaction
			err = tx.SyntacticVerify(
				vm.ctx,
				vm.codec,
				currentCodecVersion,
				vm.ctx.AVAXAssetID,
				vm.txFee,
				vm.creationTxFee,
				1,
			)
			if test.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
