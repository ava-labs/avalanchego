// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// GethRPCBackends returns the backing infrastructure for geth's implementations
// of the JSON-RPC namespaces supported by the VM.
func (vm *VM) GethRPCBackends() saerpc.GethBackends {
	return vm.rpcProvider.GethBackends()
}

func (vm *VM) chain() saerpc.Chain {
	return chain{vm, vm.exec}
}

type chain struct {
	*VM
	*saexec.Executor
}

func (c chain) Logger() logging.Logger         { return c.VM.snowCtx.Log }
func (c chain) Hooks() hook.Points             { return c.hooks }
func (c chain) DB() ethdb.Database             { return c.db }
func (c chain) XDB() saetypes.ExecutionResults { return c.xdb }
func (c chain) Mempool() *txgossip.Set         { return c.mempool }
func (c chain) Peers() *p2p.Peers              { return c.VM.Peers }
func (c chain) LastAccepted() *blocks.Block    { return c.last.accepted.Load() }
func (c chain) LastSettled() *blocks.Block     { return c.last.settled.Load() }

func (c chain) ConsensusCriticalBlock(h common.Hash) (*blocks.Block, bool) {
	return c.consensusCritical.Load(h)
}

func (c chain) NewBlock(eth *types.Block, parent, lastSettled *blocks.Block) (*blocks.Block, error) {
	return c.blockBuilder.new(eth, parent, lastSettled)
}

func (c chain) SubscribeAcceptedBlocks(ch chan<- *blocks.Block) event.Subscription {
	return c.acceptedBlocks.Subscribe(ch)
}
