// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/cryptobyte"

	cryptobyte_asn1 "golang.org/x/crypto/cryptobyte/asn1"
)

var (
	ErrMalformedCertificate                  = errors.New("staking: malformed certificate")
	ErrMalformedTBSCertificate               = errors.New("staking: malformed tbs certificate")
	ErrMalformedVersion                      = errors.New("staking: malformed version")
	ErrMalformedSerialNumber                 = errors.New("staking: malformed serial number")
	ErrMalformedSignatureAlgorithmIdentifier = errors.New("staking: malformed signature algorithm identifier")
	ErrMalformedIssuer                       = errors.New("staking: malformed issuer")
	ErrMalformedValidity                     = errors.New("staking: malformed validity")
	ErrMalformedSPKI                         = errors.New("staking: malformed spki")
	ErrMalformedPublicKeyAlgorithmIdentifier = errors.New("staking: malformed public key algorithm identifier")
	ErrMalformedSubjectPublicKey             = errors.New("staking: malformed subject public key")
	ErrMalformedOID                          = errors.New("staking: malformed oid")
	ErrMalformedParameters                   = errors.New("staking: malformed parameters")
	ErrRSAKeyMissingNULLParameters           = errors.New("staking: RSA key missing NULL parameters")
	ErrInvalidRSAModulus                     = errors.New("staking: invalid RSA modulus")
	ErrInvalidRSAPublicExponent              = errors.New("staking: invalid RSA public exponent")
	ErrRSAModulusNotPositive                 = errors.New("staking: RSA modulus is not a positive number")
	ErrRSAPublicExponentNotPositive          = errors.New("staking: RSA public exponent is not a positive number")
	ErrInvalidECDSAParameters                = errors.New("staking: invalid ECDSA parameters")
	ErrUnsupportedEllipticCurve              = errors.New("staking: unsupported elliptic curve")
	ErrFailedUnmarshallingEllipticCurvePoint = errors.New("staking: failed to unmarshal elliptic curve point")
	ErrUnexpectedED25519Parameters           = errors.New("staking: Ed25519 key encoded with illegal parameters")
	ErrWrongED25519PublicKeySize             = errors.New("staking: wrong Ed25519 public key size")
	ErrUnknownPublicKeyAlgorithm             = errors.New("staking: unknown public key algorithm")
)

// ParseCertificate parses a single certificate from the given ASN.1 DER data.
func ParseCertificate(der []byte) (*Certificate, error) {
	x509Cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	stakingCert := CertificateFromX509(x509Cert)
	return stakingCert, ValidateCertificate(stakingCert)
}

