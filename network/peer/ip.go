// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// UnsignedIP is used for a validator to claim an IP. The [Timestamp] is used to
// ensure that the most updated IP claim is tracked by peers for a given
// validator.
type UnsignedIP struct {
	IP        utils.IPDesc
	Timestamp uint64
}

// Sign this IP with the provided signer and return the signed IP.
func (ip *UnsignedIP) Sign(signer crypto.Signer) (*SignedIP, error) {
	sig, err := signer.Sign(
		rand.Reader,
		hashing.ComputeHash256(ip.bytes()),
		crypto.SHA256,
	)
	return &SignedIP{
		IP:        *ip,
		Signature: sig,
	}, err
}

func (ip *UnsignedIP) bytes() []byte {
	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.IPLen+wrappers.LongLen),
	}
	p.PackIP(ip.IP)
	p.PackLong(ip.Timestamp)
	return p.Bytes
}

// SignedIP is a wrapper of an UnsignedIP with the signature from a signer.
type SignedIP struct {
	IP        UnsignedIP
	Signature []byte
}

func (ip *SignedIP) Verify(cert *x509.Certificate) error {
	return cert.CheckSignature(
		cert.SignatureAlgorithm,
		ip.IP.bytes(),
		ip.Signature,
	)
}
