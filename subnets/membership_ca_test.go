// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking/stakingtest"
)

const testLeafValidity = 30 * time.Minute

func verifies(ca *MembershipCA, chain []*x509.Certificate) bool {
	_, ok := ca.Certify(chain)
	return ok
}

func TestParseMembershipCA(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)
	otherRoot, otherIntermediate := stakingtest.NewPKI(t)
	otherChain := stakingtest.NodeChain(t, otherIntermediate, "rpc-02", testLeafValidity)

	// A staged root rotation trusts both roots.
	ca, err := ParseMembershipCA(slices.Concat(root.CertPEM(), otherRoot.CertPEM()))
	require.NoError(err)
	require.True(verifies(ca, chain))
	require.True(verifies(ca, otherChain))

	// An intermediate can be the trust anchor.
	ca, err = ParseMembershipCA(intermediate.CertPEM())
	require.NoError(err)
	require.True(verifies(ca, chain))

	_, err = ParseMembershipCA([]byte("not PEM at all"))
	require.ErrorIs(err, ErrNoMembershipCACertificates)

	// A rotation file whose copy was interrupted: the first root parses, so
	// without the check it would load as a CA trusting only the old fleet.
	_, err = ParseMembershipCA(slices.Concat(root.CertPEM(), []byte("-----BEGIN CERTIFICATE-----\nMIIB")))
	require.ErrorIs(err, errTruncatedPEM)
}

func TestMembershipCACertify(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)
	ca, err := ParseMembershipCA(root.CertPEM())
	require.NoError(err)

	// Membership ends with the earliest certificate in the chain, here the leaf.
	expiresAt, ok := ca.Certify(chain)
	require.True(ok)
	require.Equal(chain[0].NotAfter, expiresAt)

	// A node that installed its leaf without the intermediate.
	require.False(verifies(ca, chain[:1]))
	// Every stock peer, and every validator.
	require.False(verifies(ca, stakingtest.SelfSignedChain(t)))
	_, otherIntermediate := stakingtest.NewPKI(t)
	require.False(verifies(ca, stakingtest.NodeChain(t, otherIntermediate, "rpc-02", testLeafValidity)))
	require.False(verifies(ca, stakingtest.NodeChain(t, intermediate, "rpc-03", -time.Minute)))
	// The whole chain's validity is checked, so a valid leaf stops being a
	// member when its issuer expires.
	expiredIntermediate := root.NewIntermediateCA(t, "Expired Nodes CA", -time.Minute)
	require.False(verifies(ca, stakingtest.NodeChain(t, expiredIntermediate, "rpc-04", testLeafValidity)))
	require.False(verifies(ca, nil))

	var nilCA *MembershipCA
	require.False(verifies(nilCA, chain))
}

// TestMembershipCACertifyBoundsPathBuilding checks the limits on the work a
// peer's chain can cause: keys no staking certificate holds, and chains longer
// than maxMembershipChainLen, are refused before path building.
func TestMembershipCACertifyBoundsPathBuilding(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)
	ca, err := ParseMembershipCA(root.CertPEM())
	require.NoError(err)

	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(err)
	p384Intermediate := root.NewIntermediateCAWithKey(t, "P-384 Nodes CA", stakingtest.DefaultValidity, key)
	require.False(verifies(ca, stakingtest.NodeChain(t, p384Intermediate, "rpc-05", testLeafValidity)))

	// Decoys carrying the real issuer's subject are each a candidate parent to
	// check a signature against.
	decoys := make([]*x509.Certificate, 0, maxMembershipChainLen)
	for range maxMembershipChainLen {
		otherRoot := stakingtest.NewRootCA(t, "Test Root CA", stakingtest.DefaultValidity)
		decoys = append(decoys, otherRoot.NewIntermediateCA(t, "Test Nodes CA", stakingtest.DefaultValidity).Certificate())
	}
	padded := slices.Concat(chain[:1], decoys, chain[1:])
	require.Greater(len(padded), maxMembershipChainLen)
	require.False(verifies(ca, padded))

	// Within the limit, decoys are only wasted work.
	withinLimit := slices.Concat(chain[:1], decoys[:maxMembershipChainLen-2], chain[1:])
	require.Len(withinLimit, maxMembershipChainLen)
	require.True(verifies(ca, withinLimit))
}

func TestConfigLoadMembershipCA(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)

	path := filepath.Join(t.TempDir(), "membership-ca.pem")
	require.NoError(os.WriteFile(path, root.CertPEM(), 0o600))
	fromPath := Config{MembershipCAPath: path}
	ca, err := fromPath.LoadMembershipCA()
	require.NoError(err)
	require.True(verifies(ca, chain))

	inline := Config{MembershipCA: []string{string(root.CertPEM())}}
	ca, err = inline.LoadMembershipCA()
	require.NoError(err)
	require.True(verifies(ca, chain))

	unset := Config{}
	ca, err = unset.LoadMembershipCA()
	require.NoError(err)
	require.Nil(ca)

	missing := Config{MembershipCAPath: filepath.Join(t.TempDir(), "absent.pem")}
	_, err = missing.LoadMembershipCA()
	require.ErrorIs(err, os.ErrNotExist)
}
