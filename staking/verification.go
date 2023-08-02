// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
)

// MaxRSAKeyBitlen is the maximum RSA key size in bits that we are willing to
// parse.
const MaxRSAKeyBitlen = 8192

var (
	ErrInvalidPublicKeyType = errors.New("invalid public key type")
	ErrInvalidPublicKey     = errors.New("invalid public key")
)

func CheckSignature(cert *x509.Certificate, message []byte, signature []byte) error {
	if cert.PublicKeyAlgorithm == x509.RSA {
		pk, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: %T", ErrInvalidPublicKeyType, cert.PublicKey)
		}
		if bitlen := pk.N.BitLen(); bitlen > MaxRSAKeyBitlen {
			return fmt.Errorf("%w: bitlen=%d > maxBitlen=%d", ErrInvalidPublicKey, bitlen, MaxRSAKeyBitlen)
		}
	}

	return cert.CheckSignature(
		cert.SignatureAlgorithm,
		message,
		signature,
	)
}
