// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tls

import (
	stdcrypto "crypto"
	"crypto/rand"
	"crypto/tls"
	"errors"

	"github.com/ava-labs/avalanchego/utils/ips"
)

var (
	errInvalidTLSKey = errors.New("invalid TLS key")

	_ ips.Signer = (*Signer)(nil)
)

// Signer is signs messages with a TLS cert.
type Signer struct {
	privateKey stdcrypto.Signer
	opts       stdcrypto.SignerOpts
}

// NewSigner returns a new instance of Signer.
func NewSigner(cert *tls.Certificate, opts stdcrypto.SignerOpts) (*Signer, error) {
	privateKey, ok := cert.PrivateKey.(stdcrypto.Signer)
	if !ok {
		return nil, errInvalidTLSKey
	}

	return &Signer{
		privateKey: privateKey,
		opts:       opts,
	}, nil
}

func (s Signer) Sign(msg []byte) ([]byte, error) {
	return s.privateKey.Sign(rand.Reader, msg, s.opts)
}
