// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto/x509"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/units"
)

const MaxCertificateLen = 16 * units.KiB

var ErrCertificateTooLarge = fmt.Errorf("staking: certificate length is greater than %d", MaxCertificateLen)

// ParseCertificate parses a single certificate from the given ASN.1 DER data.
func ParseCertificate(der []byte) (*Certificate, error) {
	if len(der) > MaxCertificateLen {
		return nil, ErrCertificateTooLarge
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	// Verifying that the signature algorithm is in the expected set isn't
	// technically required here. However, it ensure that avalanchego will only
	// connect to peers with certificates that it considers valid.
	_, ok := signatureAlgorithmVerificationDetails[cert.SignatureAlgorithm]
	if !ok {
		return nil, ErrUnsupportedAlgorithm
	}

	return CertificateFromX509(cert), nil
}
