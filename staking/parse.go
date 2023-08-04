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
	"math/big"

	"golang.org/x/crypto/cryptobyte"

	cryptobyte_asn1 "golang.org/x/crypto/cryptobyte/asn1"
)

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

	// TODO: Does it make sense for us to allow someone to specify an unknown
	// public key without erroring?

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

// pssParameters reflects the parameters in an AlgorithmIdentifier that
// specifies RSA PSS. See RFC 3447, Appendix A.2.3.
//
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L365-L375
type pssParameters struct {
	// The following three fields are not marked as
	// optional because the default values specify SHA-1,
	// which is no longer suitable for use in signatures.
	Hash         pkix.AlgorithmIdentifier `asn1:"explicit,tag:0"`
	MGF          pkix.AlgorithmIdentifier `asn1:"explicit,tag:1"`
	SaltLength   int                      `asn1:"explicit,tag:2"`
	TrailerField int                      `asn1:"optional,explicit,tag:3,default:1"`
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

	if !ai.Algorithm.Equal(oidSignatureRSAPSS) {
		for _, details := range signatureAlgorithmParsingDetails {
			if ai.Algorithm.Equal(details.oid) {
				return details.algo
			}
		}
		return x509.UnknownSignatureAlgorithm
	}

	// RSA PSS is special because it encodes important parameters
	// in the Parameters.

	var params pssParameters
	if _, err := asn1.Unmarshal(ai.Parameters.FullBytes, &params); err != nil {
		return x509.UnknownSignatureAlgorithm
	}

	var mgf1HashFunc pkix.AlgorithmIdentifier
	if _, err := asn1.Unmarshal(params.MGF.Parameters.FullBytes, &mgf1HashFunc); err != nil {
		return x509.UnknownSignatureAlgorithm
	}

	// PSS is greatly overburdened with options. This code forces them into
	// three buckets by requiring that the MGF1 hash function always match the
	// message hash function (as recommended in RFC 3447, Section 8.1), that the
	// salt length matches the hash length, and that the trailer field has the
	// default value.
	if (len(params.Hash.Parameters.FullBytes) != 0 && !bytes.Equal(params.Hash.Parameters.FullBytes, asn1.NullBytes)) ||
		!params.MGF.Algorithm.Equal(oidMGF1) ||
		!mgf1HashFunc.Algorithm.Equal(params.Hash.Algorithm) ||
		(len(mgf1HashFunc.Parameters.FullBytes) != 0 && !bytes.Equal(mgf1HashFunc.Parameters.FullBytes, asn1.NullBytes)) ||
		params.TrailerField != 1 {
		return x509.UnknownSignatureAlgorithm
	}

	switch {
	case params.Hash.Algorithm.Equal(oidSHA256) && params.SaltLength == 32:
		return x509.SHA256WithRSAPSS
	case params.Hash.Algorithm.Equal(oidSHA384) && params.SaltLength == 48:
		return x509.SHA384WithRSAPSS
	case params.Hash.Algorithm.Equal(oidSHA512) && params.SaltLength == 64:
		return x509.SHA512WithRSAPSS
	default:
		return x509.UnknownSignatureAlgorithm
	}
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

		// TODO: Since we don't allow verification of DSA signatures, we don't
		// need to be able to parse DSA public keys right?

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
