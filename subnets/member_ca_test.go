// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking/stakingtest"
)

const testLeafValidity = 30 * time.Minute

func verifies(ca *MemberCA, chain []*x509.Certificate) bool {
	_, ok := ca.VerifyUntil(chain)
	return ok
}

func TestParseMemberCA(t *testing.T) {
	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)

	t.Run("single root", func(t *testing.T) {
		ca, err := ParseMemberCA(root.CertPEM())
		require.NoError(t, err)
		require.True(t, verifies(ca, chain))
	})

	t.Run("staged root rotation trusts both roots", func(t *testing.T) {
		require := require.New(t)

		otherRoot, otherIntermediate := stakingtest.NewPKI(t)
		otherChain := stakingtest.NodeChain(t, otherIntermediate, "rpc-02", testLeafValidity)

		ca, err := ParseMemberCA(append(root.CertPEM(), otherRoot.CertPEM()...))
		require.NoError(err)
		require.True(verifies(ca, chain))
		require.True(verifies(ca, otherChain))
	})

	t.Run("intermediate is a valid root", func(t *testing.T) {
		ca, err := ParseMemberCA(intermediate.CertPEM())
		require.NoError(t, err)
		require.True(t, verifies(ca, chain))
	})

	t.Run("empty", func(t *testing.T) {
		_, err := ParseMemberCA(nil)
		require.ErrorIs(t, err, ErrNoMemberCACertificates)
	})

	t.Run("leaf is not a CA", func(t *testing.T) {
		leafPEM := pem.EncodeToMemory(&pem.Block{
			Type:  certificatePEMType,
			Bytes: chain[0].Raw,
		})
		_, err := ParseMemberCA(leafPEM)
		require.ErrorIs(t, err, errMemberCANotACA)
	})

	t.Run("wrong PEM block type", func(t *testing.T) {
		notACert := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{0x01}})
		_, err := ParseMemberCA(notACert)
		require.ErrorIs(t, err, errUnexpectedPEMBlock)
	})

	t.Run("malformed certificate", func(t *testing.T) {
		garbage := pem.EncodeToMemory(&pem.Block{Type: certificatePEMType, Bytes: []byte("not a certificate")})
		_, err := ParseMemberCA(garbage)
		require.ErrorIs(t, err, ErrMalformedMemberCA)
	})

	t.Run("trailing garbage after a valid root", func(t *testing.T) {
		// The shape a rotation file has when the second root was half written:
		// the first root parses, so without the trailing check this loads as a
		// CA that trusts only the old fleet.
		_, err := ParseMemberCA(append(root.CertPEM(), "-----BEGIN CERTIFI"...))
		require.ErrorIs(t, err, ErrTrailingMemberCAData)
	})

	t.Run("trailing whitespace is not garbage", func(t *testing.T) {
		ca, err := ParseMemberCA(append(root.CertPEM(), "\n\n\t "...))
		require.NoError(t, err)
		require.True(t, verifies(ca, chain))
	})

	t.Run("garbage alone", func(t *testing.T) {
		// Nothing parsed at all, which is the plainer error of the two.
		_, err := ParseMemberCA([]byte("not PEM at all"))
		require.ErrorIs(t, err, ErrNoMemberCACertificates)
	})
}

