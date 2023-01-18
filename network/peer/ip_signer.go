// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"sync"

	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// IPSigner will return a signedIP for the current value of our dynamic IP.
type IPSigner struct {
	ip     ips.DynamicIPPort
	clock  mockable.Clock
	signer crypto.Signer

	// Must be held while accessing [signedIP]
	signedIPLock sync.RWMutex
	// Note that the values in [*signedIP] are constants and can be inspected
	// without holding [signedIPLock].
	signedIP *SignedIP
}

func NewIPSigner(
	ip ips.DynamicIPPort,
	signer crypto.Signer,
) *IPSigner {
	return &IPSigner{
		ip:     ip,
		signer: signer,
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
	ip := s.ip.IPPort()
	if signedIP != nil && signedIP.IPPort.Equal(ip) {
		return signedIP, nil
	}

	// If our current IP hasn't been signed yet - then we should sign it.
	s.signedIPLock.Lock()
	defer s.signedIPLock.Unlock()

	// It's possible that multiple threads read [n.signedIP] as incorrect at the
	// same time, we should verify that we are the first thread to attempt to
	// update it.
	signedIP = s.signedIP
	if signedIP != nil && signedIP.IPPort.Equal(ip) {
		return signedIP, nil
	}

	// We should now sign our new IP at the current timestamp.
	unsignedIP := UnsignedIP{
		IPPort:    ip,
		Timestamp: s.clock.Unix(),
	}
	signedIP, err := unsignedIP.Sign(s.signer)
	if err != nil {
		return nil, err
	}

	s.signedIP = signedIP
	return s.signedIP, nil
}
