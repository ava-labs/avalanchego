// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"testing"

	"github.com/stretchr/testify/require"

	_ "embed"
)

var (
	//go:embed large_rsa_key.cert
	largeRSAKeyCert []byte
)

func TestParseCheckLargeCert(t *testing.T) {
	_, err := ParseCertificate(largeRSAKeyCert)
	require.ErrorIs(t, err, ErrCertificateTooLarge)
}

func BenchmarkParse(b *testing.B) {
	tlsCert, err := NewTLSCert()
	require.NoError(b, err)

	bytes := tlsCert.Leaf.Raw
	for i := 0; i < b.N; i++ {
		_, err = ParseCertificate(bytes)
		require.NoError(b, err)
	}
}
