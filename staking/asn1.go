// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/x509"
	"fmt"

	// Explicitly import for the crypto.RegisterHash init side-effects.
	//
	// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L30-L34
	_ "crypto/sha256"
)

// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L326-L350
var signatureAlgorithmVerificationDetails = map[x509.SignatureAlgorithm]x509.PublicKeyAlgorithm{
	x509.SHA256WithRSA:   x509.RSA,
	x509.ECDSAWithSHA256: x509.ECDSA,
}

func init() {
	if !crypto.SHA256.Available() {
		panic(fmt.Sprintf("required hash %q is not available", crypto.SHA256))
	}
}
