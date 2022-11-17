// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
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
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/metervm"
	"github.com/ava-labs/avalanchego/vms/proposervm"
	"github.com/ava-labs/avalanchego/vms/tracedvm"

	dbManager "github.com/ava-labs/avalanchego/database/manager"
	timetracker "github.com/ava-labs/avalanchego/snow/networking/tracker"

	avcon "github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	aveng "github.com/ava-labs/avalanchego/snow/engine/avalanche"
	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
	avagetter "github.com/ava-labs/avalanchego/snow/engine/avalanche/getter"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	smbootstrap "github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
)

const (
	defaultChannelSize = 1
	initialQueueSize   = 3
)

var (
	errUnknownChainID   = errors.New("unknown chain ID")
	errUnknownVMType    = errors.New("the vm should have type avalanche.DAGVM or snowman.ChainVM")
	errCreatePlatformVM = errors.New("attempted to create a chain running the PlatformVM")
	errNotBootstrapped  = errors.New("subnets not bootstrapped")

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

	// Return the router this Manager is using to route consensus messages to chains
	Router() router.Router

	// Queues a chain to be created in the future after chain creator is unblocked.
	// This is only called from the P-chain thread to create other chains
	// Queued chains are created only after P-chain is bootstrapped.
	// This assumes only chains in whitelisted subnets are queued.
	QueueChainCreation(ChainParameters)

	// Add a registrant [r]. Every time a chain is
	// created, [r].RegisterChain([new chain]) is called.
	AddRegistrant(Registrant)

	// Given an alias, return the ID of the chain associated with that alias
	Lookup(string) (ids.ID, error)

	// Given an alias, return the ID of the VM associated with that alias
	LookupVM(string) (ids.ID, error)

	// Returns the ID of the subnet that is validating the provided chain
	SubnetID(chainID ids.ID) (ids.ID, error)

	// Returns true iff the chain with the given ID exists and is finished bootstrapping
	IsBootstrapped(ids.ID) bool

	// Starts the chain creator with the initial platform chain parameters, must
	// be called once.
	StartChainCreator(platformChain ChainParameters)

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
	// Should only be set if the default beacons can't be used.
	CustomBeacons validators.Set
}

type chain struct {
	Name    string
	Engine  common.Engine
	Handler handler.Handler
	Beacons validators.Set
}

// ChainConfig is configuration settings for the current execution.
// [Config] is the user-provided config blob for the chain.
// [Upgrade] is a chain-specific blob for coordinating upgrades.
type ChainConfig struct {
	Config  []byte
	Upgrade []byte
}

type ManagerConfig struct {
	StakingEnabled bool            // True iff the network has staking enabled
	StakingCert    tls.Certificate // needed to sign snowman++ blocks
	StakingBLSKey  *bls.SecretKey
	TracingEnabled bool
	// Must not be used unless [TracingEnabled] is true as this may be nil.
	Tracer                      trace.Tracer
	Log                         logging.Logger
	LogFactory                  logging.Factory
	VMManager                   vms.Manager // Manage mappings from vm ID --> vm
	DecisionAcceptorGroup       snow.AcceptorGroup
	ConsensusAcceptorGroup      snow.AcceptorGroup
	DBManager                   dbManager.Manager
	MsgCreator                  message.OutboundMsgBuilder // message creator, shared with network
	Router                      router.Router              // Routes incoming messages to the appropriate chain
	Net                         network.Network            // Sends consensus messages to other validators
	ConsensusParams             avcon.Parameters           // The consensus parameters (alpha, beta, etc.) for new chains
	Validators                  validators.Manager         // Validators validating on this chain
	NodeID                      ids.NodeID                 // The ID of this node
	NetworkID                   uint32                     // ID of the network this node is connected to
	Server                      server.Server              // Handles HTTP API calls
	Keystore                    keystore.Keystore
	AtomicMemory                *atomic.Memory
	AVAXAssetID                 ids.ID
	XChainID                    ids.ID
	CriticalChains              ids.Set         // Chains that can't exit gracefully
	TimeoutManager              timeout.Manager // Manages request timeouts when sending messages to other validators
	Health                      health.Registerer
	RetryBootstrap              bool                    // Should Bootstrap be retried
	RetryBootstrapWarnFrequency int                     // Max number of times to retry bootstrap before warning the node operator
	SubnetConfigs               map[ids.ID]SubnetConfig // ID -> SubnetConfig
	ChainConfigs                map[string]ChainConfig  // alias -> ChainConfig
	// ShutdownNodeFunc allows the chain manager to issue a request to shutdown the node
	ShutdownNodeFunc func(exitCode int)
	MeterVMEnabled   bool // Should each VM be wrapped with a MeterVM
	Metrics          metrics.MultiGatherer

	ConsensusGossipFrequency time.Duration

	GossipConfig sender.GossipConfig

	// Max Time to spend fetching a container and its
	// ancestors when responding to a GetAncestors
	BootstrapMaxTimeGetAncestors time.Duration
	// Max number of containers in an ancestors message sent by this node.
	BootstrapAncestorsMaxContainersSent int
	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	BootstrapAncestorsMaxContainersReceived int

	ApricotPhase4Time            time.Time
	ApricotPhase4MinPChainHeight uint64

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker timetracker.ResourceTracker

	StateSyncBeacons []ids.NodeID
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
	unblockChainCreatorCh  chan struct{}
	chainCreatorShutdownCh chan struct{}

	subnetsLock sync.Mutex
	// Key: Subnet's ID
	// Value: Subnet description
	subnets map[ids.ID]Subnet

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]handler.Handler

	// snowman++ related interface to allow validators retrieval
	validatorState validators.State
}

