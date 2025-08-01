// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestParse(t *testing.T) {
	mustCreate := func(msg Payload, err error) Payload {
		require.NoError(t, err)
		return msg
	}
	tests := []struct {
		name        string
		bytes       []byte
		expected    Payload
		expectedErr error
	}{
		{
			name:        "invalid message",
			bytes:       []byte{255, 255, 255, 255},
			expectedErr: codec.ErrUnknownVersion,
		},
		{
			name: "SubnetToL1Conversion",
			bytes: []byte{
				// Codec version:
				0x00, 0x00,
				// Payload type = SubnetToL1Conversion:
				0x00, 0x00, 0x00, 0x00,
				// ID:
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
			},
			expected: mustCreate(NewSubnetToL1Conversion(
				ids.ID{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				},
			)),
		},
		{
			name: "RegisterL1Validator",
			bytes: []byte{
				// Codec version:
				0x00, 0x00,
				// Payload type = RegisterL1Validator:
				0x00, 0x00, 0x00, 0x01,
				// SubnetID:
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				// NodeID Length:
				0x00, 0x00, 0x00, 0x14,
				// NodeID:
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30,
				0x31, 0x32, 0x33, 0x34,
				// BLSPublicKey:
				0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c,
				0x3d, 0x3e, 0x3f, 0x40, 0x41, 0x42, 0x43, 0x44,
				0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c,
				0x4d, 0x4e, 0x4f, 0x50, 0x51, 0x52, 0x53, 0x54,
				0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c,
				0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64,
				// Expiry:
				0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c,
				// Remaining Balance Owner Threshold:
				0x6d, 0x6e, 0x6f, 0x70,
				// Remaining Balance Owner Addresses Length:
				0x00, 0x00, 0x00, 0x01,
				// Remaining Balance Owner Address[0]:
				0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
				0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80,
				0x81, 0x82, 0x83, 0x84,
				// Disable Owner Threshold:
				0x85, 0x86, 0x87, 0x88,
				// Disable Owner Addresses Length:
				0x00, 0x00, 0x00, 0x01,
				// Disable Owner Address[0]:
				0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90,
				0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
				0x99, 0x9a, 0x9b, 0x9c,
				// Weight:
				0x9d, 0x9e, 0x9f, 0xa0, 0xa1, 0xa2, 0xa3, 0xa4,
			},
			expected: mustCreate(NewRegisterL1Validator(
				ids.ID{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				},
				ids.NodeID{
					0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
					0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30,
					0x31, 0x32, 0x33, 0x34,
				},
				[bls.PublicKeyLen]byte{
					0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c,
					0x3d, 0x3e, 0x3f, 0x40, 0x41, 0x42, 0x43, 0x44,
					0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c,
					0x4d, 0x4e, 0x4f, 0x50, 0x51, 0x52, 0x53, 0x54,
					0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c,
					0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64,
				},
				0x65666768696a6b6c,
				PChainOwner{
					Threshold: 0x6d6e6f70,
					Addresses: []ids.ShortID{
						{
							0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
							0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80,
							0x81, 0x82, 0x83, 0x84,
						},
					},
				},
				PChainOwner{
					Threshold: 0x85868788,
					Addresses: []ids.ShortID{
						{
							0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90,
							0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
							0x99, 0x9a, 0x9b, 0x9c,
						},
					},
				},
				0x9d9e9fa0a1a2a3a4,
			)),
		},
		{
			name: "L1ValidatorRegistration",
			bytes: []byte{
				// Codec version:
				0x00, 0x00,
				// Payload type = L1ValidatorRegistration:
				0x00, 0x00, 0x00, 0x02,
				// ValidationID:
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				// Registered:
				0x00,
			},
			expected: mustCreate(NewL1ValidatorRegistration(
				ids.ID{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				},
				false,
			)),
		},
		{
			name: "L1ValidatorWeight",
			bytes: []byte{
				// Codec version:
				0x00, 0x00,
				// Payload type = L1ValidatorWeight:
				0x00, 0x00, 0x00, 0x03,
				// ValidationID:
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				// Nonce:
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				// Weight:
				0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30,
			},
			expected: mustCreate(NewL1ValidatorWeight(
				ids.ID{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				},
				0x2122232425262728,
				0x292a2b2c2d2e2f30,
			)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			msg, err := Parse(test.bytes)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, msg)
			if msg != nil {
				require.Equal(test.bytes, msg.Bytes())
			}
		})
	}
}
