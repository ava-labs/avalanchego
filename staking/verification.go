// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
)

// MaxRSAKeyBitLen is the maximum RSA key size in bits that we are willing to
// parse.
const MaxRSAKeyBitLen = 8192

var ErrInvalidPublicKey = errors.New("invalid public key")

func CheckSignature(cert *x509.Certificate, message []byte, signature []byte) error {
	if cert.PublicKeyAlgorithm == x509.RSA {
		pk, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: %T", ErrInvalidPublicKey, cert.PublicKey)
		}
		if bitLen := pk.N.BitLen(); bitLen > MaxRSAKeyBitLen {
			return fmt.Errorf("%w: bitLen=%d > maxBitLen=%d", ErrInvalidPublicKey, bitLen, MaxRSAKeyBitLen)
		}
	}

	return cert.CheckSignature(
		cert.SignatureAlgorithm,
		message,
		signature,
	)
}
