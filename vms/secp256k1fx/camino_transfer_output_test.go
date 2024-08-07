// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
)

var validAddress = ids.ShortID{0x00, 0x01, 0x02, 0x03, 0x04}

func TestCrossOutputVerify(t *testing.T) {
	require := require.New(t)
	out := CrossTransferOutput{
		TransferOutput: TransferOutput{
			Amt: 1,
			OutputOwners: OutputOwners{
				Locktime:  1,
				Threshold: 1,
				Addrs: []ids.ShortID{
					ids.ShortEmpty,
				},
			},
		},
		Recipient: validAddress,
	}
	require.NoError(out.Verify())
}

func TestCrossOutputVerifyEmpty(t *testing.T) {
	require := require.New(t)
	out := CrossTransferOutput{
		TransferOutput: TransferOutput{
			Amt: 1,
			OutputOwners: OutputOwners{
				Locktime:  1,
				Threshold: 1,
				Addrs: []ids.ShortID{
					ids.ShortEmpty,
				},
			},
		},
		Recipient: ids.ShortEmpty,
	}
	require.ErrorIs(out.Verify(), ErrEmptyRecipient)
}

func TestCrossOutputSerialize(t *testing.T) {
	require := require.New(t)
	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(0, c))

	expected := []byte{
		// Codec version
		0x00, 0x00,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x39,
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xd4, 0x31,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses:
		0x00, 0x00, 0x00, 0x02,
		// addrs[0]:
		0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
		0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
		0x6d, 0x55, 0xa9, 0x55,
		// addrs[1]:
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
		// Recipient:
		0x00, 0x01, 0x02, 0x03, 0x04, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	out := CrossTransferOutput{
		TransferOutput: TransferOutput{
			Amt: 12345,
			OutputOwners: OutputOwners{
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
		Recipient: validAddress,
	}
	require.NoError(out.Verify())

	result, err := m.Marshal(0, &out)
	require.NoError(err)
	require.Equal(expected, result)
}
