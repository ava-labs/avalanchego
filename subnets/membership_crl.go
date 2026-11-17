// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/set"
)

const crlPEMType = "X509 CRL"

var (
	ErrNoMembershipCRLs       = errors.New("membership CRL holds no revocation lists")
	ErrMalformedMembershipCRL = errors.New("malformed membership CRL")

	errCriticalMembershipCRLExtension = errors.New("list has a critical extension, such as a partition or delta marker")
)

// ParseMembershipCAWithCRL is [ParseMembershipCA], also refusing every chain
// through a certificate revoked by an "X509 CRL" block of [crlPEM]. Like the
// roots, the lists are operator-verified, so their signatures are not checked.
// nextUpdate is not enforced: refusing a stale list would re-admit everything
// on it.
func ParseMembershipCAWithCRL(caPEM, crlPEM []byte) (*MembershipCA, error) {
	ca, err := ParseMembershipCA(caPEM)
	if err != nil {
		return nil, err
	}
	if err := checkPEMComplete(crlPEM); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedMembershipCRL, err)
	}

	ca.revoked = make(map[string]set.Set[string])
	for block, rest := pem.Decode(crlPEM); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != crlPEMType {
			continue
		}
		list, err := parseRevocationList(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformedMembershipCRL, err)
		}
		issuer := string(list.RawIssuer)
		serials := ca.revoked[issuer]
		for _, entry := range list.RevokedCertificateEntries {
			serials.Add(entry.SerialNumber.String())
		}
		ca.revoked[issuer] = serials
		ca.crls = append(ca.crls, list)
	}
	if len(ca.crls) == 0 {
		return nil, ErrNoMembershipCRLs
	}
	return ca, nil
}

// parseRevocationList refuses a list with a critical extension: crypto/x509
// processes none, and the ones in use mark a list as only part of what its
// issuer revoked, so taking it as the whole would re-admit the rest.
func parseRevocationList(der []byte) (*x509.RevocationList, error) {
	list, err := x509.ParseRevocationList(der)
	if err != nil {
		return nil, err
	}
	for _, extension := range list.Extensions {
		if extension.Critical {
			return nil, fmt.Errorf("%w: %s", errCriticalMembershipCRLExtension, extension.Id)
		}
	}
	return list, nil
}
