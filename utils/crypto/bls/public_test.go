// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestPublicKeyWrongSize(t *testing.T) {
	require := require.New(t)

	pkBytes := utils.RandomBytes(PublicKeyLen + 1)
	_, err := PublicKeyFromBytes(pkBytes)
	require.ErrorIs(err, errFailedPublicKeyDecompress)
}
