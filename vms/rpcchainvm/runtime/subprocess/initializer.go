// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
)

var _ runtime.Initializer = (*initializer)(nil)

// Subprocess VM Runtime intializer.
type initializer struct {
	once sync.Once
	// Address of the RPC Chain VM server
	vmAddr string
	// Error, if one occurred, during Initialization
	err error
	// Initialized is closed once Initialize is called
	initialized chan struct{}
}

func newInitializer() *initializer {
	return &initializer{
		initialized: make(chan struct{}),
	}
}

func (i *initializer) Initialize(_ context.Context, protocolVersion uint, vmAddr string) error {
	i.once.Do(func() {
		if version.RPCChainVMProtocol != protocolVersion {
			i.err = fmt.Errorf(
				"%w avalanchego: %d, vm: %d",
				runtime.ErrProtocolVersionMismatch,
				version.RPCChainVMProtocol,
				protocolVersion,
			)
		}
		i.vmAddr = vmAddr
		close(i.initialized)
	})
	return i.err
}
