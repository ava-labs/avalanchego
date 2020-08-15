// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"math"
	"testing"

	"github.com/ava-labs/gecko/api/keystore"
	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestExportTxSyntacticVerify(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestExportTxSyntacticVerifyNil(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := (*ExportTx)(nil)

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to a nil ExportTx")
	}
}

func TestExportTxSyntacticVerifyWrongNetworkID(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID + 1,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to a wrong network ID")
	}
}

func TestExportTxSyntacticVerifyWrongBlockchainID(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID: ids.NewID([32]byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			}),
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to wrong blockchain ID")
	}
}

func TestExportTxSyntacticVerifyInvalidMemo(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
			Memo: make([]byte, maxMemoSize+1),
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to memo field being too long")
	}
}

func TestExportTxSyntacticVerifyInvalidBaseOutput(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 10000,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 2345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to an invalid base output")
	}
}

func TestExportTxSyntacticVerifyUnsortedBaseOutputs(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
			Outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: asset},
					Out: &secp256k1fx.TransferOutput{
						Amt: 10000,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
				{
					Asset: avax.Asset{ID: asset},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1111,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
			},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1234,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to unsorted base outputs")
	}
}

func TestExportTxSyntacticVerifyInvalidOutput(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to invalid output")
	}
}

func TestExportTxSyntacticVerifyUnsortedOutputs(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 10000,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
			{
				Asset: avax.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2345,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to unsorted outputs")
	}
}

func TestExportTxSyntacticVerifyInvalidInput(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{
				{
					UTXOID: avax.UTXOID{
						TxID: ids.NewID([32]byte{
							0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
							0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
							0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
							0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
						}),
						OutputIndex: 0,
					},
					Asset: avax.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 54321,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{2},
						},
					},
				},
				{
					UTXOID: avax.UTXOID{
						TxID: ids.NewID([32]byte{
							0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
							0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
							0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
							0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
						}),
						OutputIndex: 1,
					},
					Asset: avax.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 0,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{2},
						},
					},
				},
			},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to invalid input")
	}
}

func TestExportTxSyntacticVerifyUnsortedInputs(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{
				{
					UTXOID: avax.UTXOID{
						TxID: ids.NewID([32]byte{
							0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
							0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
							0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
							0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
						}),
						OutputIndex: 1,
					},
					Asset: avax.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{2},
						},
					},
				},
				{
					UTXOID: avax.UTXOID{
						TxID: ids.NewID([32]byte{
							0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
							0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
							0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
							0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
						}),
						OutputIndex: 0,
					},
					Asset: avax.Asset{ID: asset},
					In: &secp256k1fx.TransferInput{
						Amt: 54321,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{2},
						},
					},
				},
			},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to unsorted inputs")
	}
}

func TestExportTxSyntacticVerifyInvalidFlowCheck(t *testing.T) {
	ctx := NewContext()
	c := setupCodec()

	tx := &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID: ids.NewID([32]byte{
						0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
						0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
						0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
						0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
					}),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: asset},
				In: &secp256k1fx.TransferInput{
					Amt: 54321,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 123450,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}
	tx.Initialize([]byte{})

	if err := tx.SyntacticVerify(ctx, c, ids.Empty, 0, 0); err == nil {
		t.Fatalf("should have errored due to an invalid flow check")
	}
}

