// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip"

	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// GethRPCBackends returns the backing infrastructure for geth's implementations
// of the JSON-RPC namespaces supported by the VM.
func (vm *VM) GethRPCBackends() saerpc.GethBackends {
	return vm.rpcProvider.GethBackends()
}

var _ saerpc.Chain = (*chain)(nil)

type chain struct {
	*saexec.Executor

	blockBuilder blockBuilder
	db           ethdb.Database
	xdb          saetypes.ExecutionResults
	mempool      *txgossip.Set
	network      *network.Network

	consensusCritical *syncMap[common.Hash, *blocks.Block]
	last              *last
	acceptedBlocks    *event.FeedOf[*blocks.Block]

	hooks     hook.Points
	rpcConfig saerpc.Config
	log       logging.Logger
}

func (c *chain) Logger() logging.Logger         { return c.log }
func (c *chain) Hooks() hook.Points             { return c.hooks }
func (c *chain) DB() ethdb.Database             { return c.db }
func (c *chain) XDB() saetypes.ExecutionResults { return c.xdb }
func (c *chain) Mempool() *txgossip.Set         { return c.mempool }
func (c *chain) Peers() *p2p.Peers              { return c.network.Peers }
func (c *chain) LastAccepted() *blocks.Block    { return c.last.accepted.Load() }
func (c *chain) LastSettled() *blocks.Block     { return c.last.settled.Load() }

func (c *chain) ConsensusCriticalBlock(h common.Hash) (*blocks.Block, bool) {
	return c.consensusCritical.Load(h)
}

func (c *chain) ResolvePendingToLastExecuted() bool {
	return c.rpcConfig.ResolvePendingToLastExecuted
}

func (c *chain) NewBlock(eth *types.Block, parent, lastSettled *blocks.Block) (*blocks.Block, error) {
	return c.blockBuilder.new(eth, parent, lastSettled)
}

func (c *chain) SubscribeAcceptedBlocks(ch chan<- *blocks.Block) event.Subscription {
	return c.acceptedBlocks.Subscribe(ch)
}
