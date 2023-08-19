// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import "crypto/x509"

// ParseCertificate parses a single certificate from the given ASN.1 DER data.
func ParseCertificate(der []byte) (*Certificate, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return CertificateFromX509(cert), nil
}
