// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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

// Package eth implements the Ethereum protocol.
package eth

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/accounts"
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/bloombits"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/pruner"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/eth/filters"
	"github.com/ava-labs/coreth/eth/gasprice"
	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/internal/ethapi"
	"github.com/ava-labs/coreth/internal/shutdowncheck"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

// Config contains the configuration options of the ETH protocol.
// Deprecated: use ethconfig.Config instead.
type Config = ethconfig.Config

var (
	DefaultSettings Settings = Settings{MaxBlocksPerRequest: 2000}
)

type Settings struct {
	MaxBlocksPerRequest int64 // Maximum number of blocks to serve per getLogs request
}

// Ethereum implements the Ethereum full node service.
type Ethereum struct {
	config *Config

	// Handlers
	txPool     *core.TxPool
	blockchain *core.BlockChain

	// DB interfaces
	chainDb ethdb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests     chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer      *core.ChainIndexer             // Bloom indexer operating during block imports
	closeBloomHandler chan struct{}

	APIBackend *EthAPIBackend

	miner     *miner.Miner
	etherbase common.Address

	networkID     uint64
	netRPCService *ethapi.PublicNetAPI

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and etherbase)

	shutdownTracker *shutdowncheck.ShutdownTracker // Tracks if and when the node has shutdown ungracefully

	stackRPCs []rpc.API

	settings Settings // Settings for Ethereum API
}

// roundUpCacheSize returns [input] rounded up to the next multiple of [allocSize]
func roundUpCacheSize(input int, allocSize int) int {
	cacheChunks := (input + allocSize - 1) / allocSize
	return cacheChunks * allocSize
}

