// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/tls"
	"crypto/x509"
)

var _ Signer = (*BanffSigner)(nil)

// BanffSigner is used for all signing from genesis -> banff.
type BanffSigner struct {
	tlsSigner TLSSigner
}

func NewBanffSigner(cert *tls.Certificate) (*BanffSigner, error) {
	tlsSigner, err := NewTLSSigner(cert)
	if err != nil {
		return nil, err
	}

	return &BanffSigner{
		tlsSigner: tlsSigner,
	}, nil
}

func (BanffSigner) SignBLS(_ []byte) []byte {
	return nil
}

func (b BanffSigner) SignTLS(msg []byte) ([]byte, error) {
	return b.tlsSigner.Sign(msg)
}

var _ Verifier = (*BanffVerifier)(nil)

// BanffVerifier is used for all verification <= Banff.
type BanffVerifier struct {
	tlsVerifier TLSVerifier
}

func NewBanffVerifier(cert *x509.Certificate) *BanffVerifier {
	return &BanffVerifier{
		tlsVerifier: TLSVerifier{
			Cert: cert,
		},
	}
}

func (BanffVerifier) VerifyBLS(_, _ []byte) error {
	return nil
}

func (b BanffVerifier) VerifyTLS(msg, sig []byte) error {
	return b.tlsVerifier.Verify(msg, sig)
}
