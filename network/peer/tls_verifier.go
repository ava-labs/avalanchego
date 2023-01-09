// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import "crypto/x509"

var _ IPVerifier = (*TLSVerifier)(nil)

type TLSVerifier struct {
	Cert *x509.Certificate
}

func (x TLSVerifier) Verify(ipBytes []byte, sig Signature) error {
	return x.Cert.CheckSignature(x.Cert.SignatureAlgorithm, ipBytes,
		sig.TLSSignature)
}
