// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
)

// MaxRSAKeyBitLen is the maximum RSA key size in bits that we are willing to
// parse.
const MaxRSAKeyBitLen = 8192

var (
	ErrUnsupportedAlgorithm       = errors.New("staking: cannot verify signature: unsupported algorithm")
	ErrPublicKeyAlgoMismatch      = errors.New("staking: signature algorithm specified different public key type")
	ErrInvalidPublicKey           = errors.New("invalid public key")
	ErrECDSAVerificationFailure   = errors.New("staking: ECDSA verification failure")
	ErrED25519VerificationFailure = errors.New("staking: Ed25519 verification failure")
)

// CheckSignature verifies that the signature is a valid signature over signed
// from the certificate.
//
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L793-L797
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L816-L879
func CheckSignature(cert *x509.Certificate, signed []byte, signature []byte) error {
	verificationDetails, ok := signatureAlgorithmVerificationDetails[cert.SignatureAlgorithm]
	if !ok {
		return ErrUnsupportedAlgorithm
	}

	if verificationDetails.hash != crypto.Hash(0) {
		h := verificationDetails.hash.New()
		_, err := h.Write(signed)
		if err != nil {
			return err
		}
		signed = h.Sum(nil)
	}

	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		if verificationDetails.pubKeyAlgo != x509.RSA {
			return signaturePublicKeyAlgoMismatchError(verificationDetails.pubKeyAlgo, pub)
		}
		if bitLen := pub.N.BitLen(); bitLen > MaxRSAKeyBitLen {
			return fmt.Errorf("%w: bitLen=%d > maxBitLen=%d", ErrInvalidPublicKey, bitLen, MaxRSAKeyBitLen)
		}
		return rsa.VerifyPKCS1v15(pub, verificationDetails.hash, signed, signature)
	case *ecdsa.PublicKey:
		if verificationDetails.pubKeyAlgo != x509.ECDSA {
			return signaturePublicKeyAlgoMismatchError(verificationDetails.pubKeyAlgo, pub)
		}
		if !ecdsa.VerifyASN1(pub, signed, signature) {
			return ErrECDSAVerificationFailure
		}
		return nil
	case ed25519.PublicKey:
		if verificationDetails.pubKeyAlgo != x509.Ed25519 {
			return signaturePublicKeyAlgoMismatchError(verificationDetails.pubKeyAlgo, pub)
		}
		if !ed25519.Verify(pub, signed, signature) {
			return ErrED25519VerificationFailure
		}
		return nil
	default:
		return ErrUnsupportedAlgorithm
	}
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L812-L814
func signaturePublicKeyAlgoMismatchError(expectedPubKeyAlgo x509.PublicKeyAlgorithm, pubKey any) error {
	return fmt.Errorf("%w: expected an %s public key, but have public key of type %T", ErrPublicKeyAlgoMismatch, expectedPubKeyAlgo, pubKey)
}
