// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/state"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/syncer"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/fx"
	"github.com/ava-labs/avalanchego/vms/metervm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/proposervm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/tracedvm"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	aveng "github.com/ava-labs/avalanchego/snow/engine/avalanche"
	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
	avagetter "github.com/ava-labs/avalanchego/snow/engine/avalanche/getter"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	smbootstrap "github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
)

const (
	defaultChannelSize = 1
	initialQueueSize   = 3
)

var (
	// Commonly shared VM DB prefix
	VMDBPrefix = []byte("vm")

	// Bootstrapping prefixes for LinearizableVMs
	VertexDBPrefix              = []byte("vertex")
	VertexBootstrappingDBPrefix = []byte("vertex_bs")
	TxBootstrappingDBPrefix     = []byte("tx_bs")
	BlockBootstrappingDBPrefix  = []byte("block_bs")

	// Bootstrapping prefixes for ChainVMs
	ChainBootstrappingDBPrefix = []byte("bs")

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

	// AddChain adds a chain that is running on this node. The provided chain
	// must be a chain in a tracked subnet.
	AddChain(chain *Chain)

	// Add a registrant [r]. Every time a chain is
	// created, [r].RegisterChain([new chain]) is called.
	AddRegistrant(Registrant)

	// Given an alias, return the ID of the chain associated with that alias
	Lookup(string) (ids.ID, error)

	// Given an alias, return the ID of the VM associated with that alias
	LookupVM(string) (ids.ID, error)

	// Returns true iff the chain with the given ID exists and is finished bootstrapping
	IsBootstrapped(ids.ID) bool

	// Starts the chain creator. Must be called once.
	StartChainCreator() error

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
	// The validator set the VM uses
	Validators validators.State
}

type Chain struct {
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
	Log                       logging.Logger
	VMManager                 vms.Manager        // Manage mappings from vm ID --> vm
	Router                    router.Router      // Routes incoming messages to the appropriate chain
	Validators                validators.Manager // Validators validating on this chain
	NodeID                    ids.NodeID         // The ID of this node
	PartialSyncPrimaryNetwork bool
	CriticalChains            set.Set[ids.ID] // Chains that can't exit gracefully
	Health                    health.Registerer
	SubnetConfigs             map[ids.ID]subnets.Config // ID -> SubnetConfig
	ChainConfigs              map[string]ChainConfig    // alias -> ChainConfig
	// ShutdownNodeFunc allows the chain manager to issue a request to shutdown the node
	ShutdownNodeFunc func(exitCode int)
	Aliaser          ids.Aliaser
	Subnets          *Subnets
	Factory          *Factory
	// unblocks chain creator to start processing the queue
	UnblockChainCreatorCh chan struct{}
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
	// shutdown the chain creator goroutine if the queue hasn't started to be
	// processed.
	chainCreatorShutdownCh chan struct{}
	chainCreatorExited     sync.WaitGroup

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]handler.Handler
}

// New returns a new Manager
func New(config *ManagerConfig) Manager {
	return &manager{
		Aliaser:                config.Aliaser,
		ManagerConfig:          *config,
		chains:                 make(map[ids.ID]handler.Handler),
		chainsQueue:            buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize),
		chainCreatorShutdownCh: make(chan struct{}),
	}
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

// createChain creates the chain
//
// Note: it is expected for the subnet to already have the chain registered as
// bootstrapping before this function is called
func (m *manager) createChain(chainParams ChainParameters) (*Chain, error) {
	m.Log.Info("creating chain",
		zap.Stringer("subnetID", chainParams.SubnetID),
		zap.Stringer("chainID", chainParams.ID),
		zap.Stringer("vmID", chainParams.VMID),
	)

	vmFactory, err := m.VMManager.GetFactory(chainParams.VMID)
	if err != nil {
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}

	return m.Factory.New(
		chainParams.ID,
		chainParams.SubnetID,
		chainParams.VMID,
		chainParams.FxIDs,
		chainParams.GenesisData,
		m.Validators,
		vmFactory,
		chainParams.Validators,
		nil,
		validators.UnhandledSubnetConnector,
	)
}

