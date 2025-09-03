// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/simplex"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap/queue"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/state"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/syncer"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/fx"
	"github.com/ava-labs/avalanchego/vms/metervm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/proposervm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/tracedvm"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	aveng "github.com/ava-labs/avalanchego/snow/engine/avalanche"
	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
	avagetter "github.com/ava-labs/avalanchego/snow/engine/avalanche/getter"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	smbootstrap "github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	timetracker "github.com/ava-labs/avalanchego/snow/networking/tracker"
)

const (
	ChainLabel = "chain"

	defaultChannelSize = 1
	initialQueueSize   = 3

	avalancheNamespace    = constants.PlatformName + metric.NamespaceSeparator + "avalanche"
	handlerNamespace      = constants.PlatformName + metric.NamespaceSeparator + "handler"
	meterchainvmNamespace = constants.PlatformName + metric.NamespaceSeparator + "meterchainvm"
	meterdagvmNamespace   = constants.PlatformName + metric.NamespaceSeparator + "meterdagvm"
	proposervmNamespace   = constants.PlatformName + metric.NamespaceSeparator + "proposervm"
	p2pNamespace          = constants.PlatformName + metric.NamespaceSeparator + "p2p"
	snowmanNamespace      = constants.PlatformName + metric.NamespaceSeparator + "snowman"
	stakeNamespace        = constants.PlatformName + metric.NamespaceSeparator + "stake"
)

var (
	// Commonly shared VM DB prefix
	VMDBPrefix = []byte("vm")

	// Bootstrapping prefixes for LinearizableVMs
	VertexDBPrefix              = []byte("vertex")
	VertexBootstrappingDBPrefix = []byte("vertex_bs")
	TxBootstrappingDBPrefix     = []byte("tx_bs")
	BlockBootstrappingDBPrefix  = []byte("interval_block_bs")

	// Bootstrapping prefixes for ChainVMs
	ChainBootstrappingDBPrefix = []byte("interval_bs")

	// Prefix used for simplex storage
	simplexDBPrefix = []byte("simplex")

	errUnknownVMType           = errors.New("the vm should have type avalanche.DAGVM or snowman.ChainVM")
	errCreatePlatformVM        = errors.New("attempted to create a chain running the PlatformVM")
	errNotBootstrapped         = errors.New("subnets not bootstrapped")
	errPartialSyncAsAValidator = errors.New("partial sync should not be configured for a validator")

	fxs = map[ids.ID]fx.Factory{
		secp256k1fx.ID: &secp256k1fx.Factory{},
		nftfx.ID:       &nftfx.Factory{},
		propertyfx.ID:  &propertyfx.Factory{},
	}

	_ Manager = (*manager)(nil)
)

// Manager manages the chains running on this node.
// It can:
//   - Create a chain
//   - Add a registrant. When a chain is created, each registrant calls
//     RegisterChain with the new chain as the argument.
//   - Manage the aliases of chains
type Manager interface {
	ids.Aliaser

	// Queues a chain to be created in the future after chain creator is unblocked.
	// This is only called from the P-chain thread to create other chains
	// Queued chains are created only after P-chain is bootstrapped.
	// This assumes only chains in tracked subnets are queued.
	QueueChainCreation(ChainParameters)

	// Add a registrant [r]. Every time a chain is
	// created, [r].RegisterChain([new chain]) is called.
	AddRegistrant(Registrant)

	// Given an alias, return the ID of the chain associated with that alias
	Lookup(string) (ids.ID, error)

	// Given an alias, return the ID of the VM associated with that alias
	LookupVM(string) (ids.ID, error)

	// Returns true iff the chain with the given ID exists and is finished bootstrapping
	IsBootstrapped(ids.ID) bool

	// Starts the chain creator with the initial platform chain parameters, must
	// be called once.
	StartChainCreator(platformChain ChainParameters) error

	Shutdown()
}

// ChainParameters defines the chain being created
type ChainParameters struct {
	// The ID of the chain being created.
	ID ids.ID
	// ID of the subnet that validates this chain.
	SubnetID ids.ID
	// The genesis data of this chain's ledger.
	GenesisData []byte
	// The ID of the vm this chain is running.
	VMID ids.ID
	// The IDs of the feature extensions this chain is running.
	FxIDs []ids.ID
	// Invariant: Only used when [ID] is the P-chain ID.
	CustomBeacons validators.Manager
}

type chain struct {
	Name    string
	Context *snow.ConsensusContext
	VM      common.VM
	Handler handler.Handler
}

// ChainConfig is configuration settings for the current execution.
// [Config] is the user-provided config blob for the chain.
// [Upgrade] is a chain-specific blob for coordinating upgrades.
type ChainConfig struct {
	Config  []byte
	Upgrade []byte
}

