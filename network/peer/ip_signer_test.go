// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/ips"
)

func TestIPSigner(t *testing.T) {
	require := require.New(t)

	dynIP := ips.NewDynamicIPPort(
		net.IPv6loopback,
		0,
	)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	key := tlsCert.PrivateKey.(crypto.Signer)

	s := NewIPSigner(dynIP, key)

	s.clock.Set(time.Unix(10, 0))

	signedIP1, err := s.GetSignedIP()
	require.NoError(err)
	require.Equal(dynIP.IPPort(), signedIP1.IPPort)
	require.Equal(uint64(10), signedIP1.Timestamp)

	s.clock.Set(time.Unix(11, 0))

	signedIP2, err := s.GetSignedIP()
	require.NoError(err)
	require.Equal(dynIP.IPPort(), signedIP2.IPPort)
	require.Equal(uint64(10), signedIP2.Timestamp)
	require.Equal(signedIP1.Signature, signedIP2.Signature)

	dynIP.SetIP(net.IPv4(1, 2, 3, 4))

	signedIP3, err := s.GetSignedIP()
	require.NoError(err)
	require.Equal(dynIP.IPPort(), signedIP3.IPPort)
	require.Equal(uint64(11), signedIP3.Timestamp)
	require.NotEqual(signedIP2.Signature, signedIP3.Signature)
}
