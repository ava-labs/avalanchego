// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"errors"

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
func (ip *UnsignedIP) Sign(signer crypto.Signer, sigHashFunc func([]byte) []byte, sigAlgo crypto.SignerOpts) (*SignedIP, error) {
	sig, err := signer.Sign(
		rand.Reader,
		sigHashFunc(ip.bytes()),
		sigAlgo,
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

var ErrSignatureAlgorithmNotSupported = errors.New("signature algorithm not supported")

func deriveHash(sigAlgo x509.SignatureAlgorithm) (func([]byte) []byte, crypto.SignerOpts, error) {
	switch sigAlgo {
	case x509.SHA256WithRSA, x509.ECDSAWithSHA256, x509.SHA256WithRSAPSS:
		return hashing.ComputeHash256, crypto.SHA256, nil
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384, x509.SHA384WithRSAPSS:
		return hashing.ComputeHash384, crypto.SHA384, nil
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512, x509.SHA512WithRSAPSS:
		return hashing.ComputeHash512, crypto.SHA512, nil
	default:
		return nil, nil, ErrSignatureAlgorithmNotSupported
	}
}
