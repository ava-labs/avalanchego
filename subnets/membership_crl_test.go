// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking/stakingtest"
)

func crlPEM(t *testing.T, ca *stakingtest.CA, number int64, revoked ...*x509.Certificate) []byte {
	return ca.CRLPEM(t, number, stakingtest.DefaultValidity, revoked...)
}

// reencoded returns [cert] with its ECDSA signature's s replaced by n-s. The
// result is a different certificate, and so a different node ID, that still
// verifies: anyone holding a leaf's key can mint it without the CA.
func reencoded(t *testing.T, cert *x509.Certificate) *x509.Certificate {
	t.Helper()
	require := require.New(t)

	var signature struct{ R, S *big.Int }
	_, err := asn1.Unmarshal(cert.Signature, &signature)
	require.NoError(err)
	signature.S.Sub(elliptic.P256().Params().N, signature.S)
	flipped, err := asn1.Marshal(signature)
	require.NoError(err)

	var outer struct {
		TBSCertificate     asn1.RawValue
		SignatureAlgorithm asn1.RawValue
		SignatureValue     asn1.BitString
	}
	_, err = asn1.Unmarshal(cert.Raw, &outer)
	require.NoError(err)
	outer.SignatureValue = asn1.BitString{Bytes: flipped, BitLength: 8 * len(flipped)}
	der, err := asn1.Marshal(outer)
	require.NoError(err)

	twin, err := x509.ParseCertificate(der)
	require.NoError(err)
	require.NotEqual(cert.Raw, twin.Raw)
	require.Equal(cert.RawTBSCertificate, twin.RawTBSCertificate)
	return twin
}

func TestParseMembershipCAWithCRL(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	revokedChain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)
	keptChain := stakingtest.NodeChain(t, intermediate, "rpc-02", testLeafValidity)

	// A list from the issuing intermediate revokes only what it names.
	ca, err := ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, intermediate, 1, revokedChain[0]))
	require.NoError(err)
	require.Len(ca.CRLs(), 1)
	require.False(verifies(ca, revokedChain))
	require.True(ca.Revokes(revokedChain))
	require.True(verifies(ca, keptChain))
	require.False(ca.Revokes(keptChain))
	// A chain that never verified is not reported as revoked.
	require.False(ca.Revokes(revokedChain[:1]))

	// A list from a root revokes an intermediate and everything under it.
	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, root, 1, intermediate.Certificate()))
	require.NoError(err)
	require.False(verifies(ca, revokedChain))
	require.True(ca.Revokes(keptChain))

	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, intermediate, 1))
	require.NoError(err)
	require.True(verifies(ca, revokedChain))

	// nextUpdate is not enforced.
	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), intermediate.CRLPEM(t, 1, -time.Minute, revokedChain[0]))
	require.NoError(err)
	require.False(verifies(ca, revokedChain))

	// Two lists under one issuer revoke what either names.
	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), slices.Concat(
		crlPEM(t, intermediate, 1, revokedChain[0]),
		crlPEM(t, intermediate, 2, keptChain[0]),
	))
	require.NoError(err)
	require.False(verifies(ca, revokedChain))
	require.False(verifies(ca, keptChain))
}

// TestParseMembershipCAWithCRLRefuses checks the list files that fail the
// start, because taking them as given would re-admit revoked certificates.
func TestParseMembershipCAWithCRLRefuses(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	leaf := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)[0]

	// Dropped without the check, the newest list would re-admit everyone on it.
	newest := crlPEM(t, intermediate, 2, leaf)
	_, err := ParseMembershipCAWithCRL(root.CertPEM(), slices.Concat(crlPEM(t, intermediate, 1, leaf), newest[:len(newest)/2]))
	require.ErrorIs(err, errTruncatedPEM)

	_, err = ParseMembershipCAWithCRL(root.CertPEM(), intermediate.CertPEM())
	require.ErrorIs(err, ErrNoMembershipCRLs)

	garbage := pem.EncodeToMemory(&pem.Block{Type: crlPEMType, Bytes: []byte("not a list")})
	_, err = ParseMembershipCAWithCRL(root.CertPEM(), garbage)
	require.ErrorIs(err, ErrMalformedMembershipCRL)

	// A partitioned list carries a critical issuing distribution point, and a
	// delta a critical delta CRL indicator.
	for _, oid := range []asn1.ObjectIdentifier{{2, 5, 29, 28}, {2, 5, 29, 27}} {
		partial := intermediate.SignCRLPEM(t, &x509.RevocationList{
			Number:          big.NewInt(1),
			ThisUpdate:      time.Now().Add(-time.Minute),
			NextUpdate:      time.Now().Add(time.Hour),
			ExtraExtensions: []pkix.Extension{{Id: oid, Critical: true, Value: []byte{0x30, 0x00}}},
		})
		_, err := ParseMembershipCAWithCRL(root.CertPEM(), partial)
		require.ErrorIs(err, errCriticalMembershipCRLExtension, "extension %s", oid)
	}
}

// TestMembershipCARevocationFollowsSerials checks that a revocation names what
// an issuer signed, not the bytes that carry it. The node a revocation targets
// holds its own key, so it can always present a re-encoded twin under a new
// node ID.
func TestMembershipCARevocationFollowsSerials(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)
	rootCA, err := ParseMembershipCA(root.CertPEM())
	require.NoError(err)

	leafTwin := []*x509.Certificate{reencoded(t, chain[0]), chain[1]}
	require.True(verifies(rootCA, leafTwin))
	ca, err := ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, intermediate, 1, chain[0]))
	require.NoError(err)
	require.False(verifies(ca, leafTwin))

	intermediateTwin := []*x509.Certificate{chain[0], reencoded(t, chain[1])}
	require.True(verifies(rootCA, intermediateTwin))
	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, root, 1, intermediate.Certificate()))
	require.NoError(err)
	require.False(verifies(ca, intermediateTwin))
}

// TestConfigLoadMembershipCRL checks that both CRL keys reach the CA.
func TestConfigLoadMembershipCRL(t *testing.T) {
	require := require.New(t)

	root, intermediate := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, intermediate, "rpc-01", testLeafValidity)
	revocations := crlPEM(t, intermediate, 1, chain[0])

	path := filepath.Join(t.TempDir(), "membership-crl.pem")
	require.NoError(os.WriteFile(path, revocations, 0o600))
	fromPath := Config{
		MembershipCA:      []string{string(root.CertPEM())},
		MembershipCRLPath: path,
	}
	ca, err := fromPath.LoadMembershipCA()
	require.NoError(err)
	require.False(verifies(ca, chain))

	inline := Config{
		MembershipCA:  []string{string(root.CertPEM())},
		MembershipCRL: []string{string(revocations)},
	}
	ca, err = inline.LoadMembershipCA()
	require.NoError(err)
	require.False(verifies(ca, chain))
}
