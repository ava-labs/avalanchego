package ecdsa

import (
	stdcrypto "crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"

	"github.com/ava-labs/avalanchego/utils/crypto"
)

var (
	_ crypto.Signer = (*Signer)(nil)
)

func NewSigner(curve elliptic.Curve, opts stdcrypto.SignerOpts) (Signer, error) {
	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return Signer{}, err
	}
	return Signer{
		privateKey: privateKey,
		opts:       opts,
	}, nil
}

type Signer struct {
	privateKey *ecdsa.PrivateKey
	opts       stdcrypto.SignerOpts
}

func (s Signer) Sign(msg []byte) ([]byte, error) {
	return s.privateKey.Sign(rand.Reader, msg, s.opts)
}
