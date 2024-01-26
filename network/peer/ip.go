// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errTimestampTooFarInFuture = errors.New("timestamp too far in the future")
	errInvalidSignature        = errors.New("invalid signature")
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

// Returns nil if:
// * [ip.Timestamp] is not after [maxTimestamp].
// * [ip.Signature] is a valid signature over [ip.UnsignedIP] from [cert].
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
		ip.Signature,
	); err != nil {
		return fmt.Errorf("%w: %w", errInvalidSignature, err)
	}
	return nil
}
