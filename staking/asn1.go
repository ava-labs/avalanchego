// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/x509"
	"fmt"

	// Explicitly import for the crypto.RegisterHash init side-effects.
	//
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L30-L34
	_ "crypto/sha256" // initializes SHA256
)

var (
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L326-L350
	signatureAlgorithmVerificationDetails = map[x509.SignatureAlgorithm]struct {
		pubKeyAlgo x509.PublicKeyAlgorithm
		hash       crypto.Hash
	}{
		x509.SHA256WithRSA:   {x509.RSA, crypto.SHA256},                           // RSA-SHA256
		x509.ECDSAWithSHA256: {x509.ECDSA, crypto.SHA256},                         // ECDSA-SHA256
		x509.PureEd25519:     {x509.Ed25519, crypto.Hash(0) /* no pre-hashing */}, // Ed25519
	}
)

func init() {
	if !crypto.SHA256.Available() {
		panic(fmt.Sprintf("required hash %q is not available", crypto.SHA256))
	}
}
