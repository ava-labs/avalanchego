// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"bytes"
	"crypto"
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

// ParseCertificate parses a single certificate from the given ASN.1 DER data.
func ParseCertificate(der []byte) (*Certificate, error) {
	cert, err := parseCertificate(der)
	if err != nil {
		return nil, err
	}
	return CertificateFromX509(cert), nil
}

var (
	// RFC 3279, 2.3 Public Key Algorithms
	//
	//	pkcs-1 OBJECT IDENTIFIER ::== { iso(1) member-body(2) us(840)
	//		rsadsi(113549) pkcs(1) 1 }
	//
	// rsaEncryption OBJECT IDENTIFIER ::== { pkcs1-1 1 }
	oidPublicKeyRSA = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	// RFC 5480, 2.1.1 Unrestricted Algorithm Identifier and Parameters
	//
	//	id-ecPublicKey OBJECT IDENTIFIER ::= {
	//		iso(1) member-body(2) us(840) ansi-X9-62(10045) keyType(2) 1 }
	oidPublicKeyECDSA = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	// RFC 8410, Section 3
	//
	//	id-Ed25519   OBJECT IDENTIFIER ::= { 1 3 101 112 }
	oidPublicKeyEd25519 = asn1.ObjectIdentifier{1, 3, 101, 112}
)

// OIDs for signature algorithms
//
//	pkcs-1 OBJECT IDENTIFIER ::= {
//		iso(1) member-body(2) us(840) rsadsi(113549) pkcs(1) 1 }
//
// RFC 3279 2.2.1 RSA Signature Algorithms
//
//	sha-1WithRSAEncryption OBJECT IDENTIFIER ::= { pkcs-1 5 }
//
// RFC 3279 2.2.3 ECDSA Signature Algorithm
//
//	ecdsa-with-SHA1 OBJECT IDENTIFIER ::= {
//		iso(1) member-body(2) us(840) ansi-x962(10045)
//		signatures(4) ecdsa-with-SHA1(1)}
//
// RFC 4055 5 PKCS #1 Version 1.5
//
//	sha256WithRSAEncryption OBJECT IDENTIFIER ::= { pkcs-1 11 }
//
//	sha384WithRSAEncryption OBJECT IDENTIFIER ::= { pkcs-1 12 }
//
//	sha512WithRSAEncryption OBJECT IDENTIFIER ::= { pkcs-1 13 }
//
// RFC 5758 3.2 ECDSA Signature Algorithm
//
//	ecdsa-with-SHA256 OBJECT IDENTIFIER ::= { iso(1) member-body(2)
//		us(840) ansi-X9-62(10045) signatures(4) ecdsa-with-SHA2(3) 2 }
//
//	ecdsa-with-SHA384 OBJECT IDENTIFIER ::= { iso(1) member-body(2)
//		us(840) ansi-X9-62(10045) signatures(4) ecdsa-with-SHA2(3) 3 }
//
//	ecdsa-with-SHA512 OBJECT IDENTIFIER ::= { iso(1) member-body(2)
//		us(840) ansi-X9-62(10045) signatures(4) ecdsa-with-SHA2(3) 4 }
//
// RFC 8410 3 Curve25519 and Curve448 Algorithm Identifiers
//
//	id-Ed25519   OBJECT IDENTIFIER ::= { 1 3 101 112 }
var (
	oidSignatureSHA1WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidSignatureSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSignatureSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSignatureSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidSignatureRSAPSS          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidSignatureECDSAWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	oidSignatureECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidSignatureECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidSignatureECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidSignatureEd25519         = asn1.ObjectIdentifier{1, 3, 101, 112}

	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}

	oidMGF1 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}

	// oidISOSignatureSHA1WithRSA means the same as oidSignatureSHA1WithRSA
	// but it's specified by ISO. Microsoft's makecert.exe has been known
	// to produce certificates with this OID.
	oidISOSignatureSHA1WithRSA = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 29}
)

