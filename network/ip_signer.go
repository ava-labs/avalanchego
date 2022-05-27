// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"sync"

	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// ipSigner will return a signedIP for the current value of our dynamic IP.
type ipSigner struct {
	ip     ips.DynamicIPPort
	clock  *mockable.Clock
	signer crypto.Signer

	// Must be held while accessing [signedIP]
	signedIPLock sync.RWMutex
	// Note that the values in [*signedIP] are constants and can be inspected
	// without holding [signedIPLock].
	signedIP *peer.SignedIP
}

func newIPSigner(
	ip ips.DynamicIPPort,
	clock *mockable.Clock,
	signer crypto.Signer,
) *ipSigner {
	return &ipSigner{
		ip:     ip,
		clock:  clock,
		signer: signer,
	}
}

// getSignedIP returns the signedIP of the current value of the provided
// dynamicIP. If the dynamicIP hasn't changed since the prior call to
// getSignedIP, then the same [SignedIP] will be returned.
//
// It's safe for multiple goroutines to concurrently call getSignedIP.
func (s *ipSigner) getSignedIP() (*peer.SignedIP, error) {
	// Optimistically, the IP should already be signed. By grabbing a read lock
	// here we enable full concurrency of new connections.
	s.signedIPLock.RLock()
	signedIP := s.signedIP
	s.signedIPLock.RUnlock()
	ip := s.ip.IPPort()
	if signedIP != nil && signedIP.IP.IP.Equal(ip) {
		return signedIP, nil
	}

	// If our current IP hasn't been signed yet - then we should sign it.
	s.signedIPLock.Lock()
	defer s.signedIPLock.Unlock()

	// It's possible that multiple threads read [n.signedIP] as incorrect at the
	// same time, we should verify that we are the first thread to attempt to
	// update it.
	signedIP = s.signedIP
	if signedIP != nil && signedIP.IP.IP.Equal(ip) {
		return signedIP, nil
	}

	// We should now sign our new IP at the current timestamp.
	unsignedIP := peer.UnsignedIP{
		IP:        ip,
		Timestamp: s.clock.Unix(),
	}
	signedIP, err := unsignedIP.Sign(s.signer)
	if err != nil {
		return nil, err
	}

	s.signedIP = signedIP
	return s.signedIP, nil
}
