// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

type Ciphersuite int

const (
	CiphersuiteSignature Ciphersuite = iota
	CiphersuiteProofOfPossession
)

var ciphersuiteStrings = [...]string{
	"BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_",
	"BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_",
}

var ciphersuiteBytes = [...][]byte{
	[]byte(ciphersuiteStrings[0]),
	[]byte(ciphersuiteStrings[1]),
}

func (c Ciphersuite) String() string {
	return ciphersuiteStrings[c]
}

func (c Ciphersuite) Bytes() []byte {
	return ciphersuiteBytes[c]
}
