// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"fmt"

	// Explicitly import these for their crypto.RegisterHash init side-effects.
	//
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L30-L34
	_ "crypto/sha1"   // initializes SHA1
	_ "crypto/sha256" // initializes SHA256
	_ "crypto/sha512" // initializes SHA384 and SHA512
)

var (
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L433-L452
	//
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

	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L248-L324
	//
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

	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L326-L350
	signatureAlgorithmParsingDetails = []struct {
		algo x509.SignatureAlgorithm
		oid  asn1.ObjectIdentifier
	}{
		{x509.SHA1WithRSA, oidSignatureSHA1WithRSA},         // RSA-SHA1
		{x509.SHA1WithRSA, oidISOSignatureSHA1WithRSA},      // RSA-SHA1
		{x509.SHA256WithRSA, oidSignatureSHA256WithRSA},     // RSA-SHA256
		{x509.SHA384WithRSA, oidSignatureSHA384WithRSA},     // RSA-SHA384
		{x509.SHA512WithRSA, oidSignatureSHA512WithRSA},     // RSA-SHA512
		{x509.SHA256WithRSAPSS, oidSignatureRSAPSS},         // RSAPSS-SHA256
		{x509.SHA384WithRSAPSS, oidSignatureRSAPSS},         // RSAPSS-SHA384
		{x509.SHA512WithRSAPSS, oidSignatureRSAPSS},         // RSAPSS-SHA512
		{x509.ECDSAWithSHA1, oidSignatureECDSAWithSHA1},     // ECDSA-SHA1
		{x509.ECDSAWithSHA256, oidSignatureECDSAWithSHA256}, // ECDSA-SHA256
		{x509.ECDSAWithSHA384, oidSignatureECDSAWithSHA384}, // ECDSA-SHA384
		{x509.ECDSAWithSHA512, oidSignatureECDSAWithSHA512}, // ECDSA-SHA512
		{x509.PureEd25519, oidSignatureEd25519},             // Ed25519
	}

	signatureAlgorithmVerificationDetails = map[x509.SignatureAlgorithm]struct {
		pubKeyAlgo x509.PublicKeyAlgorithm
		hash       crypto.Hash
	}{
		x509.SHA1WithRSA:      {x509.RSA, crypto.SHA1},                             // RSA-SHA1
		x509.SHA256WithRSA:    {x509.RSA, crypto.SHA256},                           // RSA-SHA256
		x509.SHA384WithRSA:    {x509.RSA, crypto.SHA384},                           // RSA-SHA384
		x509.SHA512WithRSA:    {x509.RSA, crypto.SHA512},                           // RSA-SHA512
		x509.SHA256WithRSAPSS: {x509.RSA, crypto.SHA256},                           // RSAPSS-SHA256
		x509.SHA384WithRSAPSS: {x509.RSA, crypto.SHA384},                           // RSAPSS-SHA384
		x509.SHA512WithRSAPSS: {x509.RSA, crypto.SHA512},                           // RSAPSS-SHA512
		x509.ECDSAWithSHA1:    {x509.ECDSA, crypto.SHA1},                           // ECDSA-SHA1
		x509.ECDSAWithSHA256:  {x509.ECDSA, crypto.SHA256},                         // ECDSA-SHA256
		x509.ECDSAWithSHA384:  {x509.ECDSA, crypto.SHA384},                         // ECDSA-SHA384
		x509.ECDSAWithSHA512:  {x509.ECDSA, crypto.SHA512},                         // ECDSA-SHA512
		x509.PureEd25519:      {x509.Ed25519, crypto.Hash(0) /* no pre-hashing */}, // Ed25519
	}

	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L468-L489
	//
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
	oidNamedCurveP224 = asn1.ObjectIdentifier{1, 3, 132, 0, 33}
	oidNamedCurveP256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}
	oidNamedCurveP384 = asn1.ObjectIdentifier{1, 3, 132, 0, 34}
	oidNamedCurveP521 = asn1.ObjectIdentifier{1, 3, 132, 0, 35}
)

func init() {
	for _, hash := range []crypto.Hash{
		crypto.SHA1,
		crypto.SHA256,
		crypto.SHA384,
		crypto.SHA512,
	} {
		if !hash.Available() {
			panic(fmt.Sprintf("required hash %q is not available", hash))
		}
	}
}
