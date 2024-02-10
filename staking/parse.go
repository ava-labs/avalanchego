// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/cryptobyte"

	"github.com/ava-labs/avalanchego/utils/units"

	cryptobyte_asn1 "golang.org/x/crypto/cryptobyte/asn1"
)

const (
	MaxCertificateLen = 2 * units.KiB

	allowedRSASmallModulusLen     = 2048
	allowedRSALargeModulusLen     = 4096
	allowedRSAPublicExponentValue = 65537
)

var (
	ErrCertificateTooLarge                   = fmt.Errorf("staking: certificate length is greater than %d", MaxCertificateLen)
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
	ErrInvalidRSAPublicKey                   = errors.New("staking: invalid RSA public key")
	ErrInvalidRSAModulus                     = errors.New("staking: invalid RSA modulus")
	ErrInvalidRSAPublicExponent              = errors.New("staking: invalid RSA public exponent")
	ErrRSAModulusNotPositive                 = errors.New("staking: RSA modulus is not a positive number")
	ErrUnsupportedRSAModulusBitLen           = errors.New("staking: unsupported RSA modulus bitlen")
	ErrRSAModulusIsEven                      = errors.New("staking: RSA modulus is an even number")
	ErrUnsupportedRSAPublicExponent          = errors.New("staking: unsupported RSA public exponent")
	ErrFailedUnmarshallingEllipticCurvePoint = errors.New("staking: failed to unmarshal elliptic curve point")
	ErrUnknownPublicKeyAlgorithm             = errors.New("staking: unknown public key algorithm")
)

// ParseCertificate parses a single certificate from the given ASN.1 DER data.
//
// TODO: Remove after v1.11.x activates.
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
// This function does not validate that the certificate is valid to be used
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
	var pkAI asn1.ObjectIdentifier
	if !pkAISeq.ReadASN1ObjectIdentifier(&pkAI) {
		return nil, ErrMalformedOID
	}

	// Note: Unlike the x509 package, we require parsing the public key.

	var spk asn1.BitString
	if !input.ReadASN1BitString(&spk) {
		return nil, ErrMalformedSubjectPublicKey
	}
	publicKey, signatureAlgorithm, err := parsePublicKey(pkAI, spk)
	return &Certificate{
		Raw:                bytes,
		SignatureAlgorithm: signatureAlgorithm,
		PublicKey:          publicKey,
	}, err
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L215-L306
func parsePublicKey(oid asn1.ObjectIdentifier, publicKey asn1.BitString) (crypto.PublicKey, x509.SignatureAlgorithm, error) {
	der := cryptobyte.String(publicKey.RightAlign())
	switch {
	case oid.Equal(oidPublicKeyRSA):
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

		if bitLen := pub.N.BitLen(); bitLen != allowedRSALargeModulusLen && bitLen != allowedRSASmallModulusLen {
			return nil, 0, fmt.Errorf("%w: %d", ErrUnsupportedRSAModulusBitLen, bitLen)
		}
		if pub.N.Bit(0) == 0 {
			return nil, 0, ErrRSAModulusIsEven
		}
		if pub.E != allowedRSAPublicExponentValue {
			return nil, 0, fmt.Errorf("%w: %d", ErrUnsupportedRSAPublicExponent, pub.E)
		}
		return pub, x509.SHA256WithRSA, nil
	case oid.Equal(oidPublicKeyECDSA):
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