var signatureAlgorithmDetails = []struct {
	algo       x509.SignatureAlgorithm
	oid        asn1.ObjectIdentifier
	pubKeyAlgo x509.PublicKeyAlgorithm
	hash       crypto.Hash
}{
	{x509.SHA1WithRSA, oidSignatureSHA1WithRSA, x509.RSA, crypto.SHA1},                         // RSA-SHA1
	{x509.SHA1WithRSA, oidISOSignatureSHA1WithRSA, x509.RSA, crypto.SHA1},                      // RSA-SHA1
	{x509.SHA256WithRSA, oidSignatureSHA256WithRSA, x509.RSA, crypto.SHA256},                   // RSA-SHA256
	{x509.SHA384WithRSA, oidSignatureSHA384WithRSA, x509.RSA, crypto.SHA384},                   // RSA-SHA384
	{x509.SHA512WithRSA, oidSignatureSHA512WithRSA, x509.RSA, crypto.SHA512},                   // RSA-SHA512
	{x509.SHA256WithRSAPSS, oidSignatureRSAPSS, x509.RSA, crypto.SHA256},                       // RSAPSS-SHA256
	{x509.SHA384WithRSAPSS, oidSignatureRSAPSS, x509.RSA, crypto.SHA384},                       // RSAPSS-SHA384
	{x509.SHA512WithRSAPSS, oidSignatureRSAPSS, x509.RSA, crypto.SHA512},                       // RSAPSS-SHA512
	{x509.ECDSAWithSHA1, oidSignatureECDSAWithSHA1, x509.ECDSA, crypto.SHA1},                   // ECDSA-SHA1
	{x509.ECDSAWithSHA256, oidSignatureECDSAWithSHA256, x509.ECDSA, crypto.SHA256},             // ECDSA-SHA256
	{x509.ECDSAWithSHA384, oidSignatureECDSAWithSHA384, x509.ECDSA, crypto.SHA384},             // ECDSA-SHA384
	{x509.ECDSAWithSHA512, oidSignatureECDSAWithSHA512, x509.ECDSA, crypto.SHA512},             // ECDSA-SHA512
	{x509.PureEd25519, oidSignatureEd25519, x509.Ed25519, crypto.Hash(0) /* no pre-hashing */}, // Ed25519
}

// RFC 5480, 2.1.1.1. Named Curve
//
//	secp224r1 OBJECT IDENTIFIER ::= {
//	  iso(1) identified-organization(3) certicom(132) curve(0) 33 }
//
//	secp256r1 OBJECT IDENTIFIER ::= {
//	  iso(1) member-body(2) us(840) ansi-X9-62(10045) curves(3)
//	  prime(1) 7 }
//
//	secp384r1 OBJECT IDENTIFIER ::= {
//	  iso(1) identified-organization(3) certicom(132) curve(0) 34 }
//
//	secp521r1 OBJECT IDENTIFIER ::= {
//	  iso(1) identified-organization(3) certicom(132) curve(0) 35 }
//
// NB: secp256r1 is equivalent to prime256v1
var (
	oidNamedCurveP224 = asn1.ObjectIdentifier{1, 3, 132, 0, 33}
	oidNamedCurveP256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}
	oidNamedCurveP384 = asn1.ObjectIdentifier{1, 3, 132, 0, 34}
	oidNamedCurveP521 = asn1.ObjectIdentifier{1, 3, 132, 0, 35}
)

func parseCertificate(der []byte) (*x509.Certificate, error) {
	input := cryptobyte.String(der)
	// Read the SEQUENCE including length and tag bytes so that we can check if
	// there is an unexpected suffix.
	if !input.ReadASN1Element(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed certificate")
	}

	// If there is an unexpected suffix the certificate was not DER encoded.
	if len(der) != len(input) {
		return nil, errors.New("x509: trailing data")
	}

	// Consume the length and tag bytes.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		// Note: because the above call to ReadASN1Element succeeded, this
		// should never happen.
		return nil, errors.New("x509: malformed certificate")
	}

	// Read the "to be signed" certificate into input.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed tbs certificate")
	}
	if !input.SkipOptionalASN1(cryptobyte_asn1.Tag(0).Constructed().ContextSpecific()) {
		return nil, errors.New("x509: malformed version")
	}
	if !input.SkipASN1(cryptobyte_asn1.INTEGER) {
		return nil, errors.New("x509: malformed serial number")
	}

	// Read the signature algorithm identifier.
	var sigAISeq cryptobyte.String
	if !input.ReadASN1(&sigAISeq, cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed signature algorithm identifier")
	}
	sigAI, err := parseAI(sigAISeq)
	if err != nil {
		return nil, err
	}
	signatureAlgorithm := getSignatureAlgorithmFromAI(sigAI)

	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed issuer")
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed validity")
	}
	if !input.SkipASN1(cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed issuer")
	}

	// Read the "simple public key infrastructure" data into input.
	if !input.ReadASN1(&input, cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed spki")
	}

	// Read the public key algorithm identifier.
	//
	// Note: We can't generate this from the signature algorithm because of the
	// additional params field.
	var pkAISeq cryptobyte.String
	if !input.ReadASN1(&pkAISeq, cryptobyte_asn1.SEQUENCE) {
		return nil, errors.New("x509: malformed public key algorithm identifier")
	}
	pkAI, err := parseAI(pkAISeq)
	if err != nil {
		return nil, err
	}

	// TODO: Does it make sense for us to allow someone to specify an unknown
	// public key without erroring?

	var spk asn1.BitString
	if !input.ReadASN1BitString(&spk) {
		return nil, errors.New("x509: malformed subjectPublicKey")
	}
	publicKey, err := parsePublicKey(&publicKeyInfo{
		Algorithm: pkAI,
		PublicKey: spk,
	})
	return &x509.Certificate{
		Raw:                der,
		SignatureAlgorithm: signatureAlgorithm,
		PublicKey:          publicKey,
	}, err
}

