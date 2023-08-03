// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCertificate(t *testing.T) {
	require := require.New(t)

	tlsCert, err := NewTLSCert()
	require.NoError(err)

	x509Cert, err := x509.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	x509StakingCert := CertificateFromX509(x509Cert)

	stakingCert, err := ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	require.Equal(x509StakingCert, stakingCert)
}
