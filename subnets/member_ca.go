// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

const (
	certificatePEMType = "CERTIFICATE"

	// maxMemberChainLen bounds path building, not chain structure. Every
	// certificate sharing the issuer's subject is a candidate parent whose
	// signature is checked, so without a limit a peer can spend this node's CPU
	// by padding its chain with same-subject decoys, up to the 100 signature
	// checks crypto/x509 allows. A real chain is two or three certificates, so
	// this sits well above one: a deeper bundle - a cross-signed root staged
	// during a rotation, say - still verifies.
	maxMemberChainLen = 8
)

var (
	ErrNoMemberCACertificates = errors.New("member CA holds no certificates")
	ErrMalformedMemberCA      = errors.New("malformed member CA certificate")

	errUnexpectedPEMBlock = errors.New("unexpected PEM block type in member CA")
	errMemberCANotACA     = errors.New("member CA certificate is not a certificate authority")
)

// MemberCA holds the root certificates that a peer's staking certificate chain
// may verify against. A peer whose chain verifies is a member of the subnet
// that declares the CA, whether or not it validates that subnet.
//
// Holding more than one root is how a root rotation is staged: the new root is
// added everywhere, leaves are reissued over time, and the old root is dropped
// once nothing chains to it.
type MemberCA struct {
	roots *x509.CertPool
}

// ParseMemberCA builds a MemberCA from one or more concatenated PEM
// certificate blocks.
func ParseMemberCA(pemBytes []byte) (*MemberCA, error) {
	var (
		ca = &MemberCA{
			roots: x509.NewCertPool(),
		}
		numRoots int
		rest     = pemBytes
	)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != certificatePEMType {
			return nil, fmt.Errorf("%w: %q", errUnexpectedPEMBlock, block.Type)
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformedMemberCA, err)
		}
		// x509 verification would reject a non-CA parent during the handshake.
		// Rejecting it here turns a silently unreachable subnet into a startup
		// error naming the offending certificate.
		if !cert.IsCA {
			return nil, fmt.Errorf("%w: %q", errMemberCANotACA, cert.Subject)
		}

		ca.roots.AddCert(cert)
		numRoots++
	}

	if numRoots == 0 {
		return nil, ErrNoMemberCACertificates
	}
	return ca, nil
}

// VerifyUntil reports whether [chain] - a peer's certificate chain as
// crypto/tls reports it, leaf first - chains to one of the trusted roots, and
// when the membership it grants expires.
//
// The validity window of every certificate in the chain is checked, and there
// is no revocation check: a certificate is a membership until it expires. When
// several chains verify, membership lasts until the last of them expires.
//
// A nil MemberCA, or a chain longer than [maxMemberChainLen], verifies
// nothing - the former is what a subnet without a member CA wants.
func (c *MemberCA) VerifyUntil(chain []*x509.Certificate) (time.Time, bool) {
	if c == nil || len(chain) == 0 || len(chain) > maxMemberChainLen {
		return time.Time{}, false
	}

	opts := x509.VerifyOptions{
		Roots: c.roots,
		// Membership is not a hostname or a role claim, so any extended key
		// usage on the leaf is acceptable.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if len(chain) > 1 {
		opts.Intermediates = x509.NewCertPool()
		for _, cert := range chain[1:] {
			opts.Intermediates.AddCert(cert)
		}
	}

	verifiedChains, err := chain[0].Verify(opts)
	if err != nil {
		return time.Time{}, false
	}

	var expiresAt time.Time
	for _, verifiedChain := range verifiedChains {
		chainExpiresAt := verifiedChain[0].NotAfter
		for _, cert := range verifiedChain[1:] {
			if cert.NotAfter.Before(chainExpiresAt) {
				chainExpiresAt = cert.NotAfter
			}
		}
		if chainExpiresAt.After(expiresAt) {
			expiresAt = chainExpiresAt
		}
	}
	return expiresAt, true
}
