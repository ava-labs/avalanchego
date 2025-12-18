// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/avalanchego/vms/saevm/evm"
	"github.com/ava-labs/strevm/adaptor"
)

func main() {
	vm := adaptor.Convert(&evm.VM{})
	rpcchainvm.Serve(context.Background(), vm)
}
