// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package stakingtest builds the certificate trees that membership-CA tests need.
package stakingtest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/stretchr/testify/require"
)

// CA issues staking certificates that chain to a root a subnet trusts, so that
// a test can build a node with no on-chain identity - an RPC, archival or
// stateful node - and check that its peers accept it as a member.
//
// The signing key lives only in memory and cannot be exported or reloaded, so
// nothing this mints outlives the process. A deployment builds its membership CA
// with its own PKI and points membershipCAPath at the root.
type CA struct {
	cert *x509.Certificate
	key  crypto.Signer
}

// NewRootCA returns a self-signed root valid for [validity].
func NewRootCA(t require.TestingT, commonName string, validity time.Duration) *CA {
	return newCA(t, nil, caTemplate(commonName, 1, validity), newKey(t))
}

// NewIntermediateCA returns an issuing intermediate signed by this CA. It may
// sign leaves but no further CAs.
func (c *CA) NewIntermediateCA(t require.TestingT, commonName string, validity time.Duration) *CA {
	return c.NewIntermediateCAWithKey(t, commonName, validity, newKey(t))
}

// NewIntermediateCAWithKey is [CA.NewIntermediateCA] with a key the caller
// picks, for the cases that turn on the key's type.
func (c *CA) NewIntermediateCAWithKey(t require.TestingT, commonName string, validity time.Duration, key crypto.Signer) *CA {
	return newCA(t, c, caTemplate(commonName, 0, validity), key)
}

// Certificate returns this CA's certificate.
func (c *CA) Certificate() *x509.Certificate {
	return c.cert
}

// CertPEM returns this CA's certificate in PEM form. The root's PEM is what a
// subnet config points at with membershipCAPath.
func (c *CA) CertPEM() []byte {
	return pemEncode(c.cert.Raw)
}

// IssueNodeCert returns a staking certificate and key for a node, in the PEM
// form AvalancheGo loads from --staking-tls-cert-file and
// --staking-tls-key-file.
//
// The certificate is the leaf followed by this CA's own certificate, so that a
// peer receives the chain it needs to verify against the root. The node ID is
// the hash of the leaf, so reissuing a certificate gives the node a new
// identity.
func (c *CA) IssueNodeCert(t require.TestingT, commonName string, validity time.Duration) (certPEM, keyPEM []byte) {
	leaf, key := c.issueNode(t, commonName, validity)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM = append(pemEncode(leaf.Raw), c.CertPEM()...)
	return certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// issueNode returns a node's leaf certificate, signed by this CA, and its key.
func (c *CA) issueNode(t require.TestingT, commonName string, validity time.Duration) (*x509.Certificate, *ecdsa.PrivateKey) {
	key := newKey(t)
	leaf := c.sign(t, &x509.Certificate{
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}, key.Public())
	return leaf, key
}

// CRLPEM returns revocation list [number], signed by this CA, that revokes
// [revoked], in the PEM form a subnet config takes as membershipCRL. A negative
// [validity] of less than an hour yields a list whose nextUpdate has already
// passed.
func (c *CA) CRLPEM(t require.TestingT, number int64, validity time.Duration, revoked ...*x509.Certificate) []byte {
	now := time.Now()
	template := &x509.RevocationList{
		Number:     big.NewInt(number),
		ThisUpdate: now.Add(-time.Hour),
		NextUpdate: now.Add(validity),
	}
	for _, cert := range revoked {
		template.RevokedCertificateEntries = append(template.RevokedCertificateEntries, x509.RevocationListEntry{
			SerialNumber:   cert.SerialNumber,
			RevocationTime: now,
		})
	}
	return c.SignCRLPEM(t, template)
}

// SignCRLPEM signs [template] with this CA and returns it in PEM form. It is
// for the lists [CA.CRLPEM] does not build, such as one with extensions.
func (c *CA) SignCRLPEM(t require.TestingT, template *x509.RevocationList) []byte {
	der, err := x509.CreateRevocationList(rand.Reader, template, c.cert, c.key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der})
}

// newCA signs [template] with [parent], or self-signs it when [parent] is nil.
func newCA(t require.TestingT, parent *CA, template *x509.Certificate, key crypto.Signer) *CA {
	if parent == nil {
		parent = &CA{cert: template, key: key}
	}
	return &CA{cert: parent.sign(t, template, key.Public()), key: key}
}

// sign returns [template] issued by this CA to the holder of [pub]. The
// template names no serial number, so crypto/x509 picks a random one.
func (c *CA) sign(t require.TestingT, template *x509.Certificate, pub crypto.PublicKey) *x509.Certificate {
	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, pub, c.key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func caTemplate(commonName string, maxPathLen int, validity time.Duration) *x509.Certificate {
	return &x509.Certificate{
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            maxPathLen,
		MaxPathLenZero:        maxPathLen == 0,
	}
}

func newKey(t require.TestingT) *ecdsa.PrivateKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

func pemEncode(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