// New creates a new Ethereum object (including the
// initialisation of the common Ethereum object)
func New(
	stack *node.Node,
	config *Config,
	cb *dummy.ConsensusCallbacks,
	chainDb ethdb.Database,
	settings Settings,
	lastAcceptedHash common.Hash,
	clock *mockable.Clock,
) (*Ethereum, error) {
	if chainDb == nil {
		return nil, errors.New("chainDb cannot be nil")
	}
	if !config.Pruning && config.TrieDirtyCache > 0 {
		// If snapshots are enabled, allocate 2/5 of the TrieDirtyCache memory cap to the snapshot cache
		if config.SnapshotCache > 0 {
			config.TrieCleanCache += config.TrieDirtyCache * 3 / 5
			config.SnapshotCache += config.TrieDirtyCache * 2 / 5
		} else {
			// If snapshots are disabled, the TrieDirtyCache will be written through to the clean cache
			// so move the cache allocation from the dirty cache to the clean cache
			config.TrieCleanCache += config.TrieDirtyCache
			config.TrieDirtyCache = 0
		}
	}

	// round TrieCleanCache and SnapshotCache up to nearest 64MB, since fastcache will mmap
	// memory in 64MBs chunks.
	config.TrieCleanCache = roundUpCacheSize(config.TrieCleanCache, 64)
	config.SnapshotCache = roundUpCacheSize(config.SnapshotCache, 64)

	log.Info(
		"Allocated trie memory caches",
		"clean", common.StorageSize(config.TrieCleanCache)*1024*1024,
		"dirty", common.StorageSize(config.TrieDirtyCache)*1024*1024,
	)

	chainConfig, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if genesisErr != nil {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	// Note: RecoverPruning must be called to handle the case that we are midway through offline pruning.
	// If the data directory is changed in between runs preventing RecoverPruning from performing its job correctly,
	// it may cause DB corruption.
	// Since RecoverPruning will only continue a pruning run that already began, we do not need to ensure that
	// reprocessState has already been called and completed successfully. To ensure this, we must maintain
	// that Prune is only run after reprocessState has finished successfully.
	if err := pruner.RecoverPruning(config.OfflinePruningDataDirectory, chainDb); err != nil {
		log.Error("Failed to recover state", "error", err)
	}
	eth := &Ethereum{
		config:            config,
		chainDb:           chainDb,
		eventMux:          new(event.TypeMux),
		accountManager:    stack.AccountManager(),
		engine:            dummy.NewDummyEngine(cb),
		closeBloomHandler: make(chan struct{}),
		networkID:         config.NetworkId,
		etherbase:         config.Miner.Etherbase,
		bloomRequests:     make(chan chan *bloombits.Retrieval),
		bloomIndexer:      core.NewBloomIndexer(chainDb, params.BloomBitsBlocks, params.BloomConfirms),
		settings:          settings,
		shutdownTracker:   shutdowncheck.NewShutdownTracker(chainDb),
	}

	bcVersion := rawdb.ReadDatabaseVersion(chainDb)
	var dbVer = "<nil>"
	if bcVersion != nil {
		dbVer = fmt.Sprintf("%d", *bcVersion)
	}
	log.Info("Initialising Ethereum protocol", "network", config.NetworkId, "dbversion", dbVer)

	if !config.SkipBcVersionCheck {
		if bcVersion != nil && *bcVersion > core.BlockChainVersion {
			return nil, fmt.Errorf("database version is v%d, Coreth %s only supports v%d", *bcVersion, params.VersionWithMeta, core.BlockChainVersion)
		} else if bcVersion == nil || *bcVersion < core.BlockChainVersion {
			log.Warn("Upgrade blockchain database version", "from", dbVer, "to", core.BlockChainVersion)
			rawdb.WriteDatabaseVersion(chainDb, core.BlockChainVersion)
		}
	}
	var (
		vmConfig = vm.Config{
			EnablePreimageRecording: config.EnablePreimageRecording,
			AllowUnfinalizedQueries: config.AllowUnfinalizedQueries,
		}
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit:                  config.TrieCleanCache,
			TrieDirtyLimit:                  config.TrieDirtyCache,
			TrieDirtyCommitTarget:           config.TrieDirtyCommitTarget,
			Pruning:                         config.Pruning,
			AcceptorQueueLimit:              config.AcceptorQueueLimit,
			CommitInterval:                  config.CommitInterval,
			PopulateMissingTries:            config.PopulateMissingTries,
			PopulateMissingTriesParallelism: config.PopulateMissingTriesParallelism,
			AllowMissingTries:               config.AllowMissingTries,
			SnapshotDelayInit:               config.SnapshotDelayInit,
			SnapshotLimit:                   config.SnapshotCache,
			SnapshotAsync:                   config.SnapshotAsync,
			SnapshotVerify:                  config.SnapshotVerify,
			SkipSnapshotRebuild:             config.SkipSnapshotRebuild,
			Preimages:                       config.Preimages,
		}
	)

	if err := eth.precheckPopulateMissingTries(); err != nil {
		return nil, err
	}

	var err error
	eth.blockchain, err = core.NewBlockChain(chainDb, cacheConfig, chainConfig, eth.engine, vmConfig, lastAcceptedHash)
	if err != nil {
		return nil, err
	}

	if err := eth.handleOfflinePruning(cacheConfig, chainConfig, vmConfig, lastAcceptedHash); err != nil {
		return nil, err
	}

	eth.bloomIndexer.Start(eth.blockchain)

	config.TxPool.Journal = ""
	eth.txPool = core.NewTxPool(config.TxPool, chainConfig, eth.blockchain)

	eth.miner = miner.New(eth, &config.Miner, chainConfig, eth.EventMux(), eth.engine, clock)

	eth.APIBackend = &EthAPIBackend{
		extRPCEnabled:       stack.Config().ExtRPCEnabled(),
		allowUnprotectedTxs: config.AllowUnprotectedTxs,
		eth:                 eth,
	}
	if config.AllowUnprotectedTxs {
		log.Info("Unprotected transactions allowed")
	}
	gpoParams := config.GPO
	eth.APIBackend.gpo = gasprice.NewOracle(eth.APIBackend, gpoParams)

	if err != nil {
		return nil, err
	}

	// Start the RPC service
	eth.netRPCService = ethapi.NewPublicNetAPI(eth.NetVersion())

	eth.stackRPCs = stack.APIs()

	// Successful startup; push a marker and check previous unclean shutdowns.
	eth.shutdownTracker.MarkStartup()

	return eth, nil
}