func TestExportTxSerialization(t *testing.T) {
	expected := []byte{
		// Codec version:
		0x00, 0x00,
		// txID:
		0x00, 0x00, 0x00, 0x04,
		// networkID:
		0x00, 0x00, 0x00, 0x02,
		// blockchainID:
		0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
		0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
		0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
		0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
		// number of outs:
		0x00, 0x00, 0x00, 0x00,
		// number of inputs:
		0x00, 0x00, 0x00, 0x01,
		// utxoID:
		0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
		0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
		0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
		0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		// output index
		0x00, 0x00, 0x00, 0x00,
		// assetID:
		0x1f, 0x3f, 0x5f, 0x7f, 0x9e, 0xbe, 0xde, 0xfe,
		0x1d, 0x3d, 0x5d, 0x7d, 0x9c, 0xbc, 0xdc, 0xfc,
		0x1b, 0x3b, 0x5b, 0x7b, 0x9a, 0xba, 0xda, 0xfa,
		0x19, 0x39, 0x59, 0x79, 0x98, 0xb8, 0xd8, 0xf8,
		// input:
		// input ID:
		0x00, 0x00, 0x00, 0x05,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0xe8,
		// num sig indices:
		0x00, 0x00, 0x00, 0x01,
		// sig index[0]:
		0x00, 0x00, 0x00, 0x00,
		// Memo length:
		0x00, 0x00, 0x00, 0x04,
		// Memo:
		0x00, 0x01, 0x02, 0x03,
		// Destination Chain ID:
		0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e,
		0x2d, 0x7d, 0xad, 0xfd, 0x2c, 0x7c, 0xac, 0xfc,
		0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a, 0xba, 0xea,
		0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8,
		// number of exported outs:
		0x00, 0x00, 0x00, 0x00,
	}

	tx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: 2,
			BCID: ids.NewID([32]byte{
				0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
				0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
				0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
				0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
			}),
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{TxID: ids.NewID([32]byte{
					0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
					0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
					0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
					0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
				})},
				Asset: avax.Asset{ID: ids.NewID([32]byte{
					0x1f, 0x3f, 0x5f, 0x7f, 0x9e, 0xbe, 0xde, 0xfe,
					0x1d, 0x3d, 0x5d, 0x7d, 0x9c, 0xbc, 0xdc, 0xfc,
					0x1b, 0x3b, 0x5b, 0x7b, 0x9a, 0xba, 0xda, 0xfa,
					0x19, 0x39, 0x59, 0x79, 0x98, 0xb8, 0xd8, 0xf8,
				})},
				In: &secp256k1fx.TransferInput{
					Amt:   1000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
			Memo: []byte{0x00, 0x01, 0x02, 0x03},
		},
		DestinationChain: ids.NewID([32]byte{
			0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e,
			0x2d, 0x7d, 0xad, 0xfd, 0x2c, 0x7c, 0xac, 0xfc,
			0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a, 0xba, 0xea,
			0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8,
		}),
	}}

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

