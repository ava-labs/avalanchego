// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// UnsignedIP is used for a validator to claim an IP. The [Timestamp] is used to
// ensure that the most updated IP claim is tracked by peers for a given
// validator.
type UnsignedIP struct {
	ips.IPPort
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
		UnsignedIP: *ip,
		Signature:  sig,
	}, err
}

func (ip *UnsignedIP) bytes() []byte {
	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.IPLen+wrappers.LongLen),
	}
	ips.PackIP(&p, ip.IPPort)
	p.PackLong(ip.Timestamp)
	return p.Bytes
}

// SignedIP is a wrapper of an UnsignedIP with the signature from a signer.
type SignedIP struct {
	UnsignedIP
	Signature []byte
}

func (ip *SignedIP) Verify(cert *x509.Certificate) error {
	return cert.CheckSignature(
		cert.SignatureAlgorithm,
		ip.UnsignedIP.bytes(),
		ip.Signature,
	)
}
