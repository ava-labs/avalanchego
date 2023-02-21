// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import "crypto/x509"

// TLSVerifier verifies a signature of an ip against  a TLS cert.
type TLSVerifier struct {
	Cert *x509.Certificate
}

func (t TLSVerifier) Verify(msg, sig []byte) error {
	return t.Cert.CheckSignature(t.Cert.SignatureAlgorithm, msg, sig)
}
