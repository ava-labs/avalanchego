// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errTimestampTooFarInFuture = errors.New("timestamp too far in the future")
	errInvalidTLSSignature     = errors.New("invalid TLS signature")
)

// UnsignedIP is used for a validator to claim an IP. The [Timestamp] is used to
// ensure that the most updated IP claim is tracked by peers for a given
// validator.
type UnsignedIP struct {
	AddrPort  netip.AddrPort
	Timestamp uint64
}

// Sign this IP with the provided signer and return the signed IP.
func (ip *UnsignedIP) Sign(tlsSigner crypto.Signer, blsSigner bls.Signer) (*SignedIP, error) {
	ipBytes := ip.bytes()
	tlsSignature, err := tlsSigner.Sign(
		rand.Reader,
		hashing.ComputeHash256(ipBytes),
		crypto.SHA256,
	)
	if err != nil {
		return nil, err
	}

	blsSignature, err := blsSigner.SignProofOfPossession(ipBytes)
	if err != nil {
		return nil, err
	}

	return &SignedIP{
		UnsignedIP:        *ip,
		TLSSignature:      tlsSignature,
		BLSSignature:      blsSignature,
		BLSSignatureBytes: bls.SignatureToBytes(blsSignature),
	}, nil
}

func (ip *UnsignedIP) bytes() []byte {
	p := wrappers.Packer{
		Bytes: make([]byte, net.IPv6len+wrappers.ShortLen+wrappers.LongLen),
	}
	addrBytes := ip.AddrPort.Addr().As16()
	p.PackFixedBytes(addrBytes[:])
	p.PackShort(ip.AddrPort.Port())
	p.PackLong(ip.Timestamp)
	return p.Bytes
}

// SignedIP is a wrapper of an UnsignedIP with the signature from a signer.
type SignedIP struct {
	UnsignedIP
	TLSSignature      []byte
	BLSSignature      *bls.Signature
	BLSSignatureBytes []byte
}

// Returns nil if:
// * [ip.Timestamp] is not after [maxTimestamp].
// * [ip.TLSSignature] is a valid signature over [ip.UnsignedIP] from [cert].
func (ip *SignedIP) Verify(
	cert *staking.Certificate,
	maxTimestamp time.Time,
) error {
	maxUnixTimestamp := uint64(maxTimestamp.Unix())
	if ip.Timestamp > maxUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d > maxTimestamp %d", errTimestampTooFarInFuture, ip.Timestamp, maxUnixTimestamp)
	}

	if err := staking.CheckSignature(
		cert,
		ip.UnsignedIP.bytes(),
		ip.TLSSignature,
	); err != nil {
		return fmt.Errorf("%w: %w", errInvalidTLSSignature, err)
	}
	return nil
}