type ManagerConfig struct {
	SybilProtectionEnabled bool
	StakingTLSSigner       crypto.Signer
	StakingTLSCert         *staking.Certificate
	StakingBLSKey          bls.Signer
	TracingEnabled         bool
	// Must not be used unless [TracingEnabled] is true as this may be nil.
	Tracer                    trace.Tracer
	Log                       logging.Logger
	LogFactory                logging.Factory
	VMManager                 vms.Manager // Manage mappings from vm ID --> vm
	BlockAcceptorGroup        snow.AcceptorGroup
	TxAcceptorGroup           snow.AcceptorGroup
	VertexAcceptorGroup       snow.AcceptorGroup
	DB                        database.Database
	MsgCreator                message.OutboundMsgBuilder // message creator, shared with network
	Router                    router.Router              // Routes incoming messages to the appropriate chain
	Net                       network.Network            // Sends consensus messages to other validators
	Validators                validators.Manager         // Validators validating on this chain
	NodeID                    ids.NodeID                 // The ID of this node
	NetworkID                 uint32                     // ID of the network this node is connected to
	PartialSyncPrimaryNetwork bool
	Server                    server.Server // Handles HTTP API calls
	AtomicMemory              *atomic.Memory
	AVAXAssetID               ids.ID
	XChainID                  ids.ID          // ID of the X-Chain,
	CChainID                  ids.ID          // ID of the C-Chain,
	CriticalChains            set.Set[ids.ID] // Chains that can't exit gracefully
	TimeoutManager            timeout.Manager // Manages request timeouts when sending messages to other validators
	Health                    health.Registerer
	SubnetConfigs             map[ids.ID]subnets.Config // ID -> SubnetConfig
	ChainConfigs              map[string]ChainConfig    // alias -> ChainConfig
	// ShutdownNodeFunc allows the chain manager to issue a request to shutdown the node
	ShutdownNodeFunc func(exitCode int)
	MeterVMEnabled   bool // Should each VM be wrapped with a MeterVM

	Metrics        metrics.MultiGatherer
	MeterDBMetrics metrics.MultiGatherer

	FrontierPollFrequency   time.Duration
	ConsensusAppConcurrency int

	// Max Time to spend fetching a container and its
	// ancestors when responding to a GetAncestors
	BootstrapMaxTimeGetAncestors time.Duration
	// Max number of containers in an ancestors message sent by this node.
	BootstrapAncestorsMaxContainersSent int
	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	BootstrapAncestorsMaxContainersReceived int

	Upgrades upgrade.Config

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker timetracker.ResourceTracker

	StateSyncBeacons []ids.NodeID

	ChainDataDir string

	Subnets *Subnets
}

type manager struct {
	// Note: The string representation of a chain's ID is also considered to be an alias of the chain
	// That is, [chainID].String() is an alias for the chain, too
	ids.Aliaser
	ManagerConfig

	// Those notified when a chain is created
	registrants []Registrant

	// queue that holds chain create requests
	chainsQueue buffer.BlockingDeque[ChainParameters]
	// unblocks chain creator to start processing the queue
	unblockChainCreatorCh chan struct{}
	// shutdown the chain creator goroutine if the queue hasn't started to be
	// processed.
	chainCreatorShutdownCh chan struct{}
	chainCreatorExited     sync.WaitGroup

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]handler.Handler

	// snowman++ related interface to allow validators retrieval
	validatorState validators.State

	avalancheGatherer    metrics.MultiGatherer            // chainID
	handlerGatherer      metrics.MultiGatherer            // chainID
	meterChainVMGatherer metrics.MultiGatherer            // chainID
	meterDAGVMGatherer   metrics.MultiGatherer            // chainID
	proposervmGatherer   metrics.MultiGatherer            // chainID
	p2pGatherer          metrics.MultiGatherer            // chainID
	snowmanGatherer      metrics.MultiGatherer            // chainID
	stakeGatherer        metrics.MultiGatherer            // chainID
	vmGatherer           map[ids.ID]metrics.MultiGatherer // vmID -> chainID
}

// New returns a new Manager
func New(config *ManagerConfig) (Manager, error) {
	avalancheGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(avalancheNamespace, avalancheGatherer); err != nil {
		return nil, err
	}

	handlerGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(handlerNamespace, handlerGatherer); err != nil {
		return nil, err
	}

	meterChainVMGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterchainvmNamespace, meterChainVMGatherer); err != nil {
		return nil, err
	}

	meterDAGVMGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterdagvmNamespace, meterDAGVMGatherer); err != nil {
		return nil, err
	}

	proposervmGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(proposervmNamespace, proposervmGatherer); err != nil {
		return nil, err
	}

	p2pGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(p2pNamespace, p2pGatherer); err != nil {
		return nil, err
	}

	snowmanGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(snowmanNamespace, snowmanGatherer); err != nil {
		return nil, err
	}

	stakeGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(stakeNamespace, stakeGatherer); err != nil {
		return nil, err
	}

	return &manager{
		Aliaser:                ids.NewAliaser(),
		ManagerConfig:          *config,
		chains:                 make(map[ids.ID]handler.Handler),
		chainsQueue:            buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize),
		unblockChainCreatorCh:  make(chan struct{}),
		chainCreatorShutdownCh: make(chan struct{}),

		avalancheGatherer:    avalancheGatherer,
		handlerGatherer:      handlerGatherer,
		meterChainVMGatherer: meterChainVMGatherer,
		meterDAGVMGatherer:   meterDAGVMGatherer,
		proposervmGatherer:   proposervmGatherer,
		p2pGatherer:          p2pGatherer,
		snowmanGatherer:      snowmanGatherer,
		stakeGatherer:        stakeGatherer,
		vmGatherer:           make(map[ids.ID]metrics.MultiGatherer),
	}, nil
}

// QueueChainCreation queues a chain creation request
// Invariant: Tracked Subnet must be checked before calling this function
func (m *manager) QueueChainCreation(chainParams ChainParameters) {
	if sb, _ := m.Subnets.GetOrCreate(chainParams.SubnetID); !sb.AddChain(chainParams.ID) {
		m.Log.Debug("skipping chain creation",
			zap.String("reason", "chain already staged"),
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
		)
		return
	}

	if ok := m.chainsQueue.PushRight(chainParams); !ok {
		m.Log.Warn("skipping chain creation",
			zap.String("reason", "couldn't enqueue chain"),
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
		)
	}
}

