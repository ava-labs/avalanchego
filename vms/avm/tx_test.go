// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestTxNil(t *testing.T) {
	c := codec.NewDefault()
	tx := (*Tx)(nil)
	if err := tx.SyntacticVerify(ctx, c, 1); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
}

func TestTxEmpty(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&BaseTx{})
	c.RegisterType(&CreateAssetTx{})
	c.RegisterType(&OperationTx{})

	tx := &Tx{}
	if err := tx.SyntacticVerify(ctx, c, 1); err == nil {
		t.Fatalf("Should have errored due to nil tx")
	}
}

func TestTxInvalidCredential(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&BaseTx{})
	c.RegisterType(&CreateAssetTx{})
	c.RegisterType(&OperationTx{})
	c.RegisterType(&secp256k1fx.MintOutput{})
	c.RegisterType(&secp256k1fx.TransferOutput{})
	c.RegisterType(&secp256k1fx.MintInput{})
	c.RegisterType(&secp256k1fx.TransferInput{})
	c.RegisterType(&secp256k1fx.Credential{})
	c.RegisterType(&testVerifiable{})

	tx := &Tx{
		UnsignedTx: &OperationTx{BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*TransferableInput{
				&TransferableInput{
					UTXOID: UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: Asset{
						ID: asset,
					},
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
		}},
		Creds: []verify.Verifiable{
			&testVerifiable{err: errUnneededAddress},
		},
	}

	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := tx.SyntacticVerify(ctx, c, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid credential")
	}
}

func TestTxInvalidUnsignedTx(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&BaseTx{})
	c.RegisterType(&CreateAssetTx{})
	c.RegisterType(&OperationTx{})
	c.RegisterType(&secp256k1fx.MintOutput{})
	c.RegisterType(&secp256k1fx.TransferOutput{})
	c.RegisterType(&secp256k1fx.MintInput{})
	c.RegisterType(&secp256k1fx.TransferInput{})
	c.RegisterType(&secp256k1fx.Credential{})
	c.RegisterType(&testVerifiable{})

	tx := &Tx{
		UnsignedTx: &OperationTx{BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
			Ins: []*TransferableInput{
				&TransferableInput{
					UTXOID: UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: Asset{
						ID: asset,
					},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloAva,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
				&TransferableInput{
					UTXOID: UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: Asset{
						ID: asset,
					},
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
		}},
		Creds: []verify.Verifiable{
			&testVerifiable{},
			&testVerifiable{},
		},
	}

	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := tx.SyntacticVerify(ctx, c, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid unsigned tx")
	}
}

func TestTxInvalidNumberOfCredentials(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&BaseTx{})
	c.RegisterType(&CreateAssetTx{})
	c.RegisterType(&OperationTx{})
	c.RegisterType(&secp256k1fx.MintOutput{})
	c.RegisterType(&secp256k1fx.TransferOutput{})
	c.RegisterType(&secp256k1fx.MintInput{})
	c.RegisterType(&secp256k1fx.TransferInput{})
	c.RegisterType(&secp256k1fx.Credential{})
	c.RegisterType(&testVerifiable{})

	tx := &Tx{
		UnsignedTx: &OperationTx{
			BaseTx: BaseTx{
				NetID: networkID,
				BCID:  chainID,
				Ins: []*TransferableInput{
					&TransferableInput{
						UTXOID: UTXOID{
							TxID:        ids.Empty,
							OutputIndex: 0,
						},
						Asset: Asset{
							ID: asset,
						},
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
			Ops: []*Operation{
				&Operation{
					Asset: Asset{
						ID: asset,
					},
					Ins: []*OperableInput{
						&OperableInput{
							UTXOID: UTXOID{
								TxID:        ids.Empty,
								OutputIndex: 1,
							},
							In: &testVerifiable{},
						},
					},
				},
			},
		},
		Creds: []verify.Verifiable{
			&testVerifiable{},
		},
	}

	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := tx.SyntacticVerify(ctx, c, 1); err == nil {
		t.Fatalf("Tx should have failed due to an invalid unsigned tx")
	}
}

func TestTxDocumentation(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&BaseTx{})
	c.RegisterType(&CreateAssetTx{})
	c.RegisterType(&OperationTx{})
	c.RegisterType(&secp256k1fx.MintOutput{})
	c.RegisterType(&secp256k1fx.TransferOutput{})
	c.RegisterType(&secp256k1fx.MintInput{})
	c.RegisterType(&secp256k1fx.TransferInput{})
	c.RegisterType(&secp256k1fx.Credential{})

	txBytes := []byte{
		// unsigned transaction:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
		0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
		0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
		0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
		0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
		0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
		0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b,
		0x1c, 0x1d, 0x1e, 0x1f, 0x00, 0x00, 0x00, 0x04,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x39,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xd4, 0x31,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02,
		0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
		0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
		0x6d, 0x55, 0xa9, 0x55, 0xc3, 0x34, 0x41, 0x28,
		0xe0, 0x60, 0x12, 0x8e, 0xde, 0x35, 0x23, 0xa2,
		0x4a, 0x46, 0x1c, 0x89, 0x43, 0xab, 0x08, 0x59,
		0x00, 0x00, 0x00, 0x01, 0xf1, 0xe1, 0xd1, 0xc1,
		0xb1, 0xa1, 0x91, 0x81, 0x71, 0x61, 0x51, 0x41,
		0x31, 0x21, 0x11, 0x01, 0xf0, 0xe0, 0xd0, 0xc0,
		0xb0, 0xa0, 0x90, 0x80, 0x70, 0x60, 0x50, 0x40,
		0x30, 0x20, 0x10, 0x00, 0x00, 0x00, 0x00, 0x05,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x00,
		0x07, 0x5b, 0xcd, 0x15, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x07,
		// number of credentials:
		0x00, 0x00, 0x00, 0x01,
		// credential[0]:
		0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1e, 0x1d, 0x1f,
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2e, 0x2d, 0x2f,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
		0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
		0x00, 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46,
		0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e,
		0x4f, 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56,
		0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5e, 0x5d,
		0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66,
		0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6e, 0x6d,
		0x6f, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76,
		0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e,
		0x7f, 0x00,
	}

	tx := Tx{}
	err := c.Unmarshal(txBytes, &tx)
	if err != nil {
		t.Fatal(err)
	}
}
