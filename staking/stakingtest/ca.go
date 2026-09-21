// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package stakingtest builds the certificate trees that member-CA tests need.
package stakingtest

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/staking"
)

// CA issues staking certificates that chain to a root a subnet trusts, so that
// a test can build a node with no on-chain identity - an RPC, archival or
// stateful node - and check that its peers accept it as a member.
//
// The signing key lives only in memory and cannot be exported or reloaded, so
// nothing this mints outlives the process. A deployment builds its member CA
// with its own PKI and points memberCAPath at the root.
type CA struct {
	cert *x509.Certificate
	key  crypto.Signer
}

// NewRootCA returns a self-signed root valid for [validity].
func NewRootCA(commonName string, validity time.Duration) (*CA, error) {
	template, err := caTemplate(commonName, 1, validity)
	if err != nil {
		return nil, err
	}
	return newCA(nil, template)
}

// NewIntermediateCA returns an issuing intermediate signed by [parent]. It may
// sign leaves but no further CAs.
func (c *CA) NewIntermediateCA(commonName string, validity time.Duration) (*CA, error) {
	template, err := caTemplate(commonName, 0, validity)
	if err != nil {
		return nil, err
	}
	return newCA(c, template)
}

// Certificate returns this CA's certificate.
func (c *CA) Certificate() *x509.Certificate {
	return c.cert
}

// CertPEM returns this CA's certificate in PEM form. The root's PEM is what a
// subnet config points at with memberCAPath.
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
func (c *CA) IssueNodeCert(commonName string, validity time.Duration) (certPEM, keyPEM []byte, err error) {
	serialNumber, err := newSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("stakingtest: generating node key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, key.Public(), c.key)
	if err != nil {
		return nil, nil, fmt.Errorf("stakingtest: signing node certificate: %w", err)
	}

	// AvalancheGo refuses leaf certificates above MaxCertificateLen, and a node
	// that cannot parse its own certificate cannot start.
	if _, err := staking.ParseCertificate(der); err != nil {
		return nil, nil, fmt.Errorf("stakingtest: issued an unusable node certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("stakingtest: marshalling node key: %w", err)
	}

	var bundle bytes.Buffer
	bundle.Write(pemEncode(der))
	bundle.Write(c.CertPEM())
	return bundle.Bytes(), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

// newCA signs [template] with [parent], or self-signs it when [parent] is nil.
func newCA(parent *CA, template *x509.Certificate) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("stakingtest: generating CA key: %w", err)
	}

	var (
		issuer           = template
		signer           = crypto.Signer(key)
		signerCommonName = template.Subject.CommonName
	)
	if parent != nil {
		issuer = parent.cert
		signer = parent.key
		signerCommonName = parent.cert.Subject.CommonName
	}

	der, err := x509.CreateCertificate(rand.Reader, template, issuer, key.Public(), signer)
	if err != nil {
		return nil, fmt.Errorf("stakingtest: signing %q with %q: %w", template.Subject.CommonName, signerCommonName, err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("stakingtest: parsing issued CA certificate: %w", err)
	}
	return &CA{cert: cert, key: key}, nil
}

func caTemplate(commonName string, maxPathLen int, validity time.Duration) (*x509.Certificate, error) {
	serialNumber, err := newSerialNumber()
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            maxPathLen,
		MaxPathLenZero:        maxPathLen == 0,
	}, nil
}

func newSerialNumber() (*big.Int, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("stakingtest: generating serial number: %w", err)
	}
	return serialNumber, nil
}

func pemEncode(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
