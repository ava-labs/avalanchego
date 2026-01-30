// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errTest = errors.New("non-nil error")

func TestInitialStateVerifySerialization(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&secp256k1fx.TransferOutput{}))
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(CodecVersion, c))

	expected := []byte{
		// Codec version:
		0x00, 0x00,
		// fxID:
		0x00, 0x00, 0x00, 0x00,
		// num outputs:
		0x00, 0x00, 0x00, 0x01,
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

	is := &InitialState{
		FxIndex: 0,
		Outs: []verify.State{
			&secp256k1fx.TransferOutput{
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
		},
	}

	isBytes, err := m.Marshal(CodecVersion, is)
	require.NoError(err)
	require.Equal(expected, isBytes)
}

func TestInitialStateVerifyNil(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(CodecVersion, c))
	numFxs := 1

	is := (*InitialState)(nil)
	err := is.Verify(m, numFxs)
	require.ErrorIs(err, ErrNilInitialState)
}

func TestInitialStateVerifyUnknownFxID(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(CodecVersion, c))
	numFxs := 1

	is := InitialState{
		FxIndex: 1,
	}
	err := is.Verify(m, numFxs)
	require.ErrorIs(err, ErrUnknownFx)
}

func TestInitialStateVerifyNilOutput(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(CodecVersion, c))
	numFxs := 1

	is := InitialState{
		FxIndex: 0,
		Outs:    []verify.State{nil},
	}
	err := is.Verify(m, numFxs)
	require.ErrorIs(err, ErrNilFxOutput)
}

func TestInitialStateVerifyInvalidOutput(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&avax.TestState{}))
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(CodecVersion, c))
	numFxs := 1

	is := InitialState{
		FxIndex: 0,
		Outs:    []verify.State{&avax.TestState{Err: errTest}},
	}
	err := is.Verify(m, numFxs)
	require.ErrorIs(err, errTest)
}

func TestInitialStateVerifyUnsortedOutputs(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	require.NoError(c.RegisterType(&avax.TestTransferable{}))
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(CodecVersion, c))
	numFxs := 1

	is := InitialState{
		FxIndex: 0,
		Outs: []verify.State{
			&avax.TestTransferable{Val: 1},
			&avax.TestTransferable{Val: 0},
		},
	}
	err := is.Verify(m, numFxs)
	require.ErrorIs(err, ErrOutputsNotSorted)
	is.Sort(m)
	require.NoError(is.Verify(m, numFxs))
}

func TestInitialStateCompare(t *testing.T) {
	tests := []struct {
		a        *InitialState
		b        *InitialState
		expected int
	}{
		{
			a:        &InitialState{},
			b:        &InitialState{},
			expected: 0,
		},
		{
			a: &InitialState{
				FxIndex: 1,
			},
			b:        &InitialState{},
			expected: 1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%d", test.a.FxIndex, test.b.FxIndex, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}
