// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beacon

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSet(t *testing.T) {
	require := require.New(t)

	id0 := ids.BuildTestNodeID([]byte{0})
	id1 := ids.BuildTestNodeID([]byte{1})
	id2 := ids.BuildTestNodeID([]byte{2})

	ip0 := netip.AddrPortFrom(
		netip.IPv4Unspecified(),
		0,
	)
	ip1 := netip.AddrPortFrom(
		netip.IPv4Unspecified(),
		1,
	)
	ip2 := netip.AddrPortFrom(
		netip.IPv4Unspecified(),
		2,
	)

	b0 := New(id0, ip0)
	b1 := New(id1, ip1)
	b2 := New(id2, ip2)

	s := NewSet()

	require.Empty(s.IDsArg())
	require.Empty(s.IPsArg())
	require.Zero(s.Len())

	require.NoError(s.Add(b0))

	require.Equal("NodeID-111111111111111111116DBWJs", s.IDsArg())
	require.Equal("0.0.0.0:0", s.IPsArg())
	require.Equal(1, s.Len())

	err := s.Add(b0)
	require.ErrorIs(err, errDuplicateID)

	require.Equal("NodeID-111111111111111111116DBWJs", s.IDsArg())
	require.Equal("0.0.0.0:0", s.IPsArg())
	require.Equal(1, s.Len())

	require.NoError(s.Add(b1))

	require.Equal("NodeID-111111111111111111116DBWJs,NodeID-6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt", s.IDsArg())
	require.Equal("0.0.0.0:0,0.0.0.0:1", s.IPsArg())
	require.Equal(2, s.Len())

	require.NoError(s.Add(b2))

	require.Equal("NodeID-111111111111111111116DBWJs,NodeID-6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt,NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp", s.IDsArg())
	require.Equal("0.0.0.0:0,0.0.0.0:1,0.0.0.0:2", s.IPsArg())
	require.Equal(3, s.Len())

	require.NoError(s.RemoveByID(b0.ID()))

	require.Equal("NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp,NodeID-6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt", s.IDsArg())
	require.Equal("0.0.0.0:2,0.0.0.0:1", s.IPsArg())
	require.Equal(2, s.Len())

	require.NoError(s.RemoveByIP(b1.IP()))

	require.Equal("NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp", s.IDsArg())
	require.Equal("0.0.0.0:2", s.IPsArg())
	require.Equal(1, s.Len())
}
