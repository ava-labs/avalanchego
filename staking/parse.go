// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
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

// MaxRSAKeyBitLen is the maximum RSA key size in bits that we are willing to
// parse.
//
// https://github.com/golang/go/blob/go1.19.12/src/crypto/tls/handshake_client.go#L860-L862
const MaxRSAKeyBitLen = 8192

var (
	ErrMalformedCertificate                  = errors.New("staking: malformed certificate")
	ErrTrailingData                          = errors.New("staking: trailing data")
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
	ErrInvalidRSAPublicKey                   = errors.New("staking: invalid RSA public key")
	ErrInvalidRSAModulus                     = errors.New("staking: invalid RSA modulus")
	ErrInvalidRSAPublicExponent              = errors.New("staking: invalid RSA public exponent")
	ErrRSAModulusNotPositive                 = errors.New("staking: RSA modulus is not a positive number")
	ErrInvalidRSAModulusBitLen               = fmt.Errorf("staking: RSA modulus bitLen is greater than %d", MaxRSAKeyBitLen)
	ErrRSAPublicExponentNotPositive          = errors.New("staking: RSA public exponent is not a positive number")
	ErrInvalidECDSAParameters                = errors.New("staking: invalid ECDSA parameters")
	ErrUnsupportedEllipticCurve              = errors.New("staking: unsupported elliptic curve")
	ErrFailedUnmarshallingEllipticCurvePoint = errors.New("staking: failed to unmarshal elliptic curve point")
	ErrUnexpectedED25519Parameters           = errors.New("staking: Ed25519 key encoded with illegal parameters")
	ErrWrongED25519PublicKeySize             = errors.New("staking: wrong Ed25519 public key size")
	ErrUnknownPublicKeyAlgorithm             = errors.New("staking: unknown public key algorithm")
)

// ParseCertificate parses a single certificate from the given ASN.1 DER data.
//
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L789-L968
func ParseCertificate(der []byte) (*Certificate, error) {
	input := cryptobyte.String(der)
	// Read the SEQUENCE including length and tag bytes so that we can check if
	// there is an unexpected suffix.
	if !input.ReadASN1Element(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedCertificate
	}

	// If there is an unexpected suffix the certificate was not DER encoded.
	//
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L976-L978
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L799
	if len(der) != len(input) {
		return nil, ErrTrailingData
	}

	// Consume the length and tag bytes.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		// Note: because the above call to ReadASN1Element succeeded, this
		// should never fail.
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

	// Read the signature algorithm identifier.
	var sigAISeq cryptobyte.String
	if !input.ReadASN1(&sigAISeq, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedSignatureAlgorithmIdentifier
	}
	sigAI, err := parseAI(sigAISeq)
	if err != nil {
		return nil, err
	}
	signatureAlgorithm := getSignatureAlgorithmFromAI(sigAI)

	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedIssuer
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedValidity
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedIssuer
	}

	// Read the "simple public key infrastructure" data into input.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, ErrMalformedSPKI
	}

	// Read the public key algorithm identifier.
	//
	// Note: We can't generate this from the signature algorithm because of the
	// additional params field.
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
	publicKey, err := parsePublicKey(&publicKeyInfo{
		Algorithm: pkAI,
		PublicKey: spk,
	})
	return &Certificate{
		Raw:                der,
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

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L377-L431
func getSignatureAlgorithmFromAI(ai pkix.AlgorithmIdentifier) x509.SignatureAlgorithm {
	if ai.Algorithm.Equal(oidSignatureEd25519) {
		// RFC 8410, Section 3
		// > For all of the OIDs, the parameters MUST be absent.
		if len(ai.Parameters.FullBytes) != 0 {
			return x509.UnknownSignatureAlgorithm
		}
	}

	for _, details := range signatureAlgorithmParsingDetails {
		if ai.Algorithm.Equal(details.oid) {
			return details.algo
		}
	}
	return x509.UnknownSignatureAlgorithm
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L172-L176
type publicKeyInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	PublicKey asn1.BitString
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/parser.go#L215-L306
func parsePublicKey(keyData *publicKeyInfo) (any, error) {
	oid := keyData.Algorithm.Algorithm
	params := keyData.Algorithm.Parameters
	der := cryptobyte.String(keyData.PublicKey.RightAlign())
	switch {
	case oid.Equal(oidPublicKeyRSA):
		// RSA public keys must have a NULL in the parameters.
		// See RFC 3279, Section 2.3.1.
		if !bytes.Equal(params.FullBytes, asn1.NullBytes) {
			return nil, ErrRSAKeyMissingNULLParameters
		}

		pub := &rsa.PublicKey{N: new(big.Int)}
		if !der.ReadASN1(&der, cryptobyte_asn1.SEQUENCE) {
			return nil, ErrInvalidRSAPublicKey
		}
		if !der.ReadASN1Integer(pub.N) {
			return nil, ErrInvalidRSAModulus
		}
		if !der.ReadASN1Integer(&pub.E) {
			return nil, ErrInvalidRSAPublicExponent
		}

		if pub.N.Sign() <= 0 {
			return nil, ErrRSAModulusNotPositive
		}
		// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/tls/handshake_client.go#L874-L877
		if pub.N.BitLen() > MaxRSAKeyBitLen {
			return nil, ErrInvalidRSAModulusBitLen
		}
		if pub.E <= 0 {
			return nil, ErrRSAPublicExponentNotPositive
		}
		return pub, nil
	case oid.Equal(oidPublicKeyECDSA):
		paramsDer := cryptobyte.String(params.FullBytes)
		var namedCurveOID asn1.ObjectIdentifier
		if !paramsDer.ReadASN1ObjectIdentifier(&namedCurveOID) {
			return nil, ErrInvalidECDSAParameters
		}
		namedCurve := namedCurveFromOID(namedCurveOID)
		if namedCurve == nil {
			return nil, ErrUnsupportedEllipticCurve
		}
		x, y := elliptic.Unmarshal(namedCurve, der)
		if x == nil {
			return nil, ErrFailedUnmarshallingEllipticCurvePoint
		}
		return &ecdsa.PublicKey{
			Curve: namedCurve,
			X:     x,
			Y:     y,
		}, nil

	case oid.Equal(oidPublicKeyEd25519):
		// RFC 8410, Section 3
		// > For all of the OIDs, the parameters MUST be absent.
		if len(params.FullBytes) != 0 {
			return nil, ErrUnexpectedED25519Parameters
		}
		if len(der) != ed25519.PublicKeySize {
			return nil, ErrWrongED25519PublicKeySize
		}
		return ed25519.PublicKey(der), nil
	default:
		return nil, ErrUnknownPublicKeyAlgorithm
	}
}

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L491-L503
func namedCurveFromOID(oid asn1.ObjectIdentifier) elliptic.Curve {
	switch {
	case oid.Equal(oidNamedCurveP224):
		return elliptic.P224()
	case oid.Equal(oidNamedCurveP256):
		return elliptic.P256()
	case oid.Equal(oidNamedCurveP384):
		return elliptic.P384()
	case oid.Equal(oidNamedCurveP521):
		return elliptic.P521()
	default:
		return nil
	}
}
