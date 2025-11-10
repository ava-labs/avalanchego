// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2023 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package simulated

import (
	"errors"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/constants"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/eth/ethconfig"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/graft/coreth/interfaces"
	"github.com/ava-labs/avalanchego/graft/coreth/node"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/rpc"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
)

var _ eth.PushGossiper = (*fakePushGossiper)(nil)

type fakePushGossiper struct{}

func (*fakePushGossiper) Add(*types.Transaction) {}

// Client exposes the methods provided by the Ethereum RPC client.
type Client interface {
	ethereum.BlockNumberReader
	ethereum.ChainReader
	ethereum.ChainStateReader
	ethereum.ContractCaller
	ethereum.GasEstimator
	ethereum.GasPricer
	ethereum.GasPricer1559
	ethereum.FeeHistoryReader
	ethereum.LogFilterer
	interfaces.AcceptedStateReader
	interfaces.AcceptedContractCaller
	ethereum.TransactionReader
	ethereum.TransactionSender
	ethereum.ChainIDReader
}

// simClient wraps ethclient. This exists to prevent extracting ethclient.Client
// from the Client interface returned by Backend.
type simClient struct {
	*ethclient.Client
}

// Backend is a simulated blockchain. You can use it to test your contracts or
// other code that interacts with the Ethereum chain.
type Backend struct {
	eth    *eth.Ethereum
	client simClient
	clock  *mockable.Clock
	server *rpc.Server
}

// NewBackend creates a new simulated blockchain that can be used as a backend for
// contract bindings in unit tests.
//
// A simulated backend always uses chainID 1337.
func NewBackend(alloc types.GenesisAlloc, options ...func(nodeConf *node.Config, ethConf *ethconfig.Config)) *Backend {
	chainConfig := *params.TestChainConfig
	chainConfig.ChainID = big.NewInt(1337)

	// Create the default configurations for the outer node shell and the Ethereum
	// service to mutate with the options afterwards
	nodeConf := node.DefaultConfig

	ethConf := ethconfig.DefaultConfig
	ethConf.Genesis = &core.Genesis{
		Config: &chainConfig,
		Alloc:  alloc,
	}
	ethConf.AllowUnfinalizedQueries = true
	ethConf.Miner.Etherbase = constants.BlackholeAddr
	ethConf.Miner.TestOnlyAllowDuplicateBlocks = true
	ethConf.TxPool.NoLocals = true

	for _, option := range options {
		option(&nodeConf, &ethConf)
	}
	// Assemble the Ethereum stack to run the chain with
	stack, err := node.New(&nodeConf)
	if err != nil {
		panic(err) // this should never happen
	}
	sim, err := newWithNode(stack, &ethConf, 0)
	if err != nil {
		panic(err) // this should never happen
	}
	return sim
}

// newWithNode sets up a simulated backend on an existing node. The provided node
// must not be started and will be started by this method.
func newWithNode(stack *node.Node, conf *eth.Config, blockPeriod uint64) (*Backend, error) {
	chaindb := rawdb.NewMemoryDatabase()
	clock := &mockable.Clock{}
	clock.Set(time.Unix(0, 0))

	engine := dummy.NewCoinbaseFaker()

	backend, err := eth.New(
		stack, conf, &fakePushGossiper{}, chaindb, eth.Settings{}, common.Hash{},
		engine, clock,
	)
	if err != nil {
		return nil, err
	}
	server := rpc.NewServer(0)
	for _, api := range backend.APIs() {
		if err := server.RegisterName(api.Namespace, api.Service); err != nil {
			return nil, err
		}
	}
	return &Backend{
		eth:    backend,
		client: simClient{ethclient.NewClient(rpc.DialInProc(server))},
		clock:  clock,
		server: server,
	}, nil
}

// Close shuts down the simBackend.
// The simulated backend can't be used afterwards.
func (n *Backend) Close() error {
	if n.client.Client != nil {
		n.client.Close()
	}
	n.server.Stop()
	return nil
}