// APIs return the collection of RPC services the ethereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Ethereum) APIs() []rpc.API {
	apis := ethapi.GetAPIs(s.APIBackend)

	// Append tracing APIs
	apis = append(apis, tracers.APIs(s.APIBackend)...)

	// Add the APIs from the node
	apis = append(apis, s.stackRPCs...)

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicEthereumAPI(s),
			Public:    true,
			Name:      "public-eth",
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.APIBackend, false, 5*time.Minute),
			Public:    true,
			Name:      "public-eth-filter",
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
			Name:      "private-admin",
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
			Name:      "public-debug",
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(s),
			Name:      "private-debug",
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
			Name:      "net",
		},
	}...)
}

func (s *Ethereum) Etherbase() (eb common.Address, err error) {
	s.lock.RLock()
	etherbase := s.etherbase
	s.lock.RUnlock()

	if etherbase != (common.Address{}) {
		return etherbase, nil
	}
	if wallets := s.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			etherbase := accounts[0].Address

			s.lock.Lock()
			s.etherbase = etherbase
			s.lock.Unlock()

			log.Info("Etherbase automatically configured", "address", etherbase)
			return etherbase, nil
		}
	}
	return common.Address{}, fmt.Errorf("etherbase must be explicitly specified")
}

// SetEtherbase sets the mining reward address.
func (s *Ethereum) SetEtherbase(etherbase common.Address) {
	s.lock.Lock()
	s.etherbase = etherbase
	s.lock.Unlock()

	s.miner.SetEtherbase(etherbase)
}

func (s *Ethereum) Miner() *miner.Miner { return s.miner }

func (s *Ethereum) AccountManager() *accounts.Manager { return s.accountManager }
func (s *Ethereum) BlockChain() *core.BlockChain      { return s.blockchain }
func (s *Ethereum) TxPool() *core.TxPool              { return s.txPool }
func (s *Ethereum) EventMux() *event.TypeMux          { return s.eventMux }
func (s *Ethereum) Engine() consensus.Engine          { return s.engine }
func (s *Ethereum) ChainDb() ethdb.Database           { return s.chainDb }

func (s *Ethereum) NetVersion() uint64               { return s.networkID }
func (s *Ethereum) ArchiveMode() bool                { return !s.config.Pruning }
func (s *Ethereum) BloomIndexer() *core.ChainIndexer { return s.bloomIndexer }

// Start implements node.Lifecycle, starting all internal goroutines needed by the
// Ethereum protocol implementation.
func (s *Ethereum) Start() {
	// Start the bloom bits servicing goroutines
	s.startBloomHandlers(params.BloomBitsBlocks)

	// Regularly update shutdown marker
	s.shutdownTracker.Start()
}

// Stop implements node.Lifecycle, terminating all internal goroutines used by the
// Ethereum protocol.
// FIXME remove error from type if this will never return an error
func (s *Ethereum) Stop() error {
	s.bloomIndexer.Close()
	close(s.closeBloomHandler)
	s.txPool.Stop()
	s.blockchain.Stop()
	s.engine.Close()

	// Clean shutdown marker as the last thing before closing db
	s.shutdownTracker.Stop()

	s.chainDb.Close()
	s.eventMux.Stop()
	return nil
}

func (s *Ethereum) LastAcceptedBlock() *types.Block {
	return s.blockchain.LastAcceptedBlock()
}

