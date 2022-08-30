// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSubnet(t *testing.T) {
	require := require.New(t)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()
	chainID2 := ids.GenerateTestID()

	s := newSubnet()
	s.addChain(chainID0)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID0)
	require.True(s.IsBootstrapped(), "A subnet with only bootstrapped chains should be considered bootstrapped")

	s.addChain(chainID1)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.addChain(chainID2)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID1)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID2)
	require.True(s.IsBootstrapped(), "A subnet with only bootstrapped chains should be considered bootstrapped")
}
