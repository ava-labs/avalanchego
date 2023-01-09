// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
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
func (ip *UnsignedIP) Sign(signer IPSigner) (*SignedIP, error) {
	signature := Signature{}
	sig, err := signer.Sign(ip.bytes(), signature)

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

type Signature struct {
	TLSSignature []byte
	BLSSignature []byte
}

// SignedIP is a wrapper of an UnsignedIP with the signature from a signer.
type SignedIP struct {
	UnsignedIP
	Signature
}

func (ip *SignedIP) Verify(verifier IPVerifier) error {
	return verifier.Verify(ip.UnsignedIP.bytes(), ip.Signature)
}

func (ip *SignedIP) Sign(signer IPSigner) (*SignedIP, error) {
	sig, err := signer.Sign(ip.UnsignedIP.bytes(), ip.Signature)
	if err != nil {
		return nil, err
	}

	ip.Signature = sig

	return ip, err
}
