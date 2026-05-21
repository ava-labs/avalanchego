// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"encoding/asn1"
	"fmt"

	// Explicitly import for the crypto.RegisterHash init side-effects.
	//
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L30-L34
	_ "crypto/sha256"
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
)

func init() {
	if !crypto.SHA256.Available() {
		panic(fmt.Sprintf("required hash %q is not available", crypto.SHA256))
	}
}