// ParseCertificatePermissive parses a single certificate from the given ASN.1.
//
// This function does not validate that the certificate is validate to be used
// against normal TLS implementations.
//
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L789-L968
func ParseCertificatePermissive(bytes []byte) (*Certificate, error) {
	if len(bytes) > MaxCertificateLen {
		return nil, ErrCertificateTooLarge
	}

	input := cryptobyte.String(bytes)
	// Consume the length and tag bytes.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedCertificate
	}

	// Read the "to be signed" certificate into input.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedTBSCertificate
	}
	if !input.SkipOptionalASN1(cryptobyte_asn1.Tag(0).Constructed().ContextSpecific()) {
		return nil, ErrMalformedVersion
	}
	if !input.SkipASN1(cryptobyte_asn1.INTEGER) {
		return nil, ErrMalformedSerialNumber
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedSignatureAlgorithmIdentifier
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedIssuer
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedValidity
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedIssuer
	}

	// Read the "subject public key info" into input.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedSPKI
	}

	// Read the public key algorithm identifier.
	var pkAISeq cryptobyte.String
	if !input.ReadASN1(&pkAISeq, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedPublicKeyAlgorithmIdentifier
	}
	pkAI, err := parseAI(pkAISeq)
	if err != nil {
		return nil, err
	}

	// Note: Unlike the x509 package, we require parsing the public key.

	var spk asn1.BitString
	if !input.ReadASN1BitString(&spk) {
		return nil, ErrMalformedSubjectPublicKey
	}
	publicKey, signatureAlgorithm, err := parsePublicKey(&publicKeyInfo{
		Algorithm: pkAI,
		PublicKey: spk,
	})
	return &Certificate{
		Raw:                bytes,
		SignatureAlgorithm: signatureAlgorithm,
		PublicKey:          publicKey,
	}, err
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L149-L165
func parseAI(der cryptobyte.String) (pkix.AlgorithmIdentifier, error) {
	ai := pkix.AlgorithmIdentifier{}
	if !der.ReadASN1ObjectIdentifier(&ai.Algorithm) {
		return ai, ErrMalformedOID
	}
	if der.Empty() {
		return ai, nil
	}

	var (
		params cryptobyte.String
		tag    cryptobyte_asn1.Tag
	)
	if !der.ReadAnyASN1Element(&params, &tag) {
		return ai, ErrMalformedParameters
	}
	ai.Parameters.Tag = int(tag)
	ai.Parameters.FullBytes = params
	return ai, nil
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L172-L176
type publicKeyInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	PublicKey asn1.BitString
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L215-L306
func parsePublicKey(keyData *publicKeyInfo) (any, x509.SignatureAlgorithm, error) {
	oid := keyData.Algorithm.Algorithm
	params := keyData.Algorithm.Parameters
	der := cryptobyte.String(keyData.PublicKey.RightAlign())
	switch {
	case oid.Equal(oidPublicKeyRSA):
		// RSA public keys must have a NULL in the parameters.
		// See RFC 3279, Section 2.3.1.
		if !bytes.Equal(params.FullBytes, asn1.NullBytes) {
			return nil, 0, ErrRSAKeyMissingNULLParameters
		}

		pub := &rsa.PublicKey{N: new(big.Int)}
		if !der.ReadASN1(&der, cryptobyte_asn1.SEQUENCE) {
			return nil, 0, ErrInvalidRSAPublicKey
		}
		if !der.ReadASN1Integer(pub.N) {
			return nil, 0, ErrInvalidRSAModulus
		}
		if !der.ReadASN1Integer(&pub.E) {
			return nil, 0, ErrInvalidRSAPublicExponent
		}

		if pub.N.Sign() <= 0 {
			return nil, 0, ErrRSAModulusNotPositive
		}
		// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/tls/handshake_client.go#L874-L877
		if bitLen := pub.N.BitLen(); bitLen > MaxRSAKeyBitLen {
			return nil, 0, fmt.Errorf("%w: bitLen=%d > maxBitLen=%d", ErrInvalidRSAPublicKey, bitLen, MaxRSAKeyBitLen)
		}
		if pub.E <= 0 {
			return nil, 0, ErrRSAPublicExponentNotPositive
		}
		return pub, x509.SHA256WithRSA, nil
	case oid.Equal(oidPublicKeyECDSA):
		paramsDer := cryptobyte.String(params.FullBytes)
		var namedCurveOID asn1.ObjectIdentifier
		if !paramsDer.ReadASN1ObjectIdentifier(&namedCurveOID) {
			return nil, 0, ErrInvalidECDSAParameters
		}
		// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L491-L503
		if !namedCurveOID.Equal(oidNamedCurveP256) {
			return nil, 0, ErrUnsupportedEllipticCurve
		}
		namedCurve := elliptic.P256()
		x, y := elliptic.Unmarshal(namedCurve, der)
		if x == nil {
			return nil, 0, ErrFailedUnmarshallingEllipticCurvePoint
		}
		return &ecdsa.PublicKey{
			Curve: namedCurve,
			X:     x,
			Y:     y,
		}, x509.ECDSAWithSHA256, nil
	default:
		return nil, 0, ErrUnknownPublicKeyAlgorithm
	}
}
