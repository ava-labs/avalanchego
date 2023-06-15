// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/ava-labs/avalanchego/utils/crypto"
)

var (
	_ crypto.MultiSigner   = (*PreBanffSigner)(nil)
	_ crypto.MultiVerifier = (*PreBanffVerifier)(nil)
)

// PreBanffSigner is used for all ip signing up to the Banff fork.
type PreBanffSigner struct {
	tlsSigner crypto.TLSSigner
}

func NewPreBanffSigner(cert *tls.Certificate) (*PreBanffSigner, error) {
	tlsSigner, err := crypto.NewTLSSigner(cert)
	if err != nil {
		return nil, err
	}

	return &PreBanffSigner{
		tlsSigner: tlsSigner,
	}, nil
}

func (PreBanffSigner) SignBLS(_ []byte) []byte {
	return nil
}

func (b PreBanffSigner) SignTLS(msg []byte) ([]byte, error) {
	return b.tlsSigner.Sign(msg)
}

// PreBanffVerifier  is used for all ip verification up to the Banff fork.
type PreBanffVerifier struct {
	tlsVerifier crypto.TLSVerifier
}

func NewPreBanffVerifier(cert *x509.Certificate) *PreBanffVerifier {
	return &PreBanffVerifier{
		tlsVerifier: crypto.TLSVerifier{
			Cert: cert,
		},
	}
}

func (PreBanffVerifier) VerifyBLS(_, _ []byte) (bool, error) {
	return true, nil
}

func (b PreBanffVerifier) VerifyTLS(msg, sig []byte) error {
	return b.tlsVerifier.Verify(msg, sig)
}