// Commit seals a block and moves the chain forward to a new empty block.
func (n *Backend) Commit(accept bool) common.Hash {
	hash, err := n.buildBlock(accept, 10)
	if err != nil {
		panic(err)
	}
	return hash
}

func (n *Backend) buildBlock(accept bool, gap uint64) (common.Hash, error) {
	chain := n.eth.BlockChain()
	parent := chain.CurrentBlock()

	if err := n.eth.TxPool().Sync(); err != nil {
		return common.Hash{}, err
	}

	n.clock.Set(time.Unix(int64(parent.Time+gap), 0))
	block, err := n.eth.Miner().GenerateBlock(nil)
	if err != nil {
		return common.Hash{}, err
	}
	if err := chain.InsertBlock(block); err != nil {
		return common.Hash{}, err
	}
	if accept {
		if err := n.acceptAncestors(block); err != nil {
			return common.Hash{}, err
		}
		chain.DrainAcceptorQueue()
	}
	return block.Hash(), nil
}

func (n *Backend) acceptAncestors(block *types.Block) error {
	chain := n.eth.BlockChain()
	lastAccepted := chain.LastConsensusAcceptedBlock()

	// Accept all ancestors of the block
	toAccept := []*types.Block{block}
	for block.ParentHash() != lastAccepted.Hash() {
		block = chain.GetBlockByHash(block.ParentHash())
		toAccept = append(toAccept, block)
		if block.NumberU64() < lastAccepted.NumberU64() {
			return errors.New("last accepted must be an ancestor of the block to accept")
		}
	}

	for i := len(toAccept) - 1; i >= 0; i-- {
		if err := chain.Accept(toAccept[i]); err != nil {
			return err
		}
	}
	return nil
}

// Rollback removes all pending transactions, reverting to the last committed state.
func (n *Backend) Rollback() {
	// Flush all transactions from the transaction pools
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(common.Big1, 256), common.Big1)
	original := n.eth.TxPool().GasTip()
	n.eth.TxPool().SetGasTip(maxUint256)
	n.eth.TxPool().SetGasTip(original)
}

// Fork creates a side-chain that can be used to simulate reorgs.
//
// This function should be called with the ancestor block where the new side
// chain should be started. Transactions (old and new) can then be applied on
// top and Commit-ed.
//
// Note, the side-chain will only become canonical (and trigger the events) when
// it becomes longer. Until then CallContract will still operate on the current
// canonical chain.
//
// There is a % chance that the side chain becomes canonical at the same length
// to simulate live network behavior.
func (n *Backend) Fork(parentHash common.Hash) error {
	chain := n.eth.BlockChain()

	if chain.CurrentBlock().Hash() == parentHash {
		return nil
	}

	parent := chain.GetBlockByHash(parentHash)
	if parent == nil {
		return errors.New("parent block not found")
	}

	ch := make(chan core.NewTxPoolReorgEvent, 1)
	sub := n.eth.TxPool().SubscribeNewReorgEvent(ch)
	defer sub.Unsubscribe()

	if err := n.eth.BlockChain().SetPreference(parent); err != nil {
		return err
	}
	for {
		select {
		case reorg := <-ch:
			// Wait for tx pool to reorg, then flush the tx pool
			if reorg.Head.Hash() == parent.Hash() {
				n.Rollback()
				return nil
			}
		case <-time.After(2 * time.Second):
			return errors.New("fork not accepted")
		}
	}
}

// AdjustTime changes the block timestamp and creates a new block.
// It can only be called on empty blocks.
func (n *Backend) AdjustTime(adjustment time.Duration) error {
	_, err := n.buildBlock(false, uint64(adjustment))
	return err
}

// Client returns a client that accesses the simulated chain.
func (n *Backend) Client() Client {
	return n.client
}
