// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakingtest

import (
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
)

func TestCAIssueNodeCert(t *testing.T) {
	require := require.New(t)

	root, nodesCA := NewPKI(t)

	certPEM, keyPEM, err := nodesCA.IssueNodeCert("rpc-01", DefaultValidity)
	require.NoError(err)

	// The bundle is what a node loads: leaf first, then the issuing CA. It must
	// be loadable by the node itself before it is worth anything to a peer.
	tlsCert, err := staking.LoadTLSCertFromBytes(keyPEM, certPEM)
	require.NoError(err)
	require.Len(tlsCert.Certificate, 2)

	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(err)
	require.LessOrEqual(len(leaf.Raw), staking.MaxCertificateLen)

	roots := x509.NewCertPool()
	require.True(roots.AppendCertsFromPEM(root.CertPEM()))

	intermediates := x509.NewCertPool()
	intermediates.AddCert(nodesCA.Certificate())

	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	require.NoError(err)
}

func TestCAIntermediateCannotSignACA(t *testing.T) {
	require := require.New(t)

	root, nodesCA := NewPKI(t)

	// pathlen:0 on the intermediate means a chain through a second
	// intermediate cannot be built, which is what keeps the issuing key from
	// minting its own issuers.
	grandchild, err := nodesCA.NewIntermediateCA("Rogue CA", DefaultValidity)
	require.NoError(err)

	leaf := NodeChain(t, grandchild, "rogue-01", DefaultValidity)[0]

	roots := x509.NewCertPool()
	require.True(roots.AppendCertsFromPEM(root.CertPEM()))

	intermediates := x509.NewCertPool()
	intermediates.AddCert(nodesCA.Certificate())
	intermediates.AddCert(grandchild.Certificate())

	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	var invalid x509.CertificateInvalidError
	require.ErrorAs(err, &invalid)
	require.Equal(x509.TooManyIntermediates, invalid.Reason)
}
