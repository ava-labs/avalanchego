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

var (
	ErrUnsupportedAlgorithm       = errors.New("staking: cannot verify signature: unsupported algorithm")
	ErrPublicKeyAlgoMismatch      = errors.New("staking: signature algorithm specified different public key type")
	ErrECDSAVerificationFailure   = errors.New("staking: ECDSA verification failure")
	ErrED25519VerificationFailure = errors.New("staking: Ed25519 verification failure")
)

// CheckSignature verifies that the signature is a valid signature over signed
// from the certificate.
//
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L793-L797
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L816-L879
func CheckSignature(cert *Certificate, signed []byte, signature []byte) error {
	verificationDetails, ok := signatureAlgorithmVerificationDetails[cert.SignatureAlgorithm]
	if !ok {
		return ErrUnsupportedAlgorithm
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
		if isRSAPSS(cert.SignatureAlgorithm) {
			return rsa.VerifyPSS(pub, verificationDetails.hash, signed, signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
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

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L206-L213
func isRSAPSS(algo x509.SignatureAlgorithm) bool {
	switch algo {
	case x509.SHA256WithRSAPSS, x509.SHA384WithRSAPSS, x509.SHA512WithRSAPSS:
		return true
	default:
		return false
	}
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L812-L814
func signaturePublicKeyAlgoMismatchError(expectedPubKeyAlgo x509.PublicKeyAlgorithm, pubKey any) error {
	return fmt.Errorf("%w: expected an %s public key, but have public key of type %T", ErrPublicKeyAlgoMismatch, expectedPubKeyAlgo, pubKey)
}
