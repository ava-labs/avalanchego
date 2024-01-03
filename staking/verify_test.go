// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"testing"

	_ "embed"

	"github.com/stretchr/testify/require"
)

var (
	//go:embed large_rsa_key.cert
	largeRSAKeyCert []byte
	//go:embed large_rsa_key.sig
	largeRSAKeySig []byte
)

func TestCheckSignatureLargePublicKey(t *testing.T) {
	require := require.New(t)

	cert, err := ParseCertificate(largeRSAKeyCert)
	require.NoError(err)

	msg := []byte("TODO: put something clever")
	err = CheckSignature(cert, msg, largeRSAKeySig)
	require.ErrorIs(err, ErrInvalidRSAPublicKey)
}
