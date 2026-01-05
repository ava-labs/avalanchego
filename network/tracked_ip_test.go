// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var (
	ip      *ips.ClaimedIPPort
	otherIP *ips.ClaimedIPPort

	defaultLoopbackAddrPort = netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		9651,
	)
)

func init() {
	{
		cert, err := staking.NewTLSCert()
		if err != nil {
			panic(err)
		}
		stakingCert, err := staking.ParseCertificate(cert.Leaf.Raw)
		if err != nil {
			panic(err)
		}
		ip = ips.NewClaimedIPPort(
			stakingCert,
			defaultLoopbackAddrPort,
			1,   // timestamp
			nil, // signature
		)
	}

	{
		cert, err := staking.NewTLSCert()
		if err != nil {
			panic(err)
		}
		stakingCert, err := staking.ParseCertificate(cert.Leaf.Raw)
		if err != nil {
			panic(err)
		}
		otherIP = ips.NewClaimedIPPort(
			stakingCert,
			defaultLoopbackAddrPort,
			1,   // timestamp
			nil, // signature
		)
	}
}

func TestTrackedIP(t *testing.T) {
	require := require.New(t)

	ip := trackedIP{
		onStopTracking: make(chan struct{}),
	}

	require.Equal(time.Duration(0), ip.getDelay())

	ip.increaseDelay(time.Second, time.Minute)
	require.LessOrEqual(ip.getDelay(), 2*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	require.LessOrEqual(ip.getDelay(), 4*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	require.LessOrEqual(ip.getDelay(), 8*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	require.LessOrEqual(ip.getDelay(), 16*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	require.LessOrEqual(ip.getDelay(), 32*time.Second)

	for i := 0; i < 100; i++ {
		ip.increaseDelay(time.Second, time.Minute)
		require.LessOrEqual(ip.getDelay(), time.Minute)
	}
	require.GreaterOrEqual(ip.getDelay(), 45*time.Second)

	ip.stopTracking()
	<-ip.onStopTracking

	ip.stopTracking()
	<-ip.onStopTracking
}
