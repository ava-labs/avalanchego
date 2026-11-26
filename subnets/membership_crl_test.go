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
	// The node a revocation targets holds its own key, so it can always present
	// a re-encoded twin under a new node ID. A list must still catch it.
	leafTwin := []*x509.Certificate{reencoded(t, revokedChain[0]), revokedChain[1]}
	intermediateTwin := []*x509.Certificate{keptChain[0], reencoded(t, keptChain[1])}

	// An empty list revokes nothing, so the twins are valid chains.
	ca, err := ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, intermediate, 1))
	require.NoError(err)
	require.True(verifies(ca, revokedChain))
	require.True(verifies(ca, leafTwin))
	require.True(verifies(ca, intermediateTwin))

	// A list from the issuing intermediate revokes only what it names, by
	// serial.
	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, intermediate, 1, revokedChain[0]))
	require.NoError(err)
	require.Len(ca.CRLs(), 1)
	require.False(verifies(ca, revokedChain))
	require.False(verifies(ca, leafTwin))
	require.True(verifies(ca, keptChain))

	// A list from a root revokes an intermediate and everything under it.
	ca, err = ParseMembershipCAWithCRL(root.CertPEM(), crlPEM(t, root, 1, intermediate.Certificate()))
	require.NoError(err)
	require.False(verifies(ca, revokedChain))
	require.False(verifies(ca, intermediateTwin))

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
	require.ErrorIs(err, errNoMembershipCRLs)

	garbage := pem.EncodeToMemory(&pem.Block{Type: crlPEMType, Bytes: []byte("not a list")})
	_, err = ParseMembershipCAWithCRL(root.CertPEM(), garbage)
	require.ErrorIs(err, errMalformedMembershipCRL)

	// A partitioned list carries a critical issuing distribution point. Every
	// critical extension is refused alike, so one stands for the rest.
	partial := intermediate.SignCRLPEM(t, &x509.RevocationList{
		Number:          big.NewInt(1),
		ThisUpdate:      time.Now().Add(-time.Minute),
		NextUpdate:      time.Now().Add(time.Hour),
		ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 28}, Critical: true, Value: []byte{0x30, 0x00}}},
	})
	_, err = ParseMembershipCAWithCRL(root.CertPEM(), partial)
	require.ErrorIs(err, errCriticalMembershipCRLExtension)
}