// AddChain adds a chain that this node is running
// Invariant: Tracked Subnet must be checked before calling this function
func (m *manager) AddChain(chain *Chain) {
	m.chainsLock.Lock()
	m.chains[chain.Context.ChainID] = chain.Handler
	m.chainsLock.Unlock()

	// Associate the newly created chain with its default alias
	if err := m.Alias(chain.Context.ChainID, chain.Context.ChainID.String()); err != nil {
		m.Log.Error("failed to alias the new chain with itself",
			zap.Stringer("subnetID", chain.Context.SubnetID),
			zap.Stringer("chainID", chain.Context.ChainID),
			zap.Error(err),
		)
	}

	// Notify those that registered to be notified when a new chain is created
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
	if chain.Context.ChainID == constants.PlatformChainID {
		if err := m.registerBootstrappedHealthChecks(); err != nil {
			chain.Handler.StopWithError(context.TODO(), err)
		}
	}

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	chain.Handler.Start(context.TODO(), !m.CriticalChains.Contains(chain.Context.ChainID))
}

// New returns a new chain
func (f *Factory) New(
	chainID ids.ID,
	subnetID ids.ID,
	vmID ids.ID,
	fxIDs []ids.ID,
	genesis []byte,
	beacons validators.Manager,
	vmFactory vms.Factory,
	validators validators.State,
	bootstrapFunc func(),
	subnetConnector validators.SubnetConnector,
) (*Chain, error) {
	if chainID != constants.PlatformChainID && vmID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}

	sb, _ := f.subnets.GetOrCreate(subnetID)
	primaryAlias := f.aliaser.PrimaryAliasOrDefault(chainID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(f.config.ChainDataDir, chainID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog, err := f.logFactory.MakeChain(primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error while creating chain's log %w", err)
	}

	consensusMetrics := prometheus.NewRegistry()
	chainNamespace := metric.AppendNamespace(constants.PlatformName, primaryAlias)
	if err := f.metrics.Register(chainNamespace, consensusMetrics); err != nil {
		return nil, fmt.Errorf("error while registering chain's metrics %w", err)
	}

	// This converts the prefix for all the Avalanche consensus metrics from
	// `avalanche_{chainID}_` into `avalanche_{chainID}_avalanche_` so that
	// there are no conflicts when registering the Snowman consensus metrics.
	avalancheConsensusMetrics := prometheus.NewRegistry()
	avalancheDAGNamespace := metric.AppendNamespace(chainNamespace, "avalanche")
	if err := f.metrics.Register(avalancheDAGNamespace, avalancheConsensusMetrics); err != nil {
		return nil, fmt.Errorf("error while registering DAG metrics %w", err)
	}

	vmMetrics := metrics.NewOptionalGatherer()
	vmNamespace := metric.AppendNamespace(chainNamespace, "vm")
	if err := f.metrics.Register(vmNamespace, vmMetrics); err != nil {
		return nil, fmt.Errorf("error while registering vm's metrics %w", err)
	}

	ctx := &snow.ConsensusContext{
		Context: &snow.Context{
			NetworkID: f.config.NetworkID,
			SubnetID:  subnetID,
			ChainID:   chainID,
			NodeID:    f.nodeID,
			PublicKey: bls.PublicFromSecretKey(f.stakingBLSKey),

			XChainID:    f.xChainID,
			CChainID:    f.cChainID,
			AVAXAssetID: f.config.AvaxAssetID,

			Log:          chainLog,
			Keystore:     f.keystore.NewBlockchainKeyStore(chainID),
			SharedMemory: f.atomicMemory.NewSharedMemory(chainID),
			BCLookup:     f.aliaser,
			Metrics:      vmMetrics,

			WarpSigner: warp.NewSigner(f.stakingBLSKey, f.config.NetworkID, chainID),

			ValidatorState: validators,
			ChainDataDir:   chainDataDir,
		},
		BlockAcceptor:       f.blockAcceptorGroup,
		TxAcceptor:          f.txAcceptorGroup,
		VertexAcceptor:      f.vertexAcceptorGroup,
		Registerer:          consensusMetrics,
		AvalancheRegisterer: avalancheConsensusMetrics,
	}

	// Create the chain
	vm, err := vmFactory.New(chainLog)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm: %w", err)
	}
	// TODO: Shutdown VM if an error occurs

	chainFxs := make([]*common.Fx, len(fxIDs))
	for i, fxID := range fxIDs {
		fxFactory, ok := fxs[fxID]
		if !ok {
			return nil, fmt.Errorf("fx %s not found", fxID)
		}

		chainFxs[i] = &common.Fx{
			ID: fxID,
			Fx: fxFactory.New(),
		}
	}

	var chain *Chain
	switch vm := vm.(type) {
	case vertex.LinearizableVMWithEngine:
		chain, err = f.createAvalancheChain(
			ctx,
			genesis,
			f.validators,
			vm,
			chainFxs,
			sb,
			bootstrapFunc,
			subnetConnector,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new avalanche vm %w", err)
		}
	case block.ChainVM:
		chain, err = f.createSnowmanChain(
			ctx,
			genesis,
			f.validators,
			beacons,
			vm,
			chainFxs,
			sb,
			bootstrapFunc,
			subnetConnector,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new snowman vm %w", err)
		}
	default:
		return nil, errUnknownVMType
	}

	// Register the chain with the timeout manager
	if err := f.timeoutManager.RegisterChain(ctx); err != nil {
		return nil, err
	}

	return chain, nil
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// Create a DAG-based blockchain that uses Avalanche
func (f *Factory) createAvalancheChain(
	ctx *snow.ConsensusContext,
	genesisData []byte,
	vdrs validators.Manager,
	vm vertex.LinearizableVMWithEngine,
	fxs []*common.Fx,
	sb subnets.Subnet,
	bootstrapFunc func(),
	subnetConnector validators.SubnetConnector,
) (*Chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.Initializing,
	})

	meterDB, err := meterdb.New("db", ctx.Registerer, f.db)
	if err != nil {
		return nil, err
	}
	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	vertexDB := prefixdb.New(VertexDBPrefix, prefixDB)
	vertexBootstrappingDB := prefixdb.New(VertexBootstrappingDBPrefix, prefixDB)
	txBootstrappingDB := prefixdb.New(TxBootstrappingDBPrefix, prefixDB)
	blockBootstrappingDB := prefixdb.New(BlockBootstrappingDBPrefix, prefixDB)

	vtxBlocker, err := queue.NewWithMissing(vertexBootstrappingDB, "vtx", ctx.AvalancheRegisterer)
	if err != nil {
		return nil, err
	}
	txBlocker, err := queue.New(txBootstrappingDB, "tx", ctx.AvalancheRegisterer)
	if err != nil {
		return nil, err
	}
	blockBlocker, err := queue.NewWithMissing(blockBootstrappingDB, "block", ctx.Registerer)
	if err != nil {
		return nil, err
	}

	// Passes messages from the avalanche engines to the network
	avalancheMessageSender, err := sender.New(
		ctx,
		f.msgCreator,
		f.net,
		f.router,
		f.timeoutManager,
		p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche sender: %w", err)
	}

	if f.config.TracingEnabled {
		avalancheMessageSender = sender.Trace(avalancheMessageSender, f.tracer)
	}

	err = f.vertexAcceptorGroup.RegisterAcceptor(
		ctx.ChainID,
		"gossip",
		avalancheMessageSender,
		false,
	)
	if err != nil { // Set up the event dispatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	// Passes messages from the snowman engines to the network
	snowmanMessageSender, err := sender.New(
		ctx,
		f.msgCreator,
		f.net,
		f.router,
		f.timeoutManager,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche sender: %w", err)
	}

	if f.config.TracingEnabled {
		snowmanMessageSender = sender.Trace(snowmanMessageSender, f.tracer)
	}

	err = f.blockAcceptorGroup.RegisterAcceptor(
		ctx.ChainID,
		"gossip",
		snowmanMessageSender,
		false,
	)
	if err != nil { // Set up the event dispatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	chainConfig, err := f.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	dagVM := vm
	if f.config.MeterVMEnabled {
		dagVM = metervm.NewVertexVM(dagVM)
	}
	if f.config.TracingEnabled {
		dagVM = tracedvm.NewVertexVM(dagVM, f.tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := state.NewSerializer(
		state.SerializerConfig{
			ChainID:     ctx.ChainID,
			VM:          dagVM,
			DB:          vertexDB,
			Log:         ctx.Log,
			CortinaTime: version.GetCortinaTime(ctx.NetworkID),
		},
	)

	avalancheRegisterer := metrics.NewOptionalGatherer()
	snowmanRegisterer := metrics.NewOptionalGatherer()

	registerer := metrics.NewMultiGatherer()
	if err := registerer.Register("avalanche", avalancheRegisterer); err != nil {
		return nil, err
	}
	if err := registerer.Register("", snowmanRegisterer); err != nil {
		return nil, err
	}
	if err := ctx.Context.Metrics.Register(registerer); err != nil {
		return nil, err
	}

	ctx.Context.Metrics = avalancheRegisterer

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

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
		msgChan,
		fxs,
		snowmanMessageSender,
	)
	if err != nil {
		return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	if subnetCfg, ok := f.config.SubnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	f.log.Info("creating proposervm wrapper",
		zap.Time("activationTime", f.config.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", f.config.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	chainAlias := f.aliaser.PrimaryAliasOrDefault(ctx.ChainID)

	// Note: this does not use [dagVM] to ensure we use the [vm]'s height index.
	untracedVMWrappedInsideProposerVM := NewLinearizeOnInitializeVM(vm)

	var vmWrappedInsideProposerVM block.ChainVM = untracedVMWrappedInsideProposerVM
	if f.config.TracingEnabled {
		vmWrappedInsideProposerVM = tracedvm.NewBlockVM(vmWrappedInsideProposerVM, chainAlias, f.tracer)
	}

	// Note: vmWrappingProposerVM is the VM that the Snowman engines should be
	// using.
	var vmWrappingProposerVM block.ChainVM = proposervm.New(
		vmWrappedInsideProposerVM,
		proposervm.Config{
			ActivationTime:      f.config.ApricotPhase4Time,
			DurangoTime:         version.GetDurangoTime(f.config.NetworkID),
			MinimumPChainHeight: f.config.ApricotPhase4MinPChainHeight,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   f.stakingSigner,
			StakingCertLeaf:     f.stakingCert,
		},
	)

	if f.config.MeterVMEnabled {
		vmWrappingProposerVM = metervm.NewBlockVM(vmWrappingProposerVM)
	}
	if f.config.TracingEnabled {
		vmWrappingProposerVM = tracedvm.NewBlockVM(vmWrappingProposerVM, "proposervm", f.tracer)
	}

	// Note: linearizableVM is the VM that the Avalanche engines should be
	// using.
	linearizableVM := &initializeOnLinearizeVM{
		DAGVM:          dagVM,
		vmToInitialize: vmWrappingProposerVM,
		vmToLinearize:  untracedVMWrappedInsideProposerVM,

		registerer:   snowmanRegisterer,
		ctx:          ctx.Context,
		db:           vmDB,
		genesisBytes: genesisData,
		upgradeBytes: chainConfig.Upgrade,
		configBytes:  chainConfig.Config,
		toEngine:     msgChan,
		fxs:          fxs,
		appSender:    snowmanMessageSender,
	}

	bootstrapWeight, err := vdrs.TotalWeight(ctx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for subnet %s: %w", ctx.SubnetID, err)
	}

	consensusParams := sb.Config().ConsensusParameters
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	connectedValidators, err := tracker.NewMeteredPeers("", ctx.Registerer)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	vdrs.RegisterCallbackListener(ctx.SubnetID, connectedValidators)

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		ctx,
		vdrs,
		msgChan,
		f.config.FrontierPollFrequency,
		f.config.ConsensusAppConcurrency,
		f.resourceTracker,
		subnetConnector,
		sb,
		connectedValidators,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing network handler: %w", err)
	}

	connectedBeacons := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedBeacons, (3*bootstrapWeight+3)/4)
	vdrs.RegisterCallbackListener(ctx.SubnetID, startupTracker)

	snowGetHandler, err := snowgetter.New(
		vmWrappingProposerVM,
		snowmanMessageSender,
		ctx.Log,
		f.config.BootstrapMaxTimeGetAncestors,
		f.config.BootstrapAncestorsMaxContainersSent,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var snowmanConsensus smcon.Consensus = &smcon.Topological{}
	if f.config.TracingEnabled {
		snowmanConsensus = smcon.Trace(snowmanConsensus, f.tracer)
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
	snowmanEngine, err := smeng.New(snowmanEngineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	if f.config.TracingEnabled {
		snowmanEngine = smeng.TraceEngine(snowmanEngine, f.tracer)
	}

	// create bootstrap gear
	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         snowmanMessageSender,
		BootstrapTracker:               sb,
		Timer:                          h,
		AncestorsMaxContainersReceived: f.config.BootstrapAncestorsMaxContainersReceived,
		Blocked:                        blockBlocker,
		VM:                             vmWrappingProposerVM,
		Bootstrapped:                   bootstrapFunc,
	}
	var snowmanBootstrapper common.BootstrapableEngine
	snowmanBootstrapper, err = smbootstrap.New(
		bootstrapCfg,
		snowmanEngine.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman bootstrapper: %w", err)
	}

	if f.config.TracingEnabled {
		snowmanBootstrapper = common.TraceBootstrapableEngine(snowmanBootstrapper, f.tracer)
	}

	avaGetHandler, err := avagetter.New(
		vtxManager,
		avalancheMessageSender,
		ctx.Log,
		f.config.BootstrapMaxTimeGetAncestors,
		f.config.BootstrapAncestorsMaxContainersSent,
		ctx.AvalancheRegisterer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche base message handler: %w", err)
	}

	// create engine gear
	avalancheEngine := aveng.New(ctx, avaGetHandler, linearizableVM)
	if f.config.TracingEnabled {
		avalancheEngine = common.TraceEngine(avalancheEngine, f.tracer)
	}

	// create bootstrap gear
	avalancheBootstrapperConfig := avbootstrap.Config{
		AllGetsServer:                  avaGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		StartupTracker:                 startupTracker,
		Sender:                         avalancheMessageSender,
		AncestorsMaxContainersReceived: f.config.BootstrapAncestorsMaxContainersReceived,
		VtxBlocked:                     vtxBlocker,
		TxBlocked:                      txBlocker,
		Manager:                        vtxManager,
		VM:                             linearizableVM,
	}
	if ctx.ChainID == f.xChainID {
		avalancheBootstrapperConfig.StopVertexID = version.CortinaXChainStopVertexID[ctx.NetworkID]
	}

	avalancheBootstrapper, err := avbootstrap.New(
		avalancheBootstrapperConfig,
		snowmanBootstrapper.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing avalanche bootstrapper: %w", err)
	}

	if f.config.TracingEnabled {
		avalancheBootstrapper = common.TraceBootstrapableEngine(avalancheBootstrapper, f.tracer)
	}

	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: avalancheBootstrapper,
			Consensus:    avalancheEngine,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: snowmanBootstrapper,
			Consensus:    snowmanEngine,
		},
	})

	// Register health check for this chain
	if err := f.health.RegisterHealthCheck(chainAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &Chain{
		Name:    chainAlias,
		Context: ctx,
		VM:      dagVM,
		Handler: h,
	}, nil
}

// Create a linear chain using the Snowman consensus engine
func (f *Factory) createSnowmanChain(
	ctx *snow.ConsensusContext,
	genesisData []byte,
	vdrs validators.Manager,
	beacons validators.Manager,
	vm block.ChainVM,
	fxs []*common.Fx,
	sb subnets.Subnet,
	bootstrapFunc func(),
	subnetConnector validators.SubnetConnector,
) (*Chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Initializing,
	})

	meterDB, err := meterdb.New("db", ctx.Registerer, f.db)
	if err != nil {
		return nil, err
	}
	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	bootstrappingDB := prefixdb.New(ChainBootstrappingDBPrefix, prefixDB)

	blocked, err := queue.NewWithMissing(bootstrappingDB, "block", ctx.Registerer)
	if err != nil {
		return nil, err
	}

	// Passes messages from the consensus engine to the network
	messageSender, err := sender.New(
		ctx,
		f.msgCreator,
		f.net,
		f.router,
		f.timeoutManager,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize sender: %w", err)
	}

	if f.config.TracingEnabled {
		messageSender = sender.Trace(messageSender, f.tracer)
	}

	err = f.blockAcceptorGroup.RegisterAcceptor(
		ctx.ChainID,
		"gossip",
		messageSender,
		false,
	)
	if err != nil { // Set up the event dispatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	chainConfig, err := f.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	if subnetCfg, ok := f.config.SubnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	f.log.Info("creating proposervm wrapper",
		zap.Time("activationTime", f.config.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", f.config.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	chainAlias := f.aliaser.PrimaryAliasOrDefault(ctx.ChainID)
	if f.config.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, chainAlias, f.tracer)
	}

	vm = proposervm.New(
		vm,
		proposervm.Config{
			ActivationTime:      f.config.ApricotPhase4Time,
			DurangoTime:         version.GetDurangoTime(f.config.NetworkID),
			MinimumPChainHeight: f.config.ApricotPhase4MinPChainHeight,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   f.stakingSigner,
			StakingCertLeaf:     f.stakingCert,
		},
	)

	if f.config.MeterVMEnabled {
		vm = metervm.NewBlockVM(vm)
	}
	if f.config.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, "proposervm", f.tracer)
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	if err := vm.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		msgChan,
		fxs,
		messageSender,
	); err != nil {
		return nil, err
	}

	bootstrapWeight, err := beacons.TotalWeight(ctx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for subnet %s: %w", ctx.SubnetID, err)
	}

	consensusParams := sb.Config().ConsensusParameters
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	connectedValidators, err := tracker.NewMeteredPeers("", ctx.Registerer)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	vdrs.RegisterCallbackListener(ctx.SubnetID, connectedValidators)

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		ctx,
		vdrs,
		msgChan,
		f.config.FrontierPollFrequency,
		f.config.ConsensusAppConcurrency,
		f.resourceTracker,
		subnetConnector,
		sb,
		connectedValidators,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize message handler: %w", err)
	}

	connectedBeacons := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedBeacons, (3*bootstrapWeight+3)/4)
	beacons.RegisterCallbackListener(ctx.SubnetID, startupTracker)

	snowGetHandler, err := snowgetter.New(
		vm,
		messageSender,
		ctx.Log,
		f.config.BootstrapMaxTimeGetAncestors,
		f.config.BootstrapAncestorsMaxContainersSent,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var consensus smcon.Consensus = &smcon.Topological{}
	if f.config.TracingEnabled {
		consensus = smcon.Trace(consensus, f.tracer)
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
		PartialSync:         f.config.PartialSyncPrimaryNetwork && ctx.ChainID == constants.PlatformChainID,
	}
	engine, err := smeng.New(engineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	if f.config.TracingEnabled {
		engine = smeng.TraceEngine(engine, f.tracer)
	}

	// create bootstrap gear
	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        beacons,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         messageSender,
		BootstrapTracker:               sb,
		Timer:                          h,
		AncestorsMaxContainersReceived: f.config.BootstrapAncestorsMaxContainersReceived,
		Blocked:                        blocked,
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

	if f.config.TracingEnabled {
		bootstrapper = common.TraceBootstrapableEngine(bootstrapper, f.tracer)
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
		f.config.StateSyncBeacons,
		vm,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize state syncer configuration: %w", err)
	}
	stateSyncer := syncer.New(
		stateSyncCfg,
		bootstrapper.Start,
	)

	if f.config.TracingEnabled {
		stateSyncer = common.TraceStateSyncer(stateSyncer, f.tracer)
	}

	h.SetEngineManager(&handler.EngineManager{
		Avalanche: nil,
		Snowman: &handler.Engine{
			StateSyncer:  stateSyncer,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	// Register health checks
	if err := f.health.RegisterHealthCheck(chainAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &Chain{
		Name:    chainAlias,
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
func (m *manager) StartChainCreator() error {
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
	case <-m.ManagerConfig.UnblockChainCreatorCh:
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
		chain, err := m.createChain(chainParams)
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
			continue
		}

		m.AddChain(chain)
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
func (f *Factory) getChainConfig(id ids.ID) (ChainConfig, error) {
	if val, ok := f.chainConfigs[id.String()]; ok {
		return val, nil
	}
	aliases, err := f.aliaser.Aliases(id)
	if err != nil {
		return ChainConfig{}, err
	}
	for _, alias := range aliases {
		if val, ok := f.chainConfigs[alias]; ok {
			return val, nil
		}
	}

	return ChainConfig{}, nil
}
