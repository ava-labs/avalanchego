// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
	"github.com/stretchr/testify/assert"
)

func TestSubnet(t *testing.T) {
	assert := assert.New(t)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()
	chainID2 := ids.GenerateTestID()

	s := newSubnet()
	s.addChain(chainID0)
	assert.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID0)
	assert.True(s.IsBootstrapped(), "A subnet with only bootstrapped chains should be considered bootstrapped")

	s.addChain(chainID1)
	assert.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.addChain(chainID2)
	assert.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID1)
	assert.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID2)
	assert.True(s.IsBootstrapped(), "A subnet with only bootstrapped chains should be considered bootstrapped")
}
