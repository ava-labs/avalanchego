// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestTransferInputAmount(t *testing.T) {
	require := require.New(t)
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{0, 1},
		},
	}
	require.Equal(uint64(1), in.Amount())
}

func TestTransferInputVerify(t *testing.T) {
	require := require.New(t)
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{0, 1},
		},
	}
	require.NoError(in.Verify())
}

func TestTransferInputVerifyNil(t *testing.T) {
	require := require.New(t)
	in := (*TransferInput)(nil)
	err := in.Verify()
	require.ErrorIs(err, ErrNilInput)
}

func TestTransferInputVerifyNoValue(t *testing.T) {
	require := require.New(t)
	in := TransferInput{
		Amt: 0,
		Input: Input{
			SigIndices: []uint32{0, 1},
		},
	}
	err := in.Verify()
	require.ErrorIs(err, ErrNoValueInput)
}

func TestTransferInputVerifyDuplicated(t *testing.T) {
	require := require.New(t)
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{0, 0},
		},
	}
	err := in.Verify()
	require.ErrorIs(err, ErrInputIndicesNotSortedUnique)
}

func TestTransferInputVerifyUnsorted(t *testing.T) {
	require := require.New(t)
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{1, 0},
		},
	}
	err := in.Verify()
	require.ErrorIs(err, ErrInputIndicesNotSortedUnique)
}

func TestTransferInputSerialize(t *testing.T) {
	require := require.New(t)
	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(0, c))

	expected := []byte{
		// Codec version
		0x00, 0x00,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x07, 0x5b, 0xcd, 0x15,
		// length:
		0x00, 0x00, 0x00, 0x02,
		// sig[0]
		0x00, 0x00, 0x00, 0x03,
		// sig[1]
		0x00, 0x00, 0x00, 0x07,
	}
	in := TransferInput{
		Amt: 123456789,
		Input: Input{
			SigIndices: []uint32{3, 7},
		},
	}
	require.NoError(in.Verify())

	result, err := m.Marshal(0, &in)
	require.NoError(err)
	require.Equal(expected, result)
}

func TestTransferInputNotState(t *testing.T) {
	require := require.New(t)
	intf := interface{}(&TransferInput{})
	_, ok := intf.(verify.State)
	require.False(ok)
}