func parseAI(der cryptobyte.String) (pkix.AlgorithmIdentifier, error) {
	ai := pkix.AlgorithmIdentifier{}
	if !der.ReadASN1ObjectIdentifier(&ai.Algorithm) {
		return ai, errors.New("x509: malformed OID")
	}
	if der.Empty() {
		return ai, nil
	}
	var params cryptobyte.String
	var tag cryptobyte_asn1.Tag
	if !der.ReadAnyASN1Element(&params, &tag) {
		return ai, errors.New("x509: malformed parameters")
	}
	ai.Parameters.Tag = int(tag)
	ai.Parameters.FullBytes = params
	return ai, nil
}

// pssParameters reflects the parameters in an AlgorithmIdentifier that
// specifies RSA PSS. See RFC 3447, Appendix A.2.3.
type pssParameters struct {
	// The following three fields are not marked as
	// optional because the default values specify SHA-1,
	// which is no longer suitable for use in signatures.
	Hash         pkix.AlgorithmIdentifier `asn1:"explicit,tag:0"`
	MGF          pkix.AlgorithmIdentifier `asn1:"explicit,tag:1"`
	SaltLength   int                      `asn1:"explicit,tag:2"`
	TrailerField int                      `asn1:"optional,explicit,tag:3,default:1"`
}

func getSignatureAlgorithmFromAI(ai pkix.AlgorithmIdentifier) x509.SignatureAlgorithm {
	if ai.Algorithm.Equal(oidSignatureEd25519) {
		// RFC 8410, Section 3
		// > For all of the OIDs, the parameters MUST be absent.
		if len(ai.Parameters.FullBytes) != 0 {
			return x509.UnknownSignatureAlgorithm
		}
	}

	if !ai.Algorithm.Equal(oidSignatureRSAPSS) {
		for _, details := range signatureAlgorithmDetails {
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

type publicKeyInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	PublicKey asn1.BitString
}

func parsePublicKey(keyData *publicKeyInfo) (any, error) {
	oid := keyData.Algorithm.Algorithm
	params := keyData.Algorithm.Parameters
	der := cryptobyte.String(keyData.PublicKey.RightAlign())
	switch {
	case oid.Equal(oidPublicKeyRSA):
		// RSA public keys must have a NULL in the parameters.
		// See RFC 3279, Section 2.3.1.
		if !bytes.Equal(params.FullBytes, asn1.NullBytes) {
			return nil, errors.New("x509: RSA key missing NULL parameters")
		}

		pub := &rsa.PublicKey{N: new(big.Int)}
		if !der.ReadASN1(&der, cryptobyte_asn1.SEQUENCE) {
			return nil, errors.New("x509: invalid RSA public key")
		}
		if !der.ReadASN1Integer(pub.N) {
			return nil, errors.New("x509: invalid RSA modulus")
		}
		if !der.ReadASN1Integer(&pub.E) {
			return nil, errors.New("x509: invalid RSA public exponent")
		}

		if pub.N.Sign() <= 0 {
			return nil, errors.New("x509: RSA modulus is not a positive number")
		}
		if pub.E <= 0 {
			return nil, errors.New("x509: RSA public exponent is not a positive number")
		}
		return pub, nil
	case oid.Equal(oidPublicKeyECDSA):
		paramsDer := cryptobyte.String(params.FullBytes)
		var namedCurveOID asn1.ObjectIdentifier
		if !paramsDer.ReadASN1ObjectIdentifier(&namedCurveOID) {
			return nil, errors.New("x509: invalid ECDSA parameters")
		}
		namedCurve := namedCurveFromOID(namedCurveOID)
		if namedCurve == nil {
			return nil, errors.New("x509: unsupported elliptic curve")
		}
		x, y := elliptic.Unmarshal(namedCurve, der)
		if x == nil {
			return nil, errors.New("x509: failed to unmarshal elliptic curve point")
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
			return nil, errors.New("x509: Ed25519 key encoded with illegal parameters")
		}
		if len(der) != ed25519.PublicKeySize {
			return nil, errors.New("x509: wrong Ed25519 public key size")
		}
		return ed25519.PublicKey(der), nil
	default:
		return nil, errors.New("x509: unknown public key algorithm")
	}
}

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