// createChain creates and starts the chain
//
// Note: it is expected for the subnet to already have the chain registered as
// bootstrapping before this function is called
func (m *manager) createChain(chainParams ChainParameters) {
	m.Log.Info("creating chain",
		zap.Stringer("subnetID", chainParams.SubnetID),
		zap.Stringer("chainID", chainParams.ID),
		zap.Stringer("vmID", chainParams.VMID),
	)

	sb, _ := m.Subnets.GetOrCreate(chainParams.SubnetID)

	// Note: buildChain builds all chain's relevant objects (notably engine and handler)
	// but does not start their operations. Starting of the handler (which could potentially
	// issue some internal messages), is delayed until chain dispatching is started and
	// the chain is registered in the manager. This ensures that no message generated by handler
	// upon start is dropped.
	chain, err := m.buildChain(chainParams, sb)
	if err != nil {
		if m.CriticalChains.Contains(chainParams.ID) {
			// Shut down if we fail to create a required chain (i.e. X, P or C)
			m.Log.Fatal("error creating required chain",
				zap.Stringer("subnetID", chainParams.SubnetID),
				zap.Stringer("chainID", chainParams.ID),
				zap.Stringer("vmID", chainParams.VMID),
				zap.Error(err),
			)
			go m.ShutdownNodeFunc(1)
			return
		}

		chainAlias := m.PrimaryAliasOrDefault(chainParams.ID)
		m.Log.Error("error creating chain",
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.String("chainAlias", chainAlias),
			zap.Stringer("vmID", chainParams.VMID),
			zap.Error(err),
		)

		// Register the health check for this chain regardless of if it was
		// created or not. This attempts to notify the node operator that their
		// node may not be properly validating the subnet they expect to be
		// validating.
		healthCheckErr := fmt.Errorf("failed to create chain on subnet %s: %w", chainParams.SubnetID, err)
		err := m.Health.RegisterHealthCheck(
			chainAlias,
			health.CheckerFunc(func(context.Context) (interface{}, error) {
				return nil, healthCheckErr
			}),
			chainParams.SubnetID.String(),
		)
		if err != nil {
			m.Log.Error("failed to register failing health check",
				zap.Stringer("subnetID", chainParams.SubnetID),
				zap.Stringer("chainID", chainParams.ID),
				zap.String("chainAlias", chainAlias),
				zap.Stringer("vmID", chainParams.VMID),
				zap.Error(err),
			)
		}
		return
	}

	m.chainsLock.Lock()
	m.chains[chainParams.ID] = chain.Handler
	m.chainsLock.Unlock()

	// Associate the newly created chain with its default alias
	if err := m.Alias(chainParams.ID, chainParams.ID.String()); err != nil {
		m.Log.Error("failed to alias the new chain with itself",
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
			zap.Error(err),
		)
	}

	// Notify those who registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Context, chain.VM)

	// Allows messages to be routed to the new chain. If the handler hasn't been
	// started and a message is forwarded, then the message will block until the
	// handler is started.
	m.ManagerConfig.Router.AddChain(context.TODO(), chain.Handler)

	// Register bootstrapped health checks after P chain has been added to
	// chains.
	//
	// Note: Registering this after the chain has been tracked prevents a race
	//       condition between the health check and adding the first chain to
	//       the manager.
	if chainParams.ID == constants.PlatformChainID {
		if err := m.registerBootstrappedHealthChecks(); err != nil {
			chain.Handler.StopWithError(context.TODO(), err)
		}
	}

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	chain.Handler.Start(context.TODO(), !m.CriticalChains.Contains(chainParams.ID))
}

