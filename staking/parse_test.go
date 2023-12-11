// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"testing"

	_ "embed"

	"github.com/stretchr/testify/require"
)

//go:embed large_rsa_key.cert
var largeRSAKeyCert []byte

func TestParseCertificateCheckLargePublicKey(t *testing.T) {
	_, err := ParseCertificate(largeRSAKeyCert)
	require.ErrorIs(t, err, ErrInvalidRSAPublicKey)
}

func TestParseCertificatePermissiveCheckLargePublicKey(t *testing.T) {
	_, err := ParseCertificatePermissive(largeRSAKeyCert)
	require.ErrorIs(t, err, ErrInvalidRSAPublicKey)
}