func TestExportTxSemanticVerify(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	rawTx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	if err := rawTx.sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.ParseTx(rawTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	utx, ok := tx.(*UniqueTx)
	if !ok {
		t.Fatalf("wrong tx type")
	}

	if err := rawTx.UnsignedTx.SemanticVerify(vm, utx, rawTx.Creds); err != nil {
		t.Fatal(err)
	}
}

func TestExportTxSemanticVerifyUnknownCredFx(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	rawTx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	if err := rawTx.sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.ParseTx(rawTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	utx, ok := tx.(*UniqueTx)
	if !ok {
		t.Fatalf("wrong tx type")
	}

	if err := rawTx.UnsignedTx.SemanticVerify(vm, utx, []verify.Verifiable{nil}); err == nil {
		t.Fatalf("should have errored due to an unknown credential fx")
	}
}

func TestExportTxSemanticVerifyMissingUTXO(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	rawTx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1000,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	if err := rawTx.sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.ParseTx(rawTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	utx, ok := tx.(*UniqueTx)
	if !ok {
		t.Fatalf("wrong tx type")
	}

	if err := rawTx.UnsignedTx.SemanticVerify(vm, utx, rawTx.Creds); err == nil {
		t.Fatalf("should have errored due to an unknown utxo")
	}
}

func TestExportTxSemanticVerifyInvalidAssetID(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	rawTx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: ids.Empty},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: ids.Empty},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	if err := rawTx.sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.ParseTx(rawTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	utx, ok := tx.(*UniqueTx)
	if !ok {
		t.Fatalf("wrong tx type")
	}

	if err := rawTx.UnsignedTx.SemanticVerify(vm, utx, rawTx.Creds); err == nil {
		t.Fatalf("should have errored due to an invalid asset ID")
	}
}

func TestExportTxSemanticVerifyInvalidFx(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)
	ctx := NewContext()

	baseDB := memdb.New()

	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = sm.NewBlockchainSharedMemory(ctx.ChainID)

	ctx.Lock.Lock()

	userKeystore := keystore.CreateTestKeystore()
	if err := userKeystore.AddUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()

	issuer := make(chan common.Message, 1)
	vm := &VM{
		avax: avaxID,
	}
	err := vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty,
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(0),
				Fx: &FxTest{
					InitializeF: func(vmIntf interface{}) error {
						vm := vmIntf.(*VM)
						c := vm.Codec()
						c.RegisterType(&avax.TestVerifiable{})
						return nil
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	rawTx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	if err := rawTx.sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.ParseTx(rawTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	utx, ok := tx.(*UniqueTx)
	if !ok {
		t.Fatalf("wrong tx type")
	}

	if err := rawTx.UnsignedTx.SemanticVerify(vm, utx, []verify.Verifiable{&avax.TestVerifiable{}}); err == nil {
		t.Fatalf("should have errored due to using an invalid fxID")
	}
}

func TestExportTxSemanticVerifyInvalidTransfer(t *testing.T) {
	genesisBytes, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	rawTx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}}

	if err := rawTx.sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[1]}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.ParseTx(rawTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	utx, ok := tx.(*UniqueTx)
	if !ok {
		t.Fatalf("wrong tx type")
	}

	if err := rawTx.UnsignedTx.SemanticVerify(vm, utx, rawTx.Creds); err == nil {
		t.Fatalf("should have errored due to an invalid credential")
	}
}

// Test issuing an import transaction.
func TestIssueExportTx(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDB := memdb.New()

	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))

	ctx := NewContext()
	ctx.SharedMemory = sm.NewBlockchainSharedMemory(chainID)

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()

	ctx.Lock.Lock()
	vm := &VM{
		avax: avaxID,
	}
	if err := vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
	); err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	key := keys[0]

	tx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

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

	if _, err := vm.IssueTx(tx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()

	msg := <-issuer
	if msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	txs := vm.PendingTxs()
	if len(txs) != 1 {
		t.Fatalf("Should have returned %d tx(s)", 1)
	}

	parsedTx := txs[0]
	if err := parsedTx.Verify(); err != nil {
		t.Fatal(err)
	} else if err := parsedTx.Accept(); err != nil {
		t.Fatal(err)
	}

	smDB := vm.ctx.SharedMemory.GetDatabase(platformChainID)
	defer vm.ctx.SharedMemory.ReleaseDatabase(platformChainID)

	// check from the peer chain side
	state := avax.NewPrefixedState(smDB, vm.codec, platformChainID, vm.ctx.ChainID)

	utxo := avax.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 0,
	}
	utxoID := utxo.InputID()
	if _, err := state.UTXO(utxoID); err != nil {
		t.Fatal(err)
	}

	utxoIDs, err := state.Funds(key.PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	if len(utxoIDs) != 1 {
		t.Fatalf("wrong number of utxoIDs %d", len(utxoIDs))
	}
}

// Test force accepting an import transaction.
func TestClearForceAcceptedExportTx(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDB := memdb.New()

	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))

	ctx := NewContext()
	ctx.SharedMemory = sm.NewBlockchainSharedMemory(chainID)

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()
	platformID := ids.Empty.Prefix(0)

	ctx.Lock.Lock()
	vm := &VM{
		avax: avaxID,
	}
	err := vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
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

	key := keys[0]

	tx := &Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   50000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		},
		DestinationChain: platformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

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

	if _, err := vm.IssueTx(tx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()

	msg := <-issuer
	if msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	txs := vm.PendingTxs()
	if len(txs) != 1 {
		t.Fatalf("Should have returned %d tx(s)", 1)
	}

	parsedTx := txs[0]
	if err := parsedTx.Verify(); err != nil {
		t.Fatal(err)
	}

	smDB := vm.ctx.SharedMemory.GetDatabase(platformID)

	state := avax.NewPrefixedState(smDB, vm.codec, vm.ctx.ChainID, platformChainID)

	utxo := avax.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 0,
	}
	utxoID := utxo.InputID()
	if err := state.SpendUTXO(utxoID); err != nil {
		t.Fatal(err)
	}

	vm.ctx.SharedMemory.ReleaseDatabase(platformID)

	parsedTx.Accept()

	smDB = vm.ctx.SharedMemory.GetDatabase(platformID)
	defer vm.ctx.SharedMemory.ReleaseDatabase(platformID)

	state = avax.NewPrefixedState(smDB, vm.codec, vm.ctx.ChainID, platformChainID)

	if _, err := state.UTXO(utxoID); err == nil {
		t.Fatalf("should have failed to read the utxo")
	}
}

func TestExportTxNotState(t *testing.T) {
	intf := interface{}(&ExportTx{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
