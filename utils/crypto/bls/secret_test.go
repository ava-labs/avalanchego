// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestSecretKeyZero(t *testing.T) {
	require := require.New(t)

	var skArr [SecretKeyLen]byte
	skBytes := skArr[:]
	_, err := SecretKeyFromBytes(skBytes)
	require.ErrorIs(err, errFailedSecretKeyDeserialize)
}

func TestSecretKeyWrongSize(t *testing.T) {
	require := require.New(t)

	skBytes := utils.RandomBytes(SecretKeyLen + 1)
	_, err := SecretKeyFromBytes(skBytes)
	require.ErrorIs(err, errFailedSecretKeyDeserialize)
}
