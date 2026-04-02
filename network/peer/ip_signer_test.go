// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

func TestIPSigner(t *testing.T) {
	require := require.New(t)

	dynIP := utils.NewAtomic(netip.AddrPortFrom(
		netip.IPv6Loopback(),
		0,
	))

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	tlsKey := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := localsigner.New()
	require.NoError(err)

	s := NewIPSigner(dynIP, tlsKey, blsKey)

	s.clock.Set(time.Unix(10, 0))

	signedIP1, err := s.GetSignedIP()
	require.NoError(err)
	require.Equal(dynIP.Get(), signedIP1.AddrPort)
	require.Equal(uint64(10), signedIP1.Timestamp)

	s.clock.Set(time.Unix(11, 0))

	signedIP2, err := s.GetSignedIP()
	require.NoError(err)
	require.Equal(dynIP.Get(), signedIP2.AddrPort)
	require.Equal(uint64(10), signedIP2.Timestamp)
	require.Equal(signedIP1.TLSSignature, signedIP2.TLSSignature)

	dynIP.Set(netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{1, 2, 3, 4}),
		dynIP.Get().Port(),
	))

	signedIP3, err := s.GetSignedIP()
	require.NoError(err)
	require.Equal(dynIP.Get(), signedIP3.AddrPort)
	require.Equal(uint64(11), signedIP3.Timestamp)
	require.NotEqual(signedIP2.TLSSignature, signedIP3.TLSSignature)
}
