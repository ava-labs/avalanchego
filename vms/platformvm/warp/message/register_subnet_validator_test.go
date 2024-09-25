// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func newBLSPublicKey(t *testing.T) [bls.PublicKeyLen]byte {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToCompressedBytes(pk)
	return [bls.PublicKeyLen]byte(pkBytes)
}

func TestRegisterSubnetValidator(t *testing.T) {
	require := require.New(t)

	msg, err := NewRegisterSubnetValidator(
		ids.GenerateTestID(),
		ids.GenerateTestNodeID(),
		rand.Uint64(), //#nosec G404
		newBLSPublicKey(t),
		rand.Uint64(), //#nosec G404
	)
	require.NoError(err)

	parsed, err := ParseRegisterSubnetValidator(msg.Bytes())
	require.NoError(err)
	require.Equal(msg, parsed)
}