// Create a chain
func (m *manager) buildChain(chainParams ChainParameters, sb subnets.Subnet) (*chain, error) {
	if chainParams.ID != constants.PlatformChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	primaryAlias := m.PrimaryAliasOrDefault(chainParams.ID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainParams.ID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog, err := m.LogFactory.MakeChain(primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error while creating chain's log %w", err)
	}

	ctx := &snow.ConsensusContext{
		Context: &snow.Context{
			NetworkID:       m.NetworkID,
			SubnetID:        chainParams.SubnetID,
			ChainID:         chainParams.ID,
			NodeID:          m.NodeID,
			PublicKey:       m.StakingBLSKey.PublicKey(),
			NetworkUpgrades: m.Upgrades,

			XChainID:    m.XChainID,
			CChainID:    m.CChainID,
			AVAXAssetID: m.AVAXAssetID,

			Log:          chainLog,
			SharedMemory: m.AtomicMemory.NewSharedMemory(chainParams.ID),
			BCLookup:     m,
			Metrics:      metrics.NewPrefixGatherer(),

			WarpSigner: warp.NewSigner(m.StakingBLSKey, m.NetworkID, chainParams.ID),

			ValidatorState: m.validatorState,
			ChainDataDir:   chainDataDir,
		},
		PrimaryAlias:   primaryAlias,
		Registerer:     prometheus.NewRegistry(),
		BlockAcceptor:  m.BlockAcceptorGroup,
		TxAcceptor:     m.TxAcceptorGroup,
		VertexAcceptor: m.VertexAcceptorGroup,
	}

	// Get a factory for the vm we want to use on our chain
	vmFactory, err := m.VMManager.GetFactory(chainParams.VMID)
	if err != nil {
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}

	// Create the chain
	vm, err := vmFactory.New(chainLog)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm: %w", err)
	}
	// TODO: Shutdown VM if an error occurs

	chainFxs := make([]*common.Fx, len(chainParams.FxIDs))
	for i, fxID := range chainParams.FxIDs {
		fxFactory, ok := fxs[fxID]
		if !ok {
			return nil, fmt.Errorf("fx %s not found", fxID)
		}

		chainFxs[i] = &common.Fx{
			ID: fxID,
			Fx: fxFactory.New(),
		}
	}

	var chain *chain
	switch vm := vm.(type) {
	case vertex.LinearizableVMWithEngine:
		chain, err = m.createAvalancheChain(
			ctx,
			chainParams.GenesisData,
			m.Validators,
			vm,
			chainFxs,
			sb,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new avalanche vm %w", err)
		}
	case block.ChainVM:
		// handle simplex engine based off parameters
		if sb.Config().ConsensusConfig.SimplexParams != nil {
			chain, err = m.createSimplexChain(ctx, vm, sb, chainParams.GenesisData, chainFxs)
			if err != nil {
				return nil, fmt.Errorf("error while creating simplex chain %w", err)
			}
			break
		}

		beacons := m.Validators
		if chainParams.ID == constants.PlatformChainID {
			beacons = chainParams.CustomBeacons
		}

		chain, err = m.createSnowmanChain(
			ctx,
			chainParams.GenesisData,
			m.Validators,
			beacons,
			vm,
			chainFxs,
			sb,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new snowman vm %w", err)
		}
	default:
		return nil, errUnknownVMType
	}

	// Register the chain with the timeout manager
	if err := m.TimeoutManager.RegisterChain(ctx); err != nil {
		return nil, err
	}

	vmGatherer, err := m.getOrMakeVMGatherer(chainParams.VMID)
	if err != nil {
		return nil, err
	}

	return chain, errors.Join(
		m.snowmanGatherer.Register(primaryAlias, ctx.Registerer),
		vmGatherer.Register(primaryAlias, ctx.Metrics),
	)
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// Create a DAG-based blockchain that uses Avalanche
func (m *manager) createAvalancheChain(
	ctx *snow.ConsensusContext,
	genesisData []byte,
	vdrs validators.Manager,
	vm vertex.LinearizableVMWithEngine,
	fxs []*common.Fx,
	sb subnets.Subnet,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_DAG,
		State: snow.Initializing,
	})

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	meterDBReg, err := metrics.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	meterDB, err := meterdb.New(meterDBReg, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	vertexDB := prefixdb.New(VertexDBPrefix, prefixDB)
	vertexBootstrappingDB := prefixdb.New(VertexBootstrappingDBPrefix, prefixDB)
	txBootstrappingDB := prefixdb.New(TxBootstrappingDBPrefix, prefixDB)
	blockBootstrappingDB := prefixdb.New(BlockBootstrappingDBPrefix, prefixDB)

	avalancheMetrics, err := metrics.MakeAndRegister(
		m.avalancheGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	vtxBlocker, err := queue.NewWithMissing(vertexBootstrappingDB, "vtx", avalancheMetrics)
	if err != nil {
		return nil, err
	}
	txBlocker, err := queue.New(txBootstrappingDB, "tx", avalancheMetrics)
	if err != nil {
		return nil, err
	}

	// Passes messages from the avalanche engines to the network
	avalancheMessageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_DAG,
		sb,
		avalancheMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche sender: %w", err)
	}

	if m.TracingEnabled {
		avalancheMessageSender = sender.Trace(avalancheMessageSender, m.Tracer)
	}

	// Passes messages from the snowman engines to the network
	snowmanMessageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		sb,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche sender: %w", err)
	}

	if m.TracingEnabled {
		snowmanMessageSender = sender.Trace(snowmanMessageSender, m.Tracer)
	}

	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	dagVM := vm
	if m.MeterVMEnabled {
		meterdagvmReg, err := metrics.MakeAndRegister(
			m.meterDAGVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		dagVM = metervm.NewVertexVM(dagVM, meterdagvmReg)
	}
	if m.TracingEnabled {
		dagVM = tracedvm.NewVertexVM(dagVM, m.Tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := state.NewSerializer(
		state.SerializerConfig{
			ChainID: ctx.ChainID,
			VM:      dagVM,
			DB:      vertexDB,
			Log:     ctx.Log,
		},
	)

	// The only difference between using avalancheMessageSender and
	// snowmanMessageSender here is where the metrics will be placed. Because we
	// end up using this sender after the linearization, we pass in
	// snowmanMessageSender here.
	err = dagVM.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		fxs,
		snowmanMessageSender,
	)
	if err != nil {
		return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	var (
		// A default subnet configuration will be present if explicit configuration is not provided
		subnetCfg           = m.SubnetConfigs[ctx.SubnetID]
		minBlockDelay       = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	)
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.Upgrades.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.Upgrades.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	// Note: this does not use [dagVM] to ensure we use the [vm]'s height index.
	untracedVMWrappedInsideProposerVM := NewLinearizeOnInitializeVM(vm)

	var vmWrappedInsideProposerVM block.ChainVM = untracedVMWrappedInsideProposerVM
	if m.TracingEnabled {
		vmWrappedInsideProposerVM = tracedvm.NewBlockVM(vmWrappedInsideProposerVM, primaryAlias, m.Tracer)
	}

	proposervmReg, err := metrics.MakeAndRegister(
		m.proposervmGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	proposerVM := proposervm.New(
		vmWrappedInsideProposerVM,
		proposervm.Config{
			Upgrades:            m.Upgrades,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.StakingTLSSigner,
			StakingCertLeaf:     m.StakingTLSCert,
			Registerer:          proposervmReg,
		},
	)

	// Note: vmWrappingProposerVM is the VM that the Snowman engines should be
	// using.
	var vmWrappingProposerVM block.ChainVM = proposerVM

	if m.MeterVMEnabled {
		meterchainvmReg, err := metrics.MakeAndRegister(
			m.meterChainVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		vmWrappingProposerVM = metervm.NewBlockVM(vmWrappingProposerVM, meterchainvmReg)
	}
	if m.TracingEnabled {
		vmWrappingProposerVM = tracedvm.NewBlockVM(vmWrappingProposerVM, "proposervm", m.Tracer)
	}

	cn := &block.ChangeNotifier{
		ChainVM: vmWrappingProposerVM,
	}

	vmWrappingProposerVM = cn

	// Note: linearizableVM is the VM that the Avalanche engines should be
	// using.
	linearizableVM := &initializeOnLinearizeVM{
		waitForLinearize: make(chan struct{}),
		DAGVM:            dagVM,
		vmToInitialize:   vmWrappingProposerVM,
		vmToLinearize:    untracedVMWrappedInsideProposerVM,

		ctx:          ctx.Context,
		db:           vmDB,
		genesisBytes: genesisData,
		upgradeBytes: chainConfig.Upgrade,
		configBytes:  chainConfig.Config,
		fxs:          fxs,
		appSender:    snowmanMessageSender,
	}

	bootstrapWeight, err := vdrs.TotalWeight(ctx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for subnet %s: %w", ctx.SubnetID, err)
	}

	consensusParams := *sb.Config().ConsensusConfig.SnowballParams
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	stakeReg, err := metrics.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	vdrs.RegisterSetCallbackListener(ctx.SubnetID, connectedValidators)

	p2pReg, err := metrics.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	handlerReg, err := metrics.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	var halter common.Halter

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		ctx,
		cn,
		linearizableVM.WaitForEvent,
		vdrs,
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.ResourceTracker,
		sb,
		connectedValidators,
		peerTracker,
		handlerReg,
		halter.Halt,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing network handler: %w", err)
	}

	connectedBeacons := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedBeacons, (3*bootstrapWeight+3)/4)
	vdrs.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)

	snowGetHandler, err := snowgetter.New(
		vmWrappingProposerVM,
		snowmanMessageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var snowmanConsensus smcon.Consensus = &smcon.Topological{Factory: snowball.SnowflakeFactory}
	if m.TracingEnabled {
		snowmanConsensus = smcon.Trace(snowmanConsensus, m.Tracer)
	}

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	snowmanEngineConfig := smeng.Config{
		Ctx:                 ctx,
		AllGetsServer:       snowGetHandler,
		VM:                  vmWrappingProposerVM,
		Sender:              snowmanMessageSender,
		Validators:          vdrs,
		ConnectedValidators: connectedValidators,
		Params:              consensusParams,
		Consensus:           snowmanConsensus,
	}
	var snowmanEngine common.Engine
	snowmanEngine, err = smeng.New(snowmanEngineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	if m.TracingEnabled {
		snowmanEngine = common.TraceEngine(snowmanEngine, m.Tracer)
	}

	// create bootstrap gear
	bootstrapCfg := smbootstrap.Config{
		Haltable:                       &halter,
		NonVerifyingParse:              block.ParseFunc(proposerVM.ParseLocalBlock),
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         snowmanMessageSender,
		BootstrapTracker:               sb,
		PeerTracker:                    peerTracker,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		DB:                             blockBootstrappingDB,
		VM:                             vmWrappingProposerVM,
	}
	var snowmanBootstrapper common.BootstrapableEngine
	snowmanBootstrapper, err = smbootstrap.New(
		bootstrapCfg,
		snowmanEngine.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		snowmanBootstrapper = common.TraceBootstrapableEngine(snowmanBootstrapper, m.Tracer)
	}

	avaGetHandler, err := avagetter.New(
		vtxManager,
		avalancheMessageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		avalancheMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche base message handler: %w", err)
	}

	// create engine gear
	avalancheEngine := aveng.New(ctx, avaGetHandler)
	if m.TracingEnabled {
		avalancheEngine = common.TraceEngine(avalancheEngine, m.Tracer)
	}

	// create bootstrap gear
	avalancheBootstrapperConfig := avbootstrap.Config{
		AllGetsServer:                  avaGetHandler,
		Ctx:                            ctx,
		StartupTracker:                 startupTracker,
		Sender:                         avalancheMessageSender,
		PeerTracker:                    peerTracker,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		VtxBlocked:                     vtxBlocker,
		TxBlocked:                      txBlocker,
		Manager:                        vtxManager,
		VM:                             linearizableVM,
		Haltable:                       &halter,
	}
	if ctx.ChainID == m.XChainID {
		avalancheBootstrapperConfig.StopVertexID = m.Upgrades.CortinaXChainStopVertexID
	}

	var avalancheBootstrapper common.BootstrapableEngine
	avalancheBootstrapper, err = avbootstrap.New(
		avalancheBootstrapperConfig,
		snowmanBootstrapper.Start,
		avalancheMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing avalanche bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		avalancheBootstrapper = common.TraceBootstrapableEngine(avalancheBootstrapper, m.Tracer)
	}

	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: avalancheBootstrapper,
			Consensus:    avalancheEngine,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: snowmanBootstrapper,
			Consensus:    snowmanEngine,
		},
	})

	// Register health check for this chain
	if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	}

	return &chain{
		Name:    primaryAlias,
		Context: ctx,
		VM:      dagVM,
		Handler: h,
	}, nil
}