// precheckPopulateMissingTries returns an error if config flags should prevent
// [populateMissingTries]
//
// NOTE: [populateMissingTries] is called from [New] to ensure all
// state is repaired before any async processes (specifically snapshot re-generation)
// are started which could interfere with historical re-generation.
func (s *Ethereum) precheckPopulateMissingTries() error {
	if s.config.PopulateMissingTries != nil && (s.config.Pruning || s.config.OfflinePruning) {
		return fmt.Errorf("cannot run populate missing tries when pruning (enabled: %t)/offline pruning (enabled: %t) is enabled", s.config.Pruning, s.config.OfflinePruning)
	}

	if s.config.PopulateMissingTries == nil {
		// Delete the populate missing tries marker to indicate that the node started with
		// populate missing tries disabled.
		if err := rawdb.DeletePopulateMissingTries(s.chainDb); err != nil {
			return fmt.Errorf("failed to write populate missing tries disabled marker: %w", err)
		}
		return nil
	}

	if lastRun, err := rawdb.ReadPopulateMissingTries(s.chainDb); err == nil {
		log.Error("Populate missing tries is not meant to be left enabled permanently. Please disable populate missing tries and allow your node to start successfully before running again.")
		return fmt.Errorf("cannot start chain with populate missing tries enabled on consecutive starts (last=%v)", lastRun)
	}

	// Note: Time Marker is written inside of [populateMissingTries] once it
	// succeeds inside of [NewBlockChain]
	return nil
}

func (s *Ethereum) handleOfflinePruning(cacheConfig *core.CacheConfig, chainConfig *params.ChainConfig, vmConfig vm.Config, lastAcceptedHash common.Hash) error {
	if s.config.OfflinePruning && !s.config.Pruning {
		return core.ErrRefuseToCorruptArchiver
	}

	if !s.config.OfflinePruning {
		// Delete the offline pruning marker to indicate that the node started with offline pruning disabled.
		if err := rawdb.DeleteOfflinePruning(s.chainDb); err != nil {
			return fmt.Errorf("failed to write offline pruning disabled marker: %w", err)
		}
		return nil
	}

	// Perform offline pruning after NewBlockChain has been called to ensure that we have rolled back the chain
	// to the last accepted block before pruning begins.
	// If offline pruning marker is on disk, then we force the node to be started with offline pruning disabled
	// before allowing another run of offline pruning.
	if lastRun, err := rawdb.ReadOfflinePruning(s.chainDb); err == nil {
		log.Error("Offline pruning is not meant to be left enabled permanently. Please disable offline pruning and allow your node to start successfully before running offline pruning again.")
		return fmt.Errorf("cannot start chain with offline pruning enabled on consecutive starts (last=%v)", lastRun)
	}

	// Clean up middle roots
	if err := s.blockchain.CleanBlockRootsAboveLastAccepted(); err != nil {
		return err
	}
	targetRoot := s.blockchain.LastAcceptedBlock().Root()

	// Allow the blockchain to be garbage collected immediately, since we will shut down the chain after offline pruning completes.
	s.blockchain.Stop()
	s.blockchain = nil
	log.Info("Starting offline pruning", "dataDir", s.config.OfflinePruningDataDirectory, "bloomFilterSize", s.config.OfflinePruningBloomFilterSize)
	pruner, err := pruner.NewPruner(s.chainDb, s.config.OfflinePruningDataDirectory, s.config.OfflinePruningBloomFilterSize)
	if err != nil {
		return fmt.Errorf("failed to create new pruner with data directory: %s, size: %d, due to: %w", s.config.OfflinePruningDataDirectory, s.config.OfflinePruningBloomFilterSize, err)
	}
	if err := pruner.Prune(targetRoot); err != nil {
		return fmt.Errorf("failed to prune blockchain with target root: %s due to: %w", targetRoot, err)
	}
	// Note: Time Marker is written inside of [Prune] before compaction begins
	// (considered an optional optimization)
	s.blockchain, err = core.NewBlockChain(s.chainDb, cacheConfig, chainConfig, s.engine, vmConfig, lastAcceptedHash)
	if err != nil {
		return fmt.Errorf("failed to re-initialize blockchain after offline pruning: %w", err)
	}

	return nil
}
