// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakingtest

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
)

// DefaultValidity is long enough that nothing built with it expires mid-test.
const DefaultValidity = time.Hour

// NewPKI returns a root CA and the intermediate that issues node certificates,
// matching the shape the operator guide describes.
func NewPKI(t *testing.T) (root, intermediate *CA) {
	t.Helper()
	require := require.New(t)

	root, err := NewRootCA("Test Root CA", DefaultValidity)
	require.NoError(err)

	intermediate, err = root.NewIntermediateCA("Test Nodes CA", DefaultValidity)
	require.NoError(err)

	return root, intermediate
}

// NodeChain issues a node certificate from [ca] and returns the chain that node
// presents during the TLS handshake: the leaf first, then its issuing CA.
//
// A negative [validity] yields an already-expired leaf, which is how the
// expiry cases are built.
func NodeChain(t *testing.T, ca *CA, commonName string, validity time.Duration) []*x509.Certificate {
	t.Helper()

	certPEM, _, err := ca.IssueNodeCert(commonName, validity)
	require.NoError(t, err)

	chain := CertsFromPEM(t, certPEM)
	require.Len(t, chain, 2)
	return chain
}

// CertsFromPEM decodes concatenated PEM certificate blocks. It is deliberately
// lower level than [staking.LoadTLSCertFromBytes] so that expired certificates
// can be read back for the negative cases.
func CertsFromPEM(t *testing.T, pemBytes []byte) []*x509.Certificate {
	t.Helper()

	var (
		chain []*x509.Certificate
		rest  = pemBytes
	)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return chain
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		chain = append(chain, cert)
	}
}

// SelfSignedChain is what every stock peer, and every validator, presents: a
// single self-signed certificate that chains to no CA.
func SelfSignedChain(t *testing.T) []*x509.Certificate {
	t.Helper()

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(t, err)
	return []*x509.Certificate{cert}
}
