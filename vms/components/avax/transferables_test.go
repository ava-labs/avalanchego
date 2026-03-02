// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestTransferableOutputVerifyNil(t *testing.T) {
	to := (*TransferableOutput)(nil)
	err := to.Verify()
	require.ErrorIs(t, err, ErrNilTransferableOutput)
}

func TestTransferableOutputVerifyNilFx(t *testing.T) {
	to := &TransferableOutput{Asset: Asset{ID: ids.Empty}}
	err := to.Verify()
	require.ErrorIs(t, err, ErrNilTransferableFxOutput)
}

func TestTransferableOutputVerify(t *testing.T) {
	require := require.New(t)

	assetID := ids.GenerateTestID()
	to := &TransferableOutput{
		Asset: Asset{ID: assetID},
		Out:   &TestTransferable{Val: 1},
	}
	require.NoError(to.Verify())
	require.Equal(to.Out, to.Output())
}

func TestTransferableOutputSorting(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&TestTransferable{}))
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(codecVersion, c))

	assetID1 := ids.ID{1}
	outs := []*TransferableOutput{
		{
			Asset: Asset{ID: assetID1},
			Out:   &TestTransferable{Val: 1},
		},
		{
			Asset: Asset{ID: ids.Empty},
			Out:   &TestTransferable{Val: 1},
		},
		{
			Asset: Asset{ID: assetID1},
			Out:   &TestTransferable{Val: 0},
		},
		{
			Asset: Asset{ID: ids.Empty},
			Out:   &TestTransferable{Val: 0},
		},
		{
			Asset: Asset{ID: ids.Empty},
			Out:   &TestTransferable{Val: 0},
		},
	}

	require.False(IsSortedTransferableOutputs(outs, manager))
	SortTransferableOutputs(outs, manager)
	require.True(IsSortedTransferableOutputs(outs, manager))
	require.Zero(outs[0].Out.(*TestTransferable).Val)
	require.Zero(outs[1].Out.(*TestTransferable).Val)
	require.Equal(uint64(1), outs[2].Out.(*TestTransferable).Val)
	require.Equal(assetID1, outs[3].AssetID())
	require.Equal(assetID1, outs[4].AssetID())
}

func TestTransferableOutputSerialization(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&secp256k1fx.TransferOutput{}))
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(codecVersion, c))

	expected := []byte{
		// Codec version
		0x00, 0x00,
		// assetID:
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		// output:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x30, 0x39, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xd4, 0x31, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x02, 0x51, 0x02, 0x5c, 0x61,
		0xfb, 0xcf, 0xc0, 0x78, 0xf6, 0x93, 0x34, 0xf8,
		0x34, 0xbe, 0x6d, 0xd2, 0x6d, 0x55, 0xa9, 0x55,
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
	}

	out := &TransferableOutput{
		Asset: Asset{
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
					{
						0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
						0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
						0x6d, 0x55, 0xa9, 0x55,
					},
					{
						0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
						0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
						0x43, 0xab, 0x08, 0x59,
					},
				},
			},
		},
	}

	outBytes, err := manager.Marshal(codecVersion, out)
	require.NoError(err)
	require.Equal(expected, outBytes)
}

func TestTransferableInputVerifyNil(t *testing.T) {
	ti := (*TransferableInput)(nil)
	err := ti.Verify()
	require.ErrorIs(t, err, ErrNilTransferableInput)
}

func TestTransferableInputVerifyNilFx(t *testing.T) {
	ti := &TransferableInput{
		UTXOID: UTXOID{TxID: ids.Empty},
		Asset:  Asset{ID: ids.Empty},
	}
	err := ti.Verify()
	require.ErrorIs(t, err, ErrNilTransferableFxInput)
}

func TestTransferableInputVerify(t *testing.T) {
	require := require.New(t)

	assetID := ids.GenerateTestID()
	ti := &TransferableInput{
		UTXOID: UTXOID{TxID: assetID},
		Asset:  Asset{ID: assetID},
		In:     &TestTransferable{},
	}
	require.NoError(ti.Verify())
	require.Equal(ti.In, ti.Input())
}

func TestTransferableInputSorting(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&TestTransferable{}))

	ins := []*TransferableInput{
		{
			UTXOID: UTXOID{
				TxID:        ids.ID{1},
				OutputIndex: 1,
			},
			Asset: Asset{ID: ids.Empty},
			In:    &TestTransferable{},
		},
		{
			UTXOID: UTXOID{
				TxID:        ids.ID{1},
				OutputIndex: 0,
			},
			Asset: Asset{ID: ids.Empty},
			In:    &TestTransferable{},
		},
		{
			UTXOID: UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			Asset: Asset{ID: ids.Empty},
			In:    &TestTransferable{},
		},
		{
			UTXOID: UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
			Asset: Asset{ID: ids.Empty},
			In:    &TestTransferable{},
		},
	}

	require.False(utils.IsSortedAndUnique(ins))
	utils.Sort(ins)
	require.True(utils.IsSortedAndUnique(ins))

	ins = append(ins, &TransferableInput{
		UTXOID: UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: Asset{ID: ids.Empty},
		In:    &TestTransferable{},
	})

	require.False(utils.IsSortedAndUnique(ins))
}

func TestTransferableInputSerialization(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&secp256k1fx.TransferInput{}))
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(codecVersion, c))

	expected := []byte{
		// Codec version
		0x00, 0x00,
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
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x07, 0x5b, 0xcd, 0x15, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x07,
	}

	in := &TransferableInput{
		UTXOID: UTXOID{
			TxID: ids.ID{
				0xf1, 0xe1, 0xd1, 0xc1, 0xb1, 0xa1, 0x91, 0x81,
				0x71, 0x61, 0x51, 0x41, 0x31, 0x21, 0x11, 0x01,
				0xf0, 0xe0, 0xd0, 0xc0, 0xb0, 0xa0, 0x90, 0x80,
				0x70, 0x60, 0x50, 0x40, 0x30, 0x20, 0x10, 0x00,
			},
			OutputIndex: 5,
		},
		Asset: Asset{
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
	}

	inBytes, err := manager.Marshal(codecVersion, in)
	require.NoError(err)
	require.Equal(expected, inBytes)
}
