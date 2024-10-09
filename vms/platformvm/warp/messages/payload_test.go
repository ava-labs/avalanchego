// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestInvalidPayload(t *testing.T) {
	invalidMessage := []byte{255, 255, 255, 255}
	_, err := Parse(invalidMessage)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}

func TestRegisterSubnetValidator(t *testing.T) {
	require := require.New(t)
	secretKey, err := bls.NewSecretKey()
	require.NoError(err)
	publicKey := bls.PublicFromSecretKey(secretKey)
	pubKeyBytes := *(*[48]byte)(bls.PublicKeyToCompressedBytes(publicKey))

	rsv, err := NewRegisterSubnetValidator(ids.GenerateTestID(), ids.GenerateTestID(), 1000, pubKeyBytes, 9999)
	require.NoError(err)

	recovered, err := ParseRegisterSubnetValidator(rsv.Bytes())
	require.NoError(err)
	require.Equal(rsv, recovered)
}

func TestSetSubnetValidatorWeight(t *testing.T) {
	require := require.New(t)
	ssvw, err := NewSetSubnetValidatorWeight(ids.GenerateTestID(), 1, 1000)
	require.NoError(err)

	recovered, err := ParseSetSubnetValidatorWeight(ssvw.Bytes())
	require.NoError(err)
	require.Equal(ssvw, recovered)
}

func TestSubnetValidatorRegistration(t *testing.T) {
	require := require.New(t)
	validationID := ids.GenerateTestID()
	svr, err := NewSubnetValidatorRegistration(validationID, true)
	require.NoError(err)

	recovered, err := ParseSubnetValidatorRegistration(svr.Bytes())
	require.NoError(err)
	require.Equal(svr, recovered)
}

func TestSubnetValidatorWeightUpdate(t *testing.T) {
	require := require.New(t)
	svwu, err := NewSubnetValidatorWeightUpdate(ids.GenerateTestID(), 2, 2000)
	require.NoError(err)

	recovered, err := ParseSubnetValidatorWeightUpdate(svwu.Bytes())
	require.NoError(err)
	require.Equal(svwu, recovered)
}
