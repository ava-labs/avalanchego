// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

func TestMultisigCredentialVerify(t *testing.T) {
	require := require.New(t)
	cred := MultisigCredential{}
	require.NoError(cred.Verify())
}

func TestMultisigCredentialUnordered(t *testing.T) {
	require := require.New(t)
	cred := MultisigCredential{}
	cred.SigIdxs = []uint32{0, 0}
	require.ErrorIs(cred.Verify(), errSigIdxsNotUniqueOrSorted)
}

func TestMultisigCredentialSerialize(t *testing.T) {
	require := require.New(t)
	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	require.NoError(m.RegisterCodec(0, c))

	expected := []byte{
		// Codec version
		0x00, 0x00,
		// length:
		0x00, 0x00, 0x00, 0x01,
		// sig[0]
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1e, 0x1d, 0x1f,
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2e, 0x2d, 0x2f,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
		0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
		0x00,
		// length sigidx
		0x00, 0x00, 0x00, 0x01,
		// sigidx[0]
		0x01, 0x02, 0x03, 0x04,
	}
	cred := MultisigCredential{
		Credential: Credential{
			Sigs: [][secp256k1.SignatureLen]byte{
				{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
					0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
					0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1e, 0x1d, 0x1f,
					0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
					0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2e, 0x2d, 0x2f,
					0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
					0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
					0x00,
				},
			},
		},
		SigIdxs: []uint32{0x01020304},
	}
	require.NoError(cred.Verify())

	result, err := m.Marshal(0, &cred)
	require.NoError(err)
	require.Equal(expected, result)
}
