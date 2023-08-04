// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import "crypto/x509"

type Certificate struct {
	Raw                []byte
	PublicKey          any
	SignatureAlgorithm x509.SignatureAlgorithm
}

// CertificateFromX509 converts an x509 certificate into a staking certificate.
//
// Invariant: The provided certificate must be a parseable into a staking
// certificate.
func CertificateFromX509(cert *x509.Certificate) *Certificate {
	return &Certificate{
		Raw:                cert.Raw,
		PublicKey:          cert.PublicKey,
		SignatureAlgorithm: cert.SignatureAlgorithm,
	}
}
