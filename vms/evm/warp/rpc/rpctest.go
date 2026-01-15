// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"testing"
	"github.com/ava-labs/avalanchego/vms/evm/warp/rpc/rpctest"
)

func TestService(t *testing.T) {
	rpctest.TestService(t)
}