// Create a linear chain using the Snowman consensus engine
func (m *manager) createSnowmanChain(
	ctx *snow.ConsensusContext,
	genesisData []byte,
	vdrs validators.Manager,
	beacons validators.Manager,
	vm block.ChainVM,
	fxs []*common.Fx,
	sb subnets.Subnet,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Initializing,
	})

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	meterDBReg, err := metrics.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	meterDB, err := meterdb.New(meterDBReg, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	bootstrappingDB := prefixdb.New(ChainBootstrappingDBPrefix, prefixDB)

	// Passes messages from the consensus engine to the network
	messageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		sb,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize sender: %w", err)
	}

	if m.TracingEnabled {
		messageSender = sender.Trace(messageSender, m.Tracer)
	}

	var bootstrapFunc func()
	// If [m.validatorState] is nil then we are creating the P-Chain. Since the
	// P-Chain is the first chain to be created, we can use it to initialize
	// required interfaces for the other chains
	if m.validatorState == nil {
		valState, ok := vm.(validators.State)
		if !ok {
			return nil, fmt.Errorf("expected validators.State but got %T", vm)
		}

		if m.TracingEnabled {
			valState = validators.Trace(valState, "platformvm", m.Tracer)
		}

		// Notice that this context is left unlocked. This is because the
		// lock will already be held when accessing these values on the
		// P-chain.
		ctx.ValidatorState = valState

		// Initialize the validator state for future chains.
		m.validatorState = validators.NewLockedState(&ctx.Lock, valState)
		if m.TracingEnabled {
			m.validatorState = validators.Trace(m.validatorState, "lockedState", m.Tracer)
		}

		if !m.ManagerConfig.SybilProtectionEnabled {
			m.validatorState = validators.NewNoValidatorsState(m.validatorState)
			ctx.ValidatorState = validators.NewNoValidatorsState(ctx.ValidatorState)
		}

		// Set this func only for platform
		//
		// The snowman bootstrapper ensures this function is only executed once, so
		// we don't need to be concerned about closing this channel multiple times.
		bootstrapFunc = func() {
			close(m.unblockChainCreatorCh)
		}
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var (
		// A default subnet configuration will be present if explicit configuration is not provided
		subnetCfg           = m.SubnetConfigs[ctx.SubnetID]
		minBlockDelay       = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	)
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.Upgrades.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.Upgrades.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, primaryAlias, m.Tracer)
	}

	proposervmReg, err := metrics.MakeAndRegister(
		m.proposervmGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	proposerVM := proposervm.New(
		vm,
		proposervm.Config{
			Upgrades:            m.Upgrades,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.StakingTLSSigner,
			StakingCertLeaf:     m.StakingTLSCert,
			Registerer:          proposervmReg,
		},
	)

	vm = proposerVM

	if m.MeterVMEnabled {
		meterchainvmReg, err := metrics.MakeAndRegister(
			m.meterChainVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		vm = metervm.NewBlockVM(vm, meterchainvmReg)
	}
	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, "proposervm", m.Tracer)
	}

	cn := &block.ChangeNotifier{
		ChainVM: vm,
	}
	vm = cn

	if err := vm.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		fxs,
		messageSender,
	); err != nil {
		return nil, err
	}

	bootstrapWeight, err := beacons.TotalWeight(ctx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for subnet %s: %w", ctx.SubnetID, err)
	}

	consensusParams := *sb.Config().ConsensusConfig.SnowballParams
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	stakeReg, err := metrics.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	vdrs.RegisterSetCallbackListener(ctx.SubnetID, connectedValidators)

	p2pReg, err := metrics.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	handlerReg, err := metrics.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	var halter common.Halter

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		ctx,
		cn,
		vm.WaitForEvent,
		vdrs,
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.ResourceTracker,
		sb,
		connectedValidators,
		peerTracker,
		handlerReg,
		halter.Halt,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize message handler: %w", err)
	}

	connectedBeacons := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedBeacons, (3*bootstrapWeight+3)/4)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)

	snowGetHandler, err := snowgetter.New(
		vm,
		messageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var consensus smcon.Consensus = &smcon.Topological{Factory: snowball.SnowflakeFactory}
	if m.TracingEnabled {
		consensus = smcon.Trace(consensus, m.Tracer)
	}

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	engineConfig := smeng.Config{
		Ctx:                 ctx,
		AllGetsServer:       snowGetHandler,
		VM:                  vm,
		Sender:              messageSender,
		Validators:          vdrs,
		ConnectedValidators: connectedValidators,
		Params:              consensusParams,
		Consensus:           consensus,
		PartialSync:         m.PartialSyncPrimaryNetwork && ctx.ChainID == constants.PlatformChainID,
	}
	var engine common.Engine
	engine, err = smeng.New(engineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	if m.TracingEnabled {
		engine = common.TraceEngine(engine, m.Tracer)
	}

	// create bootstrap gear
	bootstrapCfg := smbootstrap.Config{
		Haltable:                       &halter,
		NonVerifyingParse:              block.ParseFunc(proposerVM.ParseLocalBlock),
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        beacons,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         messageSender,
		BootstrapTracker:               sb,
		PeerTracker:                    peerTracker,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		DB:                             bootstrappingDB,
		VM:                             vm,
		Bootstrapped:                   bootstrapFunc,
	}
	var bootstrapper common.BootstrapableEngine
	bootstrapper, err = smbootstrap.New(
		bootstrapCfg,
		engine.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		bootstrapper = common.TraceBootstrapableEngine(bootstrapper, m.Tracer)
	}

	// create state sync gear
	stateSyncCfg, err := syncer.NewConfig(
		snowGetHandler,
		ctx,
		startupTracker,
		messageSender,
		beacons,
		sampleK,
		bootstrapWeight/2+1, // must be > 50%
		m.StateSyncBeacons,
		vm,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize state syncer configuration: %w", err)
	}
	stateSyncer := syncer.New(
		stateSyncCfg,
		bootstrapper.Start,
	)

	if m.TracingEnabled {
		stateSyncer = common.TraceStateSyncer(stateSyncer, m.Tracer)
	}

	h.SetEngineManager(&handler.EngineManager{
		DAG: nil,
		Chain: &handler.Engine{
			StateSyncer:  stateSyncer,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	// Register health checks
	if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	}

	return &chain{
		Name:    primaryAlias,
		Context: ctx,
		VM:      vm,
		Handler: h,
	}, nil
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	chain, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	return chain.Context().State.Get().State == snow.NormalOp
}

func (m *manager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		if subnetIDs := m.Subnets.Bootstrapping(); len(subnetIDs) != 0 {
			return subnetIDs, errNotBootstrapped
		}
		return []ids.ID{}, nil
	})
	if err := m.Health.RegisterReadinessCheck("bootstrapped", bootstrappedCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register bootstrapped readiness check: %w", err)
	}
	if err := m.Health.RegisterHealthCheck("bootstrapped", bootstrappedCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register bootstrapped health check: %w", err)
	}

	// We should only report unhealthy if the node is partially syncing the
	// primary network and is a validator.
	if !m.PartialSyncPrimaryNetwork {
		return nil
	}

	partialSyncCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		// Note: The health check is skipped during bootstrapping to allow a
		// node to sync the network even if it was previously a validator.
		if !m.IsBootstrapped(constants.PlatformChainID) {
			return "node is currently bootstrapping", nil
		}
		if _, ok := m.Validators.GetValidator(constants.PrimaryNetworkID, m.NodeID); !ok {
			return "node is not a primary network validator", nil
		}

		m.Log.Warn("node is a primary network validator",
			zap.Error(errPartialSyncAsAValidator),
		)
		return "node is a primary network validator", errPartialSyncAsAValidator
	})

	if err := m.Health.RegisterHealthCheck("validation", partialSyncCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register validation health check: %w", err)
	}
	return nil
}

