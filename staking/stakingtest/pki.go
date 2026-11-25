// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakingtest

import (
	"crypto/x509"
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
	root = NewRootCA(t, "Test Root CA", DefaultValidity)
	return root, root.NewIntermediateCA(t, "Test Nodes CA", DefaultValidity)
}

// NodeChain issues a node certificate from [ca] and returns the chain that node
// presents during the TLS handshake: the leaf first, then its issuing CA.
func NodeChain(t *testing.T, ca *CA, commonName string, validity time.Duration) []*x509.Certificate {
	t.Helper()

	leaf, _ := ca.issueNode(t, commonName, validity)
	return []*x509.Certificate{leaf, ca.cert}
}

// SelfSignedChain is what every stock peer, and every validator, presents: a
// single self-signed certificate that chains to no CA.
func SelfSignedChain(t *testing.T) []*x509.Certificate {
	t.Helper()

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	return []*x509.Certificate{tlsCert.Leaf}
}
