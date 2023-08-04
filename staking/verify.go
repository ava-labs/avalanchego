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

var ErrInvalidPublicKey = errors.New("invalid public key")

// CheckSignature verifies that the signature is a valid signature over signed
// from the certificate.
func CheckSignature(cert *Certificate, signed []byte, signature []byte) error {
	verificationDetails, ok := signatureAlgorithmVerificationDetails[cert.SignatureAlgorithm]
	if !ok {
		return x509.ErrUnsupportedAlgorithm
	}

	if verificationDetails.hash != crypto.Hash(0) {
		h := verificationDetails.hash.New()
		// TODO: should we handle this error?
		_, _ = h.Write(signed)
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
		if isRSAPSS(cert.SignatureAlgorithm) {
			return rsa.VerifyPSS(pub, verificationDetails.hash, signed, signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
		}
		return rsa.VerifyPKCS1v15(pub, verificationDetails.hash, signed, signature)
	case *ecdsa.PublicKey:
		if verificationDetails.pubKeyAlgo != x509.ECDSA {
			return signaturePublicKeyAlgoMismatchError(verificationDetails.pubKeyAlgo, pub)
		}
		if !ecdsa.VerifyASN1(pub, signed, signature) {
			return errors.New("x509: ECDSA verification failure")
		}
		return nil
	case ed25519.PublicKey:
		if verificationDetails.pubKeyAlgo != x509.Ed25519 {
			return signaturePublicKeyAlgoMismatchError(verificationDetails.pubKeyAlgo, pub)
		}
		if !ed25519.Verify(pub, signed, signature) {
			return errors.New("x509: Ed25519 verification failure")
		}
		return nil
	default:
		return x509.ErrUnsupportedAlgorithm
	}
}

func isRSAPSS(algo x509.SignatureAlgorithm) bool {
	switch algo {
	case x509.SHA256WithRSAPSS, x509.SHA384WithRSAPSS, x509.SHA512WithRSAPSS:
		return true
	default:
		return false
	}
}

func signaturePublicKeyAlgoMismatchError(expectedPubKeyAlgo x509.PublicKeyAlgorithm, pubKey any) error {
	return fmt.Errorf("x509: signature algorithm specifies an %s public key, but have public key of type %T", expectedPubKeyAlgo, pubKey)
}