// Starts chain creation loop to process queued chains
func (m *manager) StartChainCreator(platformParams ChainParameters) error {
	// Add the P-Chain to the Primary Network
	sb, _ := m.Subnets.GetOrCreate(constants.PrimaryNetworkID)
	sb.AddChain(platformParams.ID)

	// The P-chain is created synchronously to ensure that `VM.Initialize` has
	// finished before returning from this function. This is required because
	// the P-chain initializes state that the rest of the node initialization
	// depends on.
	m.createChain(platformParams)

	m.Log.Info("starting chain creator")
	m.chainCreatorExited.Add(1)
	go m.dispatchChainCreator()
	return nil
}

func (m *manager) dispatchChainCreator() {
	defer m.chainCreatorExited.Done()

	select {
	// This channel will be closed when Shutdown is called on the manager.
	case <-m.chainCreatorShutdownCh:
		return
	case <-m.unblockChainCreatorCh:
	}

	// Handle chain creations
	for {
		// Get the next chain we should create.
		// Dequeue waits until an element is pushed, so this is not
		// busy-looping.
		chainParams, ok := m.chainsQueue.PopLeft()
		if !ok { // queue is closed, return directly
			return
		}
		m.createChain(chainParams)
	}
}

// Shutdown stops all the chains
func (m *manager) Shutdown() {
	m.Log.Info("shutting down chain manager")
	m.chainsQueue.Close()
	close(m.chainCreatorShutdownCh)
	m.chainCreatorExited.Wait()
	m.ManagerConfig.Router.Shutdown(context.TODO())
}

