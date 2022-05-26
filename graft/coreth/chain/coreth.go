// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
)

var (
	BlackholeAddr = common.Address{
		1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
)

type Tx = types.Transaction
type Block = types.Block
type Hash = common.Hash

type ETHChain struct {
	backend *eth.Ethereum
}

// NewETHChain creates an Ethereum blockchain with the given configs.
func NewETHChain(config *eth.Config, nodecfg *node.Config, chainDB ethdb.Database, settings eth.Settings, consensusCallbacks *dummy.ConsensusCallbacks, lastAcceptedHash common.Hash, clock *mockable.Clock) (*ETHChain, error) {
	node, err := node.New(nodecfg)
	if err != nil {
		return nil, err
	}
	backend, err := eth.New(node, config, consensusCallbacks, chainDB, settings, lastAcceptedHash, clock)
	if err != nil {
		return nil, fmt.Errorf("failed to create backend: %w", err)
	}
	chain := &ETHChain{backend: backend}
	backend.SetEtherbase(BlackholeAddr)
	return chain, nil
}

func (self *ETHChain) Start() {
	self.backend.Start()
}

func (self *ETHChain) Stop() {
	self.backend.Stop()
}

func (self *ETHChain) GenerateBlock() (*types.Block, error) {
	return self.backend.Miner().GenerateBlock()
}

func (self *ETHChain) BlockChain() *core.BlockChain {
	return self.backend.BlockChain()
}

func (self *ETHChain) APIBackend() *eth.EthAPIBackend {
	return self.backend.APIBackend
}

func (self *ETHChain) PendingSize() int {
	pending := self.backend.TxPool().Pending(true)
	count := 0
	for _, txs := range pending {
		count += len(txs)
	}
	return count
}

func (self *ETHChain) AddRemoteTxs(txs []*types.Transaction) []error {
	return self.backend.TxPool().AddRemotes(txs)
}

func (self *ETHChain) AddRemoteTxsSync(txs []*types.Transaction) []error {
	return self.backend.TxPool().AddRemotesSync(txs)
}

func (self *ETHChain) AddLocalTxs(txs []*types.Transaction) []error {
	return self.backend.TxPool().AddLocals(txs)
}

func (self *ETHChain) CurrentBlock() *types.Block {
	return self.backend.BlockChain().CurrentBlock()
}

// Returns a new mutable state based on the current HEAD block.
func (self *ETHChain) CurrentState() (*state.StateDB, error) {
	return self.backend.BlockChain().State()
}

// Returns a new mutable state based on the given block.
func (self *ETHChain) BlockState(block *types.Block) (*state.StateDB, error) {
	return self.backend.BlockChain().StateAt(block.Root())
}

// Retrives a block from the database by hash.
func (self *ETHChain) GetBlockByHash(hash common.Hash) *types.Block {
	return self.backend.BlockChain().GetBlockByHash(hash)
}

// Retrives a block from the database by number.
func (self *ETHChain) GetBlockByNumber(num uint64) *types.Block {
	return self.backend.BlockChain().GetBlockByNumber(num)
}

// Validate the canonical chain from current block to the genesis.
// This should only be called as a convenience method in tests, not
// in production as it traverses the entire chain.
func (self *ETHChain) ValidateCanonicalChain() error {
	return self.backend.BlockChain().ValidateCanonicalChain()
}

// SetPreference sets the current head block to the one provided as an argument
// regardless of what the chain contents were prior.
func (self *ETHChain) SetPreference(block *types.Block) error {
	return self.BlockChain().SetPreference(block)
}

// Accept sets a minimum height at which no reorg can pass. Additionally,
// this function may trigger a reorg if the block being accepted is not in the
// canonical chain.
func (self *ETHChain) Accept(block *types.Block) error {
	return self.BlockChain().Accept(block)
}

// Reject tells the chain that [block] has been rejected.
func (self *ETHChain) Reject(block *types.Block) error {
	return self.BlockChain().Reject(block)
}

// LastConsensusAcceptedBlock returns the last block to be marked as accepted.
// It may or may not be processed.
func (self *ETHChain) LastConsensusAcceptedBlock() *types.Block {
	return self.BlockChain().LastConsensusAcceptedBlock()
}

// LastAcceptedBlock returns the last block to be marked as accepted and
// processed.
func (self *ETHChain) LastAcceptedBlock() *types.Block {
	return self.BlockChain().LastAcceptedBlock()
}

// RemoveRejectedBlocks removes the rejected blocks between heights
// [start] and [end].
func (self *ETHChain) RemoveRejectedBlocks(start, end uint64) error {
	return self.BlockChain().RemoveRejectedBlocks(start, end)
}

func (self *ETHChain) GetReceiptsByHash(hash common.Hash) types.Receipts {
	return self.backend.BlockChain().GetReceiptsByHash(hash)
}

func (self *ETHChain) GetGenesisBlock() *types.Block {
	return self.backend.BlockChain().Genesis()
}

func (self *ETHChain) InsertBlock(block *types.Block) error {
	return self.backend.BlockChain().InsertBlock(block)
}

func (self *ETHChain) NewRPCHandler(maximumDuration time.Duration) *rpc.Server {
	return rpc.NewServer(maximumDuration)
}

// AttachEthService registers the backend RPC services provided by Ethereum
// to the provided handler under their assigned namespaces.
func (self *ETHChain) AttachEthService(handler *rpc.Server, names []string) error {
	enabledServicesSet := make(map[string]struct{})
	for _, ns := range names {
		enabledServicesSet[ns] = struct{}{}
	}

	apiSet := make(map[string]rpc.API)
	for _, api := range self.backend.APIs() {
		if existingAPI, exists := apiSet[api.Name]; exists {
			return fmt.Errorf("duplicated API name: %s, namespaces %s and %s", api.Name, api.Namespace, existingAPI.Namespace)
		}
		apiSet[api.Name] = api
	}

	for name := range enabledServicesSet {
		api, exists := apiSet[name]
		if !exists {
			return fmt.Errorf("API service %s not found", name)
		}
		if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}

	return nil
}

func (self *ETHChain) GetTxSubmitCh() <-chan core.NewTxsEvent {
	newTxsChan := make(chan core.NewTxsEvent)
	self.backend.TxPool().SubscribeNewTxsEvent(newTxsChan)
	return newTxsChan
}

func (self *ETHChain) GetTxAcceptedSubmitCh() <-chan core.NewTxsEvent {
	newTxsChan := make(chan core.NewTxsEvent)
	self.backend.BlockChain().SubscribeAcceptedTransactionEvent(newTxsChan)
	return newTxsChan
}

func (self *ETHChain) GetTxPool() *core.TxPool          { return self.backend.TxPool() }
func (self *ETHChain) BloomIndexer() *core.ChainIndexer { return self.backend.BloomIndexer() }
