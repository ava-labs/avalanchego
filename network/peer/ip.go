// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/signer"
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
func (ip *UnsignedIP) Sign(signer signer.Signer) (*SignedIP, error) {
	tlsSig, err := signer.SignTLS(ip.bytes())
	if err != nil {
		return nil, err
	}

	blsSig := signer.SignBLS(ip.bytes())

	return &SignedIP{
		UnsignedIP:   *ip,
		TLSSignature: tlsSig,
		BLSSignature: blsSig,
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
	TLSSignature []byte
	BLSSignature []byte
}

func (ip *SignedIP) Verify(verifier signer.Verifier) error {
	errs := wrappers.Errs{}

	errs.Add(
		verifier.VerifyTLS(ip.UnsignedIP.bytes(), ip.TLSSignature),
		verifier.VerifyBLS(ip.UnsignedIP.bytes(), ip.BLSSignature),
	)

	return errs.Err
}