// LookupVM returns the ID of the VM associated with an alias
func (m *manager) LookupVM(alias string) (ids.ID, error) {
	return m.VMManager.Lookup(alias)
}

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *manager) notifyRegistrants(name string, ctx *snow.ConsensusContext, vm common.VM) {
	for _, registrant := range m.registrants {
		registrant.RegisterChain(name, ctx, vm)
	}
}

// getChainConfig returns value of a entry by looking at ID key and alias key
// it first searches ID key, then falls back to it's corresponding primary alias
func (m *manager) getChainConfig(id ids.ID) (ChainConfig, error) {
	if val, ok := m.ManagerConfig.ChainConfigs[id.String()]; ok {
		return val, nil
	}
	aliases, err := m.Aliases(id)
	if err != nil {
		return ChainConfig{}, err
	}
	for _, alias := range aliases {
		if val, ok := m.ManagerConfig.ChainConfigs[alias]; ok {
			return val, nil
		}
	}

	return ChainConfig{}, nil
}

func (m *manager) getOrMakeVMGatherer(vmID ids.ID) (metrics.MultiGatherer, error) {
	vmGatherer, ok := m.vmGatherer[vmID]
	if ok {
		return vmGatherer, nil
	}

	vmName := constants.VMName(vmID)
	vmNamespace := metric.AppendNamespace(constants.PlatformName, vmName)
	vmGatherer = metrics.NewLabelGatherer(ChainLabel)
	err := m.Metrics.Register(
		vmNamespace,
		vmGatherer,
	)
	if err != nil {
		return nil, err
	}
	m.vmGatherer[vmID] = vmGatherer
	return vmGatherer, nil
}