func TestMemberCAVerifyUntil(t *testing.T) {
	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)

	rootCA, err := ParseMemberCA(root.CertPEM())
	require.NoError(t, err)

	t.Run("leaf and intermediate, expiring with the earliest of the chain", func(t *testing.T) {
		expiresAt, ok := rootCA.VerifyUntil(chain)
		require.True(t, ok)
		// The leaf outlives neither its issuer nor the root, so it is the
		// earliest, and membership ends with it.
		require.Equal(t, chain[0].NotAfter, expiresAt)
	})

	t.Run("leaf without its intermediate", func(t *testing.T) {
		// A node that installed only the leaf, rather than the bundle, sends a
		// chain that cannot be built.
		require.False(t, verifies(rootCA, chain[:1]))
	})

	t.Run("self-signed peer", func(t *testing.T) {
		// This is every stock peer, and every validator: no certificate, so no
		// certificate membership.
		require.False(t, verifies(rootCA, stakingtest.SelfSignedChain(t)))
	})

	t.Run("chain to a different root", func(t *testing.T) {
		_, otherIntermediate := stakingtest.NewPKI(t)
		require.False(t, verifies(rootCA, stakingtest.NodeChain(t, otherIntermediate, "rpc-02", testLeafValidity)))
	})

	t.Run("expired leaf", func(t *testing.T) {
		require.False(t, verifies(rootCA, stakingtest.NodeChain(t, intermediate, "rpc-03", -time.Minute)))
	})

	t.Run("expired intermediate", func(t *testing.T) {
		require := require.New(t)

		// The whole chain's validity is checked, so a leaf that is still valid
		// stops being a member when its issuer expires.
		shortRoot, err := stakingtest.NewRootCA("Short Root CA", stakingtest.DefaultValidity)
		require.NoError(err)

		expiredIntermediate, err := shortRoot.NewIntermediateCA("Expired Nodes CA", -time.Minute)
		require.NoError(err)

		ca, err := ParseMemberCA(shortRoot.CertPEM())
		require.NoError(err)
		require.False(verifies(ca, stakingtest.NodeChain(t, expiredIntermediate, "rpc-04", testLeafValidity)))
	})

	t.Run("empty chain", func(t *testing.T) {
		require.False(t, verifies(rootCA, nil))
	})

	t.Run("chain padded with same-subject decoys", func(t *testing.T) {
		require := require.New(t)

		// A peer that pads its chain with certificates carrying the real
		// issuer's subject makes each one a candidate parent to check a
		// signature against. The length limit is what stops that, so a chain
		// long enough to be worth padding is refused before path building.
		decoys := make([]*x509.Certificate, 0, maxMemberChainLen)
		for i := 0; i < maxMemberChainLen; i++ {
			otherRoot, err := stakingtest.NewRootCA("Test Root CA", stakingtest.DefaultValidity)
			require.NoError(err)

			decoy, err := otherRoot.NewIntermediateCA("Test Nodes CA", stakingtest.DefaultValidity)
			require.NoError(err)

			decoys = append(decoys, decoy.Certificate())
		}

		padded := append([]*x509.Certificate{chain[0]}, decoys...)
		padded = append(padded, chain[1])
		require.Greater(len(padded), maxMemberChainLen)
		require.False(verifies(rootCA, padded))

		// Up to the limit the decoys are only wasted work: the real issuer is
		// still in the chain, so the peer is still a member.
		withinLimit := append([]*x509.Certificate{chain[0]}, decoys[:maxMemberChainLen-2]...)
		withinLimit = append(withinLimit, chain[1])
		require.Len(withinLimit, maxMemberChainLen)
		require.True(verifies(rootCA, withinLimit))
	})

	t.Run("nil CA verifies nothing", func(t *testing.T) {
		var ca *MemberCA
		require.False(t, verifies(ca, chain))
	})
}

func TestConfigLoadMemberCA(t *testing.T) {
	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)

	t.Run("from path", func(t *testing.T) {
		require := require.New(t)

		path := filepath.Join(t.TempDir(), "member-ca.pem")
		require.NoError(os.WriteFile(path, root.CertPEM(), 0o600))

		config := Config{MemberCAPath: path}
		require.NoError(config.LoadMemberCA())
		require.True(verifies(config.MemberCA(), chain))
	})

	t.Run("inline", func(t *testing.T) {
		require := require.New(t)

		config := Config{MemberCAPEMs: []string{string(root.CertPEM())}}
		require.NoError(config.LoadMemberCA())
		require.True(verifies(config.MemberCA(), chain))
	})

	t.Run("unset", func(t *testing.T) {
		require := require.New(t)

		config := Config{}
		require.NoError(config.LoadMemberCA())
		require.Nil(config.MemberCA())
	})

	t.Run("both sources set", func(t *testing.T) {
		config := Config{
			MemberCAPath: "member-ca.pem",
			MemberCAPEMs: []string{string(root.CertPEM())},
		}
		require.ErrorIs(t, config.LoadMemberCA(), ErrTooManyMemberCASources)
	})

	t.Run("missing file", func(t *testing.T) {
		config := Config{MemberCAPath: filepath.Join(t.TempDir(), "absent.pem")}
		require.ErrorIs(t, config.LoadMemberCA(), os.ErrNotExist)
	})

	t.Run("malformed inline", func(t *testing.T) {
		config := Config{MemberCAPEMs: []string{"not a PEM block"}}
		require.ErrorIs(t, config.LoadMemberCA(), ErrNoMemberCACertificates)
	})
}
