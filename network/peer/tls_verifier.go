// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import "crypto/x509"

var _ IPVerifier = (*TLSVerifier)(nil)

// TLSVerifier verifies a signature of an ip against  a TLS cert.
type TLSVerifier struct {
	Cert *x509.Certificate
}

func (t TLSVerifier) Verify(ipBytes []byte, sig Signature) error {
	if len(sig.TLSSignature) == 0 {
		return errMissingSignature
	}

	return t.Cert.CheckSignature(t.Cert.SignatureAlgorithm, ipBytes,
		sig.TLSSignature)
}