// createSimplexHandler creates a handler that passes messages from the network to the consensus engine
func (m *manager) createSimplexHandler(ctx *snow.ConsensusContext, vm block.ChainVM, sb subnets.Subnet, primaryAlias string, connectedValidators tracker.Peers, peerTracker *p2p.PeerTracker, halter common.Halter) (handler.Handler, error) {
	handlerReg, err := metrics.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Asynchronously passes messages from the network to the consensus engine
	return handler.New(
		ctx,
		nil, // we don't need a change notifier for simplex, since the engine listens for events and we don't want the handler to intercept them
		vm.WaitForEvent,
		m.Validators,
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.ResourceTracker,
		sb,
		connectedValidators,
		peerTracker,
		handlerReg,
		halter.Halt,
	)
}

// createMessageSender creates a sender that passes messages from the consensus engine to the network
func (m *manager) createMessageSender(ctx *snow.ConsensusContext, sb subnets.Subnet) (common.Sender, error) {
	msgSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		sb,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize sender: %w", err)
	}

	if m.TracingEnabled {
		msgSender = sender.Trace(msgSender, m.Tracer)
	}

	return msgSender, nil
}

func (m *manager) createTrackedPeers(primaryAlias string) (tracker.Peers, error) {
	stakeReg, err := metrics.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}
	connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	return connectedValidators, nil
}

// createPeerTracker creates a peer tracker for the Snowman consensus engine
func (m *manager) createPeerTracker(ctx *snow.ConsensusContext, primaryAlias string) (*p2p.PeerTracker, error) {
	p2pReg, err := metrics.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	return peerTracker, nil
}

func simplexCtxConfig(ctx *snow.ConsensusContext) simplex.SimplexChainContext {
	return simplex.SimplexChainContext{
		NodeID:    ctx.NodeID,
		ChainID:   ctx.ChainID,
		SubnetID:  ctx.SubnetID,
		NetworkID: ctx.NetworkID,
	}
}

func (m *manager) createSimplexChain(ctx *snow.ConsensusContext, vm block.ChainVM, sb subnets.Subnet, genesisData []byte, fxs []*common.Fx) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Initializing,
	})

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	m.Log.Info("creating simplex chain", zap.String("chain", primaryAlias))

	messageSender, err := m.createMessageSender(ctx, sb)
	if err != nil {
		return nil, fmt.Errorf("couldn't create snowman message sender: %w", err)
	}

	// initialize the VM
	vmDB, simplexDB, err := m.createSimplexDBs(primaryAlias, ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("couldn't create simplex DBs: %w", err)
	}

	// TODO: wrap the vm in a LOCKED VM that locks the VM interface methods

	var halter common.Halter
	connectedValidators, err := m.createTrackedPeers(primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error creating connected validators: %w", err)
	}
	m.Validators.RegisterSetCallbackListener(ctx.SubnetID, connectedValidators)

	peerTracker, err := m.createPeerTracker(ctx, primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracking: %w", err)
	}

	h, err := m.createSimplexHandler(ctx, vm, sb, primaryAlias, connectedValidators, peerTracker, halter)
	if err != nil {
		return nil, fmt.Errorf("error creating handler: %w", err)
	}

	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	// we need to set initialize the VM before creating the simplex engine
	// in order to have the genesis data available
	if err := vm.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		fxs,
		messageSender,
	); err != nil {
		return nil, fmt.Errorf("couldn't initialize simplex VM: %w", err)
	}

	config := &simplex.Config{
		Ctx:                simplexCtxConfig(ctx),
		Log:                ctx.Log,
		Sender:             m.Net,
		OutboundMsgBuilder: m.MsgCreator,
		Validators:         m.Validators.GetMap(ctx.SubnetID),
		VM:                 vm,
		WALLocation:        getChainWALLocation(ctx.ChainDataDir, ctx.ChainID),
		SignBLS:            m.ManagerConfig.StakingBLSKey.Sign,
		DB:                 simplexDB,
	}

	engine, err := simplex.NewEngine(ctx, context.TODO(), config)
	if err != nil {
		return nil, fmt.Errorf("couldn't create simplex engine: %w", err)
	}

	bootstrapper := &simplex.TODOBootstrapper{
		Log:    ctx.Log,
		Engine: engine,
	}

	h.SetEngineManager(&handler.EngineManager{
		DAG: nil,
		Chain: &handler.Engine{
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	// Register health checks
	if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	}

	return &chain{
		Name:    primaryAlias,
		Context: ctx,
		VM:      vm,
		Handler: h,
	}, nil
}

// createSimplexDBs creates dbs for simplex. One is used for the VM to store blocks,
// the other is used for simplex to store finalizations
func (m *manager) createSimplexDBs(primaryAlias string, chainID ids.ID) (*prefixdb.Database, *prefixdb.Database, error) {
	meterDBReg, err := metrics.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, nil, err
	}

	meterDB, err := meterdb.New(meterDBReg, m.DB)
	if err != nil {
		return nil, nil, err
	}

	prefixDB := prefixdb.New(chainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	simplexDB := prefixdb.New(simplexDBPrefix, prefixDB)
	return vmDB, simplexDB, nil
}

func getChainWALLocation(chainDataDir string, chainID ids.ID) string {
	return filepath.Join(
		chainDataDir,
		chainID.String()+".wal",
	)
}
