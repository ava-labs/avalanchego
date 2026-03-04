// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestOwnedOutputState(t *testing.T) {
	intf := interface{}(&OwnedOutput{})
	_, ok := intf.(verify.State)
	require.True(t, ok)
}