// New returns a new Manager
func New(config *ManagerConfig) Manager {
	return &manager{
		Aliaser:                ids.NewAliaser(),
		ManagerConfig:          *config,
		subnets:                make(map[ids.ID]Subnet),
		chains:                 make(map[ids.ID]handler.Handler),
		chainsQueue:            buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize),
		unblockChainCreatorCh:  make(chan struct{}),
		chainCreatorShutdownCh: make(chan struct{}),
	}
}

// Router that this chain manager is using to route consensus messages to chains
func (m *manager) Router() router.Router {
	return m.ManagerConfig.Router
}

// QueueChainCreation queues a chain creation request
// Invariant: Whitelisted Subnet must be checked before calling this function
func (m *manager) QueueChainCreation(chainParams ChainParameters) {
	m.subnetsLock.Lock()
	sb, exists := m.subnets[chainParams.SubnetID]
	if !exists {
		sb = newSubnet()
		m.subnets[chainParams.SubnetID] = sb
	}
	addedChain := sb.addChain(chainParams.ID)
	m.subnetsLock.Unlock()

	if !addedChain {
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
// Note: it is expected for the subnet to already have the chain registered as
//       bootstrapping before this function is called
func (m *manager) createChain(chainParams ChainParameters) {
	m.Log.Info("creating chain",
		zap.Stringer("subnetID", chainParams.SubnetID),
		zap.Stringer("chainID", chainParams.ID),
		zap.Stringer("vmID", chainParams.VMID),
	)

	m.subnetsLock.Lock()
	sb := m.subnets[chainParams.SubnetID]
	m.subnetsLock.Unlock()

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
		healthCheckErr := fmt.Errorf("failed to create chain on subnet: %s", chainParams.SubnetID)
		err := m.Health.RegisterHealthCheck(
			chainAlias,
			health.CheckerFunc(func(context.Context) (interface{}, error) {
				return nil, healthCheckErr
			}),
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

	// Notify those that registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Engine)

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
func (m *manager) buildChain(chainParams ChainParameters, sb Subnet) (*chain, error) {
	if chainParams.ID != constants.PlatformChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	primaryAlias := m.PrimaryAliasOrDefault(chainParams.ID)

	// Create the log and context of the chain
	chainLog, err := m.LogFactory.MakeChain(primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error while creating chain's log %w", err)
	}

	consensusMetrics := prometheus.NewRegistry()
	chainNamespace := fmt.Sprintf("%s_%s", constants.PlatformName, primaryAlias)
	if err := m.Metrics.Register(chainNamespace, consensusMetrics); err != nil {
		return nil, fmt.Errorf("error while registering chain's metrics %w", err)
	}

	vmMetrics := metrics.NewOptionalGatherer()
	vmNamespace := fmt.Sprintf("%s_vm", chainNamespace)
	if err := m.Metrics.Register(vmNamespace, vmMetrics); err != nil {
		return nil, fmt.Errorf("error while registering vm's metrics %w", err)
	}

	ctx := &snow.ConsensusContext{
		Context: &snow.Context{
			NetworkID: m.NetworkID,
			SubnetID:  chainParams.SubnetID,
			ChainID:   chainParams.ID,
			NodeID:    m.NodeID,

			XChainID:    m.XChainID,
			AVAXAssetID: m.AVAXAssetID,

			Log:          chainLog,
			Keystore:     m.Keystore.NewBlockchainKeyStore(chainParams.ID),
			SharedMemory: m.AtomicMemory.NewSharedMemory(chainParams.ID),
			BCLookup:     m,
			SNLookup:     m,
			Metrics:      vmMetrics,

			ValidatorState:    m.validatorState,
			StakingCertLeaf:   m.StakingCert.Leaf,
			StakingLeafSigner: m.StakingCert.PrivateKey.(crypto.Signer),
			StakingBLSKey:     m.StakingBLSKey,
		},
		DecisionAcceptor:  m.DecisionAcceptorGroup,
		ConsensusAcceptor: m.ConsensusAcceptorGroup,
		Registerer:        consensusMetrics,
	}
	// We set the state to Initializing here because failing to set the state
	// before it's first access would cause a panic.
	ctx.SetState(snow.Initializing)

	if subnetConfig, ok := m.SubnetConfigs[chainParams.SubnetID]; ok {
		if subnetConfig.ValidatorOnly {
			ctx.SetValidatorOnly()
		}
	}

	// Get a factory for the vm we want to use on our chain
	vmFactory, err := m.VMManager.GetFactory(chainParams.VMID)
	if err != nil {
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}

	// Create the chain
	vm, err := vmFactory.New(ctx.Context)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm: %w", err)
	}
	// TODO: Shutdown VM if an error occurs

	fxs := make([]*common.Fx, len(chainParams.FxIDs))
	for i, fxID := range chainParams.FxIDs {
		// Get a factory for the fx we want to use on our chain
		fxFactory, err := m.VMManager.GetFactory(fxID)
		if err != nil {
			return nil, fmt.Errorf("error while getting fxFactory: %w", err)
		}

		fx, err := fxFactory.New(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("error while creating fx: %w", err)
		}

		// Create the fx
		fxs[i] = &common.Fx{
			ID: fxID,
			Fx: fx,
		}
	}

	consensusParams := m.ConsensusParams
	// short circuit it before reading from subnetConfigs
	if chainParams.SubnetID != constants.PrimaryNetworkID {
		if subnetConfig, ok := m.SubnetConfigs[chainParams.SubnetID]; ok {
			consensusParams = subnetConfig.ConsensusParameters
		}
	}

	var vdrs validators.Set // Validators validating this blockchain
	var ok bool
	if m.StakingEnabled {
		vdrs, ok = m.Validators.GetValidators(chainParams.SubnetID)
	} else { // Staking is disabled. Every peer validates every subnet.
		vdrs, ok = m.Validators.GetValidators(constants.PrimaryNetworkID)
	}
	if !ok {
		return nil, fmt.Errorf("couldn't get validator set of subnet with ID %s. The subnet may not exist", chainParams.SubnetID)
	}

	beacons := vdrs
	if chainParams.CustomBeacons != nil {
		beacons = chainParams.CustomBeacons
	}

	bootstrapWeight := beacons.Weight()

	var chain *chain
	switch vm := vm.(type) {
	case vertex.DAGVM:
		chain, err = m.createAvalancheChain(
			ctx,
			chainParams.GenesisData,
			vdrs,
			beacons,
			vm,
			fxs,
			consensusParams,
			bootstrapWeight,
			sb,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new avalanche vm %w", err)
		}
	case block.ChainVM:
		chain, err = m.createSnowmanChain(
			ctx,
			chainParams.GenesisData,
			vdrs,
			beacons,
			vm,
			fxs,
			consensusParams.Parameters,
			bootstrapWeight,
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

	return chain, nil
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// Create a DAG-based blockchain that uses Avalanche
func (m *manager) createAvalancheChain(
	ctx *snow.ConsensusContext,
	genesisData []byte,
	vdrs,
	beacons validators.Set,
	vm vertex.DAGVM,
	fxs []*common.Fx,
	consensusParams avcon.Parameters,
	bootstrapWeight uint64,
	sb Subnet,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	meterDBManager, err := m.DBManager.NewMeterDBManager("db", ctx.Registerer)
	if err != nil {
		return nil, err
	}
	prefixDBManager := meterDBManager.NewPrefixDBManager(ctx.ChainID[:])
	vmDBManager := prefixDBManager.NewPrefixDBManager([]byte("vm"))

	db := prefixDBManager.Current()
	vertexDB := prefixdb.New([]byte("vertex"), db.Database)
	vertexBootstrappingDB := prefixdb.New([]byte("vertex_bs"), db.Database)
	txBootstrappingDB := prefixdb.New([]byte("tx_bs"), db.Database)

	vtxBlocker, err := queue.NewWithMissing(vertexBootstrappingDB, "vtx", ctx.Registerer)
	if err != nil {
		return nil, err
	}
	txBlocker, err := queue.New(txBootstrappingDB, "tx", ctx.Registerer)
	if err != nil {
		return nil, err
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	gossipConfig := m.GossipConfig
	// short circuit it before reading from subnetConfigs
	if ctx.SubnetID != constants.PrimaryNetworkID {
		if subnetConfig, ok := m.SubnetConfigs[ctx.SubnetID]; ok {
			gossipConfig = subnetConfig.GossipConfig
		}
	}

	// Passes messages from the consensus engine to the network
	messageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		gossipConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize sender: %w", err)
	}

	if m.TracingEnabled {
		messageSender = sender.Trace(messageSender, m.Tracer)
	}

	if err := m.ConsensusAcceptorGroup.RegisterAcceptor(ctx.ChainID, "gossip", messageSender, false); err != nil { // Set up the event dipatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	if m.MeterVMEnabled {
		vm = metervm.NewVertexVM(vm)
	}
	if m.TracingEnabled {
		vm = tracedvm.NewVertexVM(vm, m.Tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := state.NewSerializer(
		state.SerializerConfig{
			ChainID:             ctx.ChainID,
			VM:                  vm,
			DB:                  vertexDB,
			Log:                 ctx.Log,
			XChainMigrationTime: version.GetXChainMigrationTime(ctx.NetworkID),
		},
	)

	err = vm.Initialize(
		context.TODO(),
		ctx.Context,
		vmDBManager,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		msgChan,
		fxs,
		messageSender,
	)
	if err != nil {
		return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	}

	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	// Asynchronously passes messages from the network to the consensus engine
	handler, err := handler.New(
		ctx,
		vdrs,
		msgChan,
		sb.afterBootstrapped(),
		m.ConsensusGossipFrequency,
		m.ResourceTracker,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing network handler: %w", err)
	}

	connectedPeers := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedPeers, (3*bootstrapWeight+3)/4)
	beacons.RegisterCallbackListener(startupTracker)

	commonCfg := common.Config{
		Ctx:                            ctx,
		Validators:                     vdrs,
		Beacons:                        beacons,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Alpha:                          bootstrapWeight/2 + 1, // must be > 50%
		Sender:                         messageSender,
		Subnet:                         sb,
		Timer:                          handler,
		RetryBootstrap:                 m.RetryBootstrap,
		RetryBootstrapWarnFrequency:    m.RetryBootstrapWarnFrequency,
		MaxTimeGetAncestors:            m.BootstrapMaxTimeGetAncestors,
		AncestorsMaxContainersSent:     m.BootstrapAncestorsMaxContainersSent,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		SharedCfg:                      &common.SharedConfig{},
	}

	avaGetHandler, err := avagetter.New(vtxManager, commonCfg)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche base message handler: %w", err)
	}

	// create bootstrap gear
	bootstrapperConfig := avbootstrap.Config{
		Config:        commonCfg,
		AllGetsServer: avaGetHandler,
		VtxBlocked:    vtxBlocker,
		TxBlocked:     txBlocker,
		Manager:       vtxManager,
		VM:            vm,
	}
	bootstrapper, err := avbootstrap.New(
		context.TODO(),
		bootstrapperConfig,
		func(ctx context.Context, lastReqID uint32) error {
			return handler.Consensus().Start(ctx, lastReqID+1)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing avalanche bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		bootstrapper = common.TraceBootstrapableEngine(bootstrapper, m.Tracer)
	}

	handler.SetBootstrapper(bootstrapper)

	var consensus avcon.Consensus = &avcon.Topological{}
	if m.TracingEnabled {
		consensus = avcon.Trace(consensus, m.Tracer)
	}

	// create engine gear
	engineConfig := aveng.Config{
		Ctx:           bootstrapperConfig.Ctx,
		AllGetsServer: avaGetHandler,
		VM:            bootstrapperConfig.VM,
		Manager:       vtxManager,
		Sender:        bootstrapperConfig.Sender,
		Validators:    vdrs,
		Params:        consensusParams,
		Consensus:     consensus,
	}
	engine, err := aveng.New(engineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing avalanche engine: %w", err)
	}

	if m.TracingEnabled {
		engine = aveng.TraceEngine(engine, m.Tracer)
	}

	handler.SetConsensus(engine)

	// Register health check for this chain
	chainAlias := m.PrimaryAliasOrDefault(ctx.ChainID)

	if err := m.Health.RegisterHealthCheck(chainAlias, handler); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &chain{
		Name:    chainAlias,
		Engine:  engine,
		Handler: handler,
	}, nil
}

// Create a linear chain using the Snowman consensus engine
func (m *manager) createSnowmanChain(
	ctx *snow.ConsensusContext,
	genesisData []byte,
	vdrs,
	beacons validators.Set,
	vm block.ChainVM,
	fxs []*common.Fx,
	consensusParams snowball.Parameters,
	bootstrapWeight uint64,
	sb Subnet,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	meterDBManager, err := m.DBManager.NewMeterDBManager("db", ctx.Registerer)
	if err != nil {
		return nil, err
	}
	prefixDBManager := meterDBManager.NewPrefixDBManager(ctx.ChainID[:])
	vmDBManager := prefixDBManager.NewPrefixDBManager([]byte("vm"))

	db := prefixDBManager.Current()
	bootstrappingDB := prefixdb.New([]byte("bs"), db.Database)

	blocked, err := queue.NewWithMissing(bootstrappingDB, "block", ctx.Registerer)
	if err != nil {
		return nil, err
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	gossipConfig := m.GossipConfig
	// short circuit it before reading from subnetConfigs
	if ctx.SubnetID != constants.PrimaryNetworkID {
		if subnetConfig, ok := m.SubnetConfigs[ctx.SubnetID]; ok {
			gossipConfig = subnetConfig.GossipConfig
		}
	}

	// Passes messages from the consensus engine to the network
	messageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		gossipConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize sender: %w", err)
	}

	if m.TracingEnabled {
		messageSender = sender.Trace(messageSender, m.Tracer)
	}

	if err := m.ConsensusAcceptorGroup.RegisterAcceptor(ctx.ChainID, "gossip", messageSender, false); err != nil { // Set up the event dipatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	// first vm to be init is P-Chain once, which provides validator interface to all ProposerVMs
	var bootstrapFunc func()
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

		if !m.ManagerConfig.StakingEnabled {
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

	minBlockDelay := proposervm.DefaultMinBlockDelay
	if subnetCfg, ok := m.SubnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
	}
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
	)

	chainAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, chainAlias, m.Tracer)
	}

	vm = proposervm.New(
		vm,
		m.ApricotPhase4Time,
		m.ApricotPhase4MinPChainHeight,
		minBlockDelay,
	)

	if m.MeterVMEnabled {
		vm = metervm.NewBlockVM(vm)
	}
	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, "proposervm", m.Tracer)
	}

	if err := vm.Initialize(
		context.TODO(),
		ctx.Context,
		vmDBManager,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		msgChan,
		fxs,
		messageSender,
	); err != nil {
		return nil, err
	}

	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	// Asynchronously passes messages from the network to the consensus engine
	handler, err := handler.New(
		ctx,
		vdrs,
		msgChan,
		sb.afterBootstrapped(),
		m.ConsensusGossipFrequency,
		m.ResourceTracker,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize message handler: %w", err)
	}

	connectedPeers := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedPeers, (3*bootstrapWeight+3)/4)
	beacons.RegisterCallbackListener(startupTracker)

	commonCfg := common.Config{
		Ctx:                            ctx,
		Validators:                     vdrs,
		Beacons:                        beacons,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Alpha:                          bootstrapWeight/2 + 1, // must be > 50%
		Sender:                         messageSender,
		Subnet:                         sb,
		Timer:                          handler,
		RetryBootstrap:                 m.RetryBootstrap,
		RetryBootstrapWarnFrequency:    m.RetryBootstrapWarnFrequency,
		MaxTimeGetAncestors:            m.BootstrapMaxTimeGetAncestors,
		AncestorsMaxContainersSent:     m.BootstrapAncestorsMaxContainersSent,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		SharedCfg:                      &common.SharedConfig{},
	}

	snowGetHandler, err := snowgetter.New(vm, commonCfg)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var consensus smcon.Consensus = &smcon.Topological{}
	if m.TracingEnabled {
		consensus = smcon.Trace(consensus, m.Tracer)
	}

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	engineConfig := smeng.Config{
		Ctx:           commonCfg.Ctx,
		AllGetsServer: snowGetHandler,
		VM:            vm,
		Sender:        commonCfg.Sender,
		Validators:    vdrs,
		Params:        consensusParams,
		Consensus:     consensus,
	}
	engine, err := smeng.New(engineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	if m.TracingEnabled {
		engine = smeng.TraceEngine(engine, m.Tracer)
	}

	handler.SetConsensus(engine)

	// create bootstrap gear
	bootstrapCfg := smbootstrap.Config{
		Config:        commonCfg,
		AllGetsServer: snowGetHandler,
		Blocked:       blocked,
		VM:            vm,
		Bootstrapped:  bootstrapFunc,
	}
	bootstrapper, err := smbootstrap.New(
		context.TODO(),
		bootstrapCfg,
		engine.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		bootstrapper = common.TraceBootstrapableEngine(bootstrapper, m.Tracer)
	}

	handler.SetBootstrapper(bootstrapper)

	// create state sync gear
	stateSyncCfg, err := syncer.NewConfig(
		commonCfg,
		m.StateSyncBeacons,
		snowGetHandler,
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

	handler.SetStateSyncer(stateSyncer)

	// Register health checks
	if err := m.Health.RegisterHealthCheck(chainAlias, handler); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &chain{
		Name:    chainAlias,
		Engine:  engine,
		Handler: handler,
	}, nil
}

func (m *manager) SubnetID(chainID ids.ID) (ids.ID, error) {
	m.chainsLock.Lock()
	defer m.chainsLock.Unlock()

	chain, exists := m.chains[chainID]
	if !exists {
		return ids.ID{}, errUnknownChainID
	}
	return chain.Context().SubnetID, nil
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	chain, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	return chain.Context().GetState() == snow.NormalOp
}

func (m *manager) subnetsNotBootstrapped() []ids.ID {
	m.subnetsLock.Lock()
	defer m.subnetsLock.Unlock()

	subnetsBootstrapping := make([]ids.ID, 0, len(m.subnets))
	for subnetID, subnet := range m.subnets {
		if !subnet.IsBootstrapped() {
			subnetsBootstrapping = append(subnetsBootstrapping, subnetID)
		}
	}
	return subnetsBootstrapping
}

func (m *manager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		subnetIDs := m.subnetsNotBootstrapped()
		if len(subnetIDs) != 0 {
			return subnetIDs, errNotBootstrapped
		}
		return subnetIDs, nil
	})
	if err := m.Health.RegisterReadinessCheck("bootstrapped", bootstrappedCheck); err != nil {
		return fmt.Errorf("couldn't register bootstrapped readiness check: %w", err)
	}
	if err := m.Health.RegisterHealthCheck("bootstrapped", bootstrappedCheck); err != nil {
		return fmt.Errorf("couldn't register bootstrapped health check: %w", err)
	}
	return nil
}

// Starts chain creation loop to process queued chains
func (m *manager) StartChainCreator(platform ChainParameters) {
	m.subnetsLock.Lock()
	sb := newSubnet()
	m.subnets[platform.SubnetID] = sb
	sb.addChain(platform.ID)
	m.subnetsLock.Unlock()

	// The P-chain is created synchronously to ensure that `VM.Initialize` has
	// finished before returning from this function. This is required because
	// the P-chain initializes state that the rest of the node initialization
	// depends on.
	m.createChain(platform)

	m.Log.Info("starting chain creator")
	go m.dispatchChainCreator()
}

func (m *manager) dispatchChainCreator() {
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
func (m *manager) closeChainCreator() {
	m.Log.Info("stopping chain creator")
	m.chainsQueue.Close()
	close(m.chainCreatorShutdownCh)
}

// Shutdown stops all the chains
func (m *manager) Shutdown() {
	m.Log.Info("shutting down chain manager")
	m.closeChainCreator()
	m.ManagerConfig.Router.Shutdown(context.TODO())
}

// LookupVM returns the ID of the VM associated with an alias
func (m *manager) LookupVM(alias string) (ids.ID, error) {
	return m.VMManager.Lookup(alias)
}

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *manager) notifyRegistrants(name string, engine common.Engine) {
	for _, registrant := range m.registrants {
		registrant.RegisterChain(name, engine)
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
