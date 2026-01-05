// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"net/netip"
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// IPSigner will return a signedIP for the current value of our dynamic IP.
type IPSigner struct {
	ip        *utils.Atomic[netip.AddrPort]
	clock     mockable.Clock
	tlsSigner crypto.Signer
	blsSigner bls.Signer

	// Must be held while accessing [signedIP]
	signedIPLock sync.RWMutex
	// Note that the values in [*signedIP] are constants and can be inspected
	// without holding [signedIPLock].
	signedIP *SignedIP
}

func NewIPSigner(
	ip *utils.Atomic[netip.AddrPort],
	tlsSigner crypto.Signer,
	blsSigner bls.Signer,
) *IPSigner {
	return &IPSigner{
		ip:        ip,
		tlsSigner: tlsSigner,
		blsSigner: blsSigner,
	}
}

// GetSignedIP returns the signedIP of the current value of the provided
// dynamicIP. If the dynamicIP hasn't changed since the prior call to
// GetSignedIP, then the same [SignedIP] will be returned.
//
// It's safe for multiple goroutines to concurrently call GetSignedIP.
func (s *IPSigner) GetSignedIP() (*SignedIP, error) {
	// Optimistically, the IP should already be signed. By grabbing a read lock
	// here we enable full concurrency of new connections.
	s.signedIPLock.RLock()
	signedIP := s.signedIP
	s.signedIPLock.RUnlock()
	ip := s.ip.Get()
	if signedIP != nil && signedIP.AddrPort == ip {
		return signedIP, nil
	}

	// If our current IP hasn't been signed yet - then we should sign it.
	s.signedIPLock.Lock()
	defer s.signedIPLock.Unlock()

	// It's possible that multiple threads read [n.signedIP] as incorrect at the
	// same time, we should verify that we are the first thread to attempt to
	// update it.
	signedIP = s.signedIP
	if signedIP != nil && signedIP.AddrPort == ip {
		return signedIP, nil
	}

	// We should now sign our new IP at the current timestamp.
	unsignedIP := UnsignedIP{
		AddrPort:  ip,
		Timestamp: s.clock.Unix(),
	}
	signedIP, err := unsignedIP.Sign(s.tlsSigner, s.blsSigner)
	if err != nil {
		return nil, err
	}

	s.signedIP = signedIP
	return s.signedIP, nil
}
