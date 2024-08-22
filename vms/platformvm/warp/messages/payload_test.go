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
	secretKey, err := bls.NewSecretKey()
	require.NoError(t, err)
	publicKey := bls.PublicFromSecretKey(secretKey)
	pubKeyBytes := *(*[48]byte)(bls.PublicKeyToCompressedBytes(publicKey))

	rsv, err := NewRegisterSubnetValidator(ids.GenerateTestID(), ids.GenerateTestID(), 1000, pubKeyBytes, 9999)
	require.NoError(t, err)

	bytes := rsv.Bytes()
	parsed, err := Parse(bytes)
	require.NoError(t, err)

	_, ok := parsed.(*RegisterSubnetValidator)
	require.True(t, ok)

	recovered, err := ParseRegisterSubnetValidator(bytes)
	require.NoError(t, err)
	require.Equal(t, rsv, recovered)
}

func TestSetSubnetValidatorWeight(t *testing.T) {
	ssvw, err := NewSetSubnetValidatorWeight(ids.GenerateTestID(), 1, 1000)
	require.NoError(t, err)

	bytes := ssvw.Bytes()
	parsed, err := Parse(bytes)
	require.NoError(t, err)

	_, ok := parsed.(*SetSubnetValidatorWeight)
	require.True(t, ok)

	recovered, err := ParseSetSubnetValidatorWeight(bytes)
	require.NoError(t, err)
	require.Equal(t, ssvw, recovered)
}

func TestSubnetValidatorRegistration(t *testing.T) {
	validationID := ids.GenerateTestID()
	svr, err := NewSubnetValidatorRegistration(validationID, true)
	require.NoError(t, err)

	bytes := svr.Bytes()
	parsed, err := Parse(bytes)
	require.NoError(t, err)

	_, ok := parsed.(*SubnetValidatorRegistration)
	require.True(t, ok)

	recovered, err := ParseSubnetValidatorRegistration(bytes)
	require.NoError(t, err)
	require.Equal(t, svr, recovered)
}

func TestSubnetValidatorWeightUpdate(t *testing.T) {
	svwu, err := NewSubnetValidatorWeightUpdate(ids.GenerateTestID(), 2, 2000)
	require.NoError(t, err)

	bytes := svwu.Bytes()
	parsed, err := Parse(bytes)
	require.NoError(t, err)

	_, ok := parsed.(*SubnetValidatorWeightUpdate)
	require.True(t, ok)

	recovered, err := ParseSubnetValidatorWeightUpdate(bytes)
	require.NoError(t, err)
	require.Equal(t, svwu, recovered)
}
