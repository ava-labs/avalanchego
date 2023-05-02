// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/stretchr/testify/require"
)

func TestMintOutputState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&MintOutput{})
	_, ok := intf.(verify.State)
	require.True(ok)
}
