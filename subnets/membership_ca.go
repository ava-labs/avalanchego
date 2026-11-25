// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	// maxMembershipChainLen bounds the certificates a peer can make path
	// building consider. Even a chain of five can reach crypto/x509's 100
	// signature checks, which [isStakingKey] keeps cheap. Real chains are two or
	// three; the headroom is for a cross-signed root staged during a rotation.
	maxMembershipChainLen = 8
)

var (
	errNoMembershipCACertificates = errors.New("membership CA holds no certificates")
	errTruncatedPEM               = errors.New("holds a PEM block that begins and does not end")
)

// MembershipCA holds the roots a peer's staking certificate chain may verify
// against to be a member of the subnet that declares it, whether or not it
// validates that subnet. Several roots stage a root rotation.
type MembershipCA struct {
	roots *x509.CertPool

	// revoked maps an issuer's raw subject to the serials it revoked; crls
	// keeps the lists for reporting. Both are set by [ParseMembershipCAWithCRL]
	// only.
	revoked map[string]set.Set[string]
	crls    []*x509.RevocationList
}

// CRLs returns the revocation lists applied to the roots.
func (c *MembershipCA) CRLs() []*x509.RevocationList {
	return c.crls
}

// ParseMembershipCA builds a MembershipCA from PEM certificate blocks, skipping
// anything else as crypto/x509 does. The operator verifies the file before
// installing it, so only a file damaged since, or one without a certificate, is
// refused.
func ParseMembershipCA(pemBytes []byte) (*MembershipCA, error) {
	if err := checkPEMComplete(pemBytes); err != nil {
		return nil, fmt.Errorf("membership CA %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, errNoMembershipCACertificates
	}
	return &MembershipCA{roots: roots}, nil
}

// checkPEMComplete refuses a truncated file: encoding/pem silently skips a
// block cut short, which would drop the last root or list.
func checkPEMComplete(pemBytes []byte) error {
	if bytes.Count(pemBytes, []byte("-----BEGIN ")) != bytes.Count(pemBytes, []byte("-----END ")) {
		return errTruncatedPEM
	}
	return nil
}

// Certify reports whether [chain], leaf first, chains to a root through no
// revoked certificate, and when that membership expires: the latest expiry
// among the chains that verify.
func (c *MembershipCA) Certify(chain []*x509.Certificate) (time.Time, bool) {
	var (
		expiresAt time.Time
		verified  bool
	)
	for _, verifiedChain := range c.verifiedChains(chain) {
		if c.isRevoked(verifiedChain) {
			continue
		}
		verified = true
		if chainExpiresAt := earliestExpiry(verifiedChain); chainExpiresAt.After(expiresAt) {
			expiresAt = chainExpiresAt
		}
	}
	return expiresAt, verified
}

func earliestExpiry(chain []*x509.Certificate) time.Time {
	return slices.MinFunc(chain, func(a, b *x509.Certificate) int {
		return a.NotAfter.Compare(b.NotAfter)
	}).NotAfter
}

// Revokes reports whether [chain] verifies but only through revoked
// certificates, which tells an evicted node apart from one that never verified.
func (c *MembershipCA) Revokes(chain []*x509.Certificate) bool {
	verifiedChains := c.verifiedChains(chain)
	for _, verifiedChain := range verifiedChains {
		if !c.isRevoked(verifiedChain) {
			return false
		}
	}
	return len(verifiedChains) > 0
}

// isRevoked matches by issuer and serial, which the issuer signed, so a
// certificate cannot shed a revocation by being re-encoded.
func (c *MembershipCA) isRevoked(verifiedChain []*x509.Certificate) bool {
	for _, cert := range verifiedChain {
		serials := c.revoked[string(cert.RawIssuer)]
		if serials.Contains(cert.SerialNumber.String()) {
			return true
		}
	}
	return false
}

// verifiedChains returns the chains [chain] builds to the roots, ignoring
// revocation.
func (c *MembershipCA) verifiedChains(chain []*x509.Certificate) [][]*x509.Certificate {
	// Without roots, crypto/x509 would verify against the system's instead.
	if c.roots == nil || len(chain) == 0 || len(chain) > maxMembershipChainLen {
		return nil
	}
	for _, cert := range chain {
		if !isStakingKey(cert) {
			return nil
		}
	}

	opts := x509.VerifyOptions{
		Roots: c.roots,
		// Membership is not a hostname or role claim, so any EKU is acceptable.
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
		return nil
	}
	return verifiedChains
}

// isStakingKey accepts the keys a staking certificate may hold: ECDSA P-256 or
// well-formed RSA-2048/4096. The peer picks the keys path building checks
// signatures against, so a costlier key would multiply the cost of every check.
func isStakingKey(cert *x509.Certificate) bool {
	switch key := cert.PublicKey.(type) {
	case *ecdsa.PublicKey:
		return key.Curve == elliptic.P256()
	case *rsa.PublicKey:
		return staking.ValidateRSAPublicKeyIsWellFormed(key) == nil
	default:
		return false
	}
}
