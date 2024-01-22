// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	coreth "github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	rpcchainvm2 "github.com/ava-labs/avalanchego/node/rpcchainvm"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	aveng "github.com/ava-labs/avalanchego/snow/engine/avalanche"
	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
	avagetter "github.com/ava-labs/avalanchego/snow/engine/avalanche/getter"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/state"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	smbootstrap "github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/syncer"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	timetracker "github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/metervm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/proposervm"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/tracedvm"
)

const (
	defaultChannelSize = 1
	initialQueueSize   = 3
)

var (
	// Commonly shared VM DB prefix
	vmDBPrefix = []byte("vm")

	// Bootstrapping prefixes for LinearizableVMs
	vertexDBPrefix              = []byte("vertex")
	vertexBootstrappingDBPrefix = []byte("vertex_bs")
	txBootstrappingDBPrefix     = []byte("tx_bs")
	blockBootstrappingDBPrefix  = []byte("block_bs")

	// Bootstrapping prefixes for ChainVMs
	bootstrappingDB = []byte("bs")

	errUnknownVMType           = errors.New("the vm should have type avalanche.DAGVM or snowman.ChainVM")
	errCreatePlatformVM        = errors.New("attempted to create a chain running the PlatformVM")
	errNotBootstrapped         = errors.New("subnets not bootstrapped")
	errNoPrimaryNetworkConfig  = errors.New("no subnet config for primary network found")
	errPartialSyncAsAValidator = errors.New("partial sync should not be configured for a validator")

	_ consensusBuilder[*rpcchainvm.VMClient] = (*snowmanBuilder[*rpcchainvm.VMClient])(nil)
	//_ consensusBuilder[*avm.VM]              = (*avalancheBuilder)(nil)
)

// chainParameters defines the chain being created
type chainParameters struct {
	// The ID of the chain being created.
	ID ids.ID
	// ID of the subnet that validates this chain.
	SubnetID ids.ID
	// The genesis data of this chain's ledger.
	GenesisData []byte
	// The ID of the vm this chain is running.
	VMID ids.ID
}

type chain[T common.VM] struct {
	Name      string
	Context   *snow.ConsensusContext
	VM        T
	WrappedVM common.VM
	Handler   handler.Handler
	Beacons   validators.Manager
}

// ChainConfig is configuration settings for the current execution.
// [Config] is the user-provided config blob for the chain.
// [Upgrade] is a chain-specific blob for coordinating upgrades.
type ChainConfig struct {
	Config  []byte
	Upgrade []byte
}

type chainManager struct {
	config.Config

	platformVMFactory vms.Factory[*VM]
	avmFactory        vms.Factory[*avm.VM]
	corethFactory     vms.Factory[*coreth.VM]

	aliaser       ids.Aliaser
	stakingBLSKey *bls.SecretKey
	// Must not be used unless [TracingEnabled] is true as this may be nil.
	tracer              trace.Tracer
	log                 logging.Logger
	logFactory          logging.Factory
	vmManager           rpcchainvm2.Manager // Manage mappings from vm ID --> vm
	blockAcceptorGroup  snow.AcceptorGroup
	txAcceptorGroup     snow.AcceptorGroup
	vertexAcceptorGroup snow.AcceptorGroup
	db                  database.Database
	msgCreator          message.OutboundMsgBuilder // message creator, shared with network
	router              router.Router              // Routes incoming messages to the appropriate chain
	net                 network.Network            // Sends consensus messages to other validators
	bootstrappers       validators.Manager
	validators          validators.Manager // Validators validating on this chain
	nodeID              ids.NodeID         // The ID of this node
	keystore            keystore.Keystore
	atomicMemory        *atomic.Memory
	xChainID            ids.ID          // ID of the X-Chain,
	cChainID            ids.ID          // ID of the C-Chain,
	criticalChains      set.Set[ids.ID] // Chains that can't exit gracefully
	timeoutManager      timeout.Manager // Manages request timeouts when sending messages to other validators
	health              health.Registerer
	subnetConfigs       map[ids.ID]subnets.Config // ID -> SubnetConfig
	chainConfigs        map[string]ChainConfig    // alias -> ChainConfig
	// ShutdownNodeFunc allows the chainManager to issue a request to shutdown the node
	shutdownNodeFunc func(exitCode int)
	metrics          metrics.MultiGatherer

	// Tracks CPU/disk usage caused by each peer.
	resourceTracker timetracker.ResourceTracker

	stakingSigner crypto.Signer
	stakingCert   *staking.Certificate

	// Those notified when a chain is created
	registrants []chains.Registrant

	// queue that holds chain create requests
	chainsQueue buffer.BlockingDeque[chainParameters]
	// unblocks chain creator to start processing the queue
	unblockChainCreatorCh chan struct{}
	// shutdown the chain creator goroutine if the queue hasn't started to be
	// processed.
	chainCreatorShutdownCh chan struct{}
	chainCreatorExited     sync.WaitGroup

	subnetsLock sync.RWMutex
	// Key: Subnet's ID
	// Value: Subnet description
	subnets map[ids.ID]subnets.Subnet

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]handler.Handler

	// snowman++ related interface to allow validators retrieval
	validatorState validators.State
}

// Create the blockchain if this node is a member of the subnet that validates
// the chain
//
// TODO verify Note: it is expected for the subnet to already have the chain registered as
// bootstrapping before this function is called
func (m *chainManager) QueueChainCreation(chainID ids.ID, subnetID ids.ID, genesis []byte, vmID ids.ID) {
	if m.SybilProtectionEnabled && // Sybil protection is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != subnetID && // All nodes must validate the primary network
		!m.TrackedSubnets.Contains(subnetID) { // This node doesn't validate this blockchain
		return
	}

	m.subnetsLock.Lock()
	sb, exists := m.subnets[subnetID]
	if !exists {
		sbConfig, ok := m.subnetConfigs[subnetID]
		if !ok {
			// default to primary subnet config
			sbConfig = m.subnetConfigs[constants.PrimaryNetworkID]
		}
		sb = subnets.New(m.nodeID, sbConfig)
		m.subnets[subnetID] = sb
	}
	addedChain := sb.AddChain(chainID)
	m.subnetsLock.Unlock()

	if !addedChain {
		m.log.Debug("skipping chain creation",
			zap.String("reason", "chain already staged"),
			zap.Stringer("subnetID", subnetID),
			zap.Stringer("chainID", chainID),
			zap.Stringer("vmID", vmID),
		)
		return
	}

	if ok := m.chainsQueue.PushRight(chainParameters{
		ID:          chainID,
		SubnetID:    subnetID,
		GenesisData: genesis,
		VMID:        vmID,
	}); !ok {
		m.log.Warn("skipping chain creation",
			zap.String("reason", "couldn't enqueue chain"),
			zap.Stringer("subnetID", subnetID),
			zap.Stringer("chainID", chainID),
			zap.Stringer("vmID", vmID),
		)
	}
}

func (m *chainManager) AddRegistrant(r chains.Registrant) {
	m.registrants = append(m.registrants, r)
}

func (m *chainManager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	chain, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	return chain.Context().State.Get().State == snow.NormalOp
}

func (m *chainManager) subnetsNotBootstrapped() []ids.ID {
	m.subnetsLock.RLock()
	defer m.subnetsLock.RUnlock()

	subnetsBootstrapping := make([]ids.ID, 0, len(m.subnets))
	for subnetID, subnet := range m.subnets {
		if !subnet.IsBootstrapped() {
			subnetsBootstrapping = append(subnetsBootstrapping, subnetID)
		}
	}
	return subnetsBootstrapping
}

func (m *chainManager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		subnetIDs := m.subnetsNotBootstrapped()
		if len(subnetIDs) != 0 {
			return subnetIDs, errNotBootstrapped
		}
		return []ids.ID{}, nil
	})
	if err := m.health.RegisterReadinessCheck("bootstrapped", bootstrappedCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register bootstrapped readiness check: %w", err)
	}
	if err := m.health.RegisterHealthCheck("bootstrapped", bootstrappedCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register bootstrapped health check: %w", err)
	}

	// We should only report unhealthy if the node is partially syncing the
	// primary network and is a validator.
	if !m.PartialSyncPrimaryNetwork {
		return nil
	}

	partialSyncCheck := health.CheckerFunc(func(ctx context.Context) (interface{}, error) {
		// Note: The health check is skipped during bootstrapping to allow a
		// node to sync the network even if it was previously a validator.
		if !m.IsBootstrapped(constants.PlatformChainID) {
			return "node is currently bootstrapping", nil
		}
		if _, ok := m.validators.GetValidator(constants.PrimaryNetworkID, m.nodeID); !ok {
			return "node is not a primary network validator", nil
		}

		m.log.Warn("node is a primary network validator",
			zap.Error(errPartialSyncAsAValidator),
		)
		return "node is a primary network validator", errPartialSyncAsAValidator
	})

	if err := m.health.RegisterHealthCheck("validation", partialSyncCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register validation health check: %w", err)
	}
	return nil
}

// Starts chain creation loop to process queued chains
func (m *chainManager) StartChainCreator(platformParams chainParameters) (*VM, error) {
	// Get the Primary Network's subnet config. If it wasn't registered, then we
	// throw a fatal error.
	sbConfig, ok := m.subnetConfigs[constants.PrimaryNetworkID]
	if !ok {
		return nil, errNoPrimaryNetworkConfig
	}

	sb := subnets.New(m.nodeID, sbConfig)
	m.subnetsLock.Lock()
	m.subnets[platformParams.SubnetID] = sb
	sb.AddChain(platformParams.ID)
	m.subnetsLock.Unlock()

	// The P-chain is created synchronously to ensure that `VM.Initialize` has
	// finished before returning from this function. This is required because
	// the P-chain initializes state that the rest of the node initialization
	// depends on.
	chainBuilder := chainCreator[*VM]{
		m:                m,
		beacons:          m.bootstrappers,
		factory:          m.platformVMFactory,
		consensusBuilder: platformBuilder{},
	}
	chain, err := chainBuilder.createChain(platformParams)
	if err != nil {
		return nil, err
	}

	m.log.Info("starting chain creator")
	m.chainCreatorExited.Add(1)
	go m.dispatchChainCreator()
	return chain.VM, err
}

func (m *chainManager) dispatchChainCreator() {
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

		switch chainParams.ID {
		case m.xChainID:
			//chainCreator := chainCreator[*linearizeOnInitializeVM]{
			//	m:                m,
			//	beacons:          m.validators,
			//	factory:          m.avmFactory,
			//	consensusBuilder: avalancheBuilder{},
			//}
			//_, _ = chainCreator.createChain(chainParams)
		case m.cChainID:
			chainCreator := chainCreator[*coreth.VM]{
				m:                m,
				beacons:          m.validators,
				factory:          m.corethFactory,
				consensusBuilder: snowmanBuilder[*coreth.VM]{},
			}
			_, _ = chainCreator.createChain(chainParams)
		default:
			vmFactory, err := m.vmManager.GetFactory(chainParams.VMID)
			if err != nil {
				m.log.Error("failed to create chain", zap.Error(err))
				continue
			}

			chainCreator := chainCreator[*rpcchainvm.VMClient]{
				m:                m,
				beacons:          m.validators,
				factory:          vmFactory,
				consensusBuilder: snowmanBuilder[*rpcchainvm.VMClient]{},
			}
			_, _ = chainCreator.createChain(chainParams)
		}
	}
}

// Shutdown stops all the chains
func (m *chainManager) Shutdown() {
	m.log.Info("shutting down chain manager")
	m.chainsQueue.Close()
	close(m.chainCreatorShutdownCh)
	m.chainCreatorExited.Wait()
	m.router.Shutdown(context.TODO())
}

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *chainManager) notifyRegistrants(name string, ctx *snow.ConsensusContext, created common.VM) {
	for _, registrant := range m.registrants {
		registrant.RegisterChain(name, ctx, created)
	}
}

// getChainConfig returns value of a entry by looking at ID key and alias key
// it first searches ID key, then falls back to it's corresponding primary alias
func (m *chainManager) getChainConfig(id ids.ID) (ChainConfig, error) {
	if val, ok := m.chainConfigs[id.String()]; ok {
		return val, nil
	}
	aliases, err := m.aliaser.Aliases(id)
	if err != nil {
		return ChainConfig{}, err
	}
	for _, alias := range aliases {
		if val, ok := m.chainConfigs[alias]; ok {
			return val, nil
		}
	}

	return ChainConfig{}, nil
}

type chainCreator[T common.VM] struct {
	m *chainManager

	beacons          validators.Manager
	factory          vms.Factory[T]
	consensusBuilder consensusBuilder[T]
}

// Create a chain
func (b *chainCreator[T]) createChain(chainParams chainParameters) (*chain[T], error) {
	m := b.m

	m.log.Info("creating chain",
		zap.Stringer("subnetID", chainParams.SubnetID),
		zap.Stringer("chainID", chainParams.ID),
		zap.Stringer("vmID", chainParams.VMID),
	)

	m.subnetsLock.RLock()
	sb := m.subnets[chainParams.SubnetID]
	m.subnetsLock.RUnlock()

	// Note: buildChain builds all chain's relevant objects (notably engine and handler)
	// but does not start their operations. Starting of the handler (which could potentially
	// issue some internal messages), is delayed until chain dispatching is started and
	// the chain is registered in the manager. This ensures that no message generated by handler
	// upon start is dropped.
	chain, err := b.buildChain(chainParams, sb)
	if err != nil {
		if m.criticalChains.Contains(chainParams.ID) {
			// Shut down if we fail to create a required chain (i.e. X, P or C)
			m.log.Fatal("error creating required chain",
				zap.Stringer("subnetID", chainParams.SubnetID),
				zap.Stringer("chainID", chainParams.ID),
				zap.Stringer("vmID", chainParams.VMID),
				zap.Error(err),
			)
			//TODO handle shutdown case in the caller of vmFactory.New()
			go m.shutdownNodeFunc(1)
			return nil, err
		}

		chainAlias := m.aliaser.PrimaryAliasOrDefault(chainParams.ID)
		m.log.Error("error creating chain",
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
		err := m.health.RegisterHealthCheck(
			chainAlias,
			health.CheckerFunc(func(context.Context) (interface{}, error) {
				return nil, healthCheckErr
			}),
			chainParams.SubnetID.String(),
		)
		if err != nil {
			m.log.Error("failed to register failing health check",
				zap.Stringer("subnetID", chainParams.SubnetID),
				zap.Stringer("chainID", chainParams.ID),
				zap.String("chainAlias", chainAlias),
				zap.Stringer("vmID", chainParams.VMID),
				zap.Error(err),
			)
		}
		return nil, err
	}

	m.chainsLock.Lock()
	m.chains[chainParams.ID] = chain.Handler
	m.chainsLock.Unlock()

	// Associate the newly created chain with its default alias
	if err := m.aliaser.Alias(chainParams.ID, chainParams.ID.String()); err != nil {
		m.log.Error("failed to alias the new chain with itself",
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
			zap.Error(err),
		)
	}

	// Notify those that registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Context, chain.WrappedVM)

	// Allows messages to be routed to the new chain. If the handler hasn't been
	// started and a message is forwarded, then the message will block until the
	// handler is started.
	m.router.AddChain(context.TODO(), chain.Handler)

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
	chain.Handler.Start(context.TODO(), !m.criticalChains.Contains(chainParams.ID))
	return chain, nil
}

func (b *chainCreator[T]) buildChain(chainParams chainParameters, sb subnets.Subnet) (*chain[T], error) {
	m := b.m
	if chainParams.ID != constants.PlatformChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	primaryAlias := m.aliaser.PrimaryAliasOrDefault(chainParams.ID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainParams.ID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog, err := m.logFactory.MakeChain(primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error while creating chain's log %w", err)
	}

	consensusMetrics := prometheus.NewRegistry()
	chainNamespace := metric.AppendNamespace(constants.PlatformName, primaryAlias)
	if err := m.metrics.Register(chainNamespace, consensusMetrics); err != nil {
		return nil, fmt.Errorf("error while registering chain's metrics %w", err)
	}

	// This converts the prefix for all the Avalanche consensus metrics from
	// `avalanche_{chainID}_` into `avalanche_{chainID}_avalanche_` so that
	// there are no conflicts when registering the Snowman consensus metrics.
	avalancheConsensusMetrics := prometheus.NewRegistry()
	avalancheDAGNamespace := metric.AppendNamespace(chainNamespace, "avalanche")
	if err := m.metrics.Register(avalancheDAGNamespace, avalancheConsensusMetrics); err != nil {
		return nil, fmt.Errorf("error while registering DAG metrics %w", err)
	}

	vmMetrics := metrics.NewOptionalGatherer()
	vmNamespace := metric.AppendNamespace(chainNamespace, "vm")
	if err := m.metrics.Register(vmNamespace, vmMetrics); err != nil {
		return nil, fmt.Errorf("error while registering vm's metrics %w", err)
	}

	ctx := &snow.ConsensusContext{
		Context: &snow.Context{
			NetworkID: m.NetworkID,
			SubnetID:  chainParams.SubnetID,
			ChainID:   chainParams.ID,
			NodeID:    m.nodeID,
			PublicKey: bls.PublicFromSecretKey(m.stakingBLSKey),

			XChainID:    m.xChainID,
			CChainID:    m.cChainID,
			AVAXAssetID: m.AVAXAssetID,

			Log:          chainLog,
			Keystore:     m.keystore.NewBlockchainKeyStore(chainParams.ID),
			SharedMemory: m.atomicMemory.NewSharedMemory(chainParams.ID),
			BCLookup:     m.aliaser,
			Metrics:      vmMetrics,

			WarpSigner: warp.NewSigner(m.stakingBLSKey, m.NetworkID, chainParams.ID),

			ValidatorState: m.validatorState,
			ChainDataDir:   chainDataDir,
		},
		BlockAcceptor:       m.blockAcceptorGroup,
		TxAcceptor:          m.txAcceptorGroup,
		VertexAcceptor:      m.vertexAcceptorGroup,
		Registerer:          consensusMetrics,
		AvalancheRegisterer: avalancheConsensusMetrics,
	}

	chain, err := b.consensusBuilder.build(m, ctx, chainParams, b.beacons, m.validators, b.factory, sb)
	if err != nil {
		return nil, err
	}

	// Register the chain with the timeout ChainManager
	if err := m.timeoutManager.RegisterChain(ctx); err != nil {
		return nil, err
	}

	return chain, nil
}

type consensusBuilder[T common.VM] interface {
	build(
		m *chainManager,
		ctx *snow.ConsensusContext,
		params chainParameters,
		vdrs,
		beacons validators.Manager,
		vmFactory vms.Factory[T],
		sb subnets.Subnet,
	) (*chain[T], error)
}

type snowmanBuilder[T block.ChainVM] struct{}

func (s snowmanBuilder[T]) build(
	m *chainManager,
	ctx *snow.ConsensusContext,
	params chainParameters,
	vdrs,
	beacons validators.Manager,
	vmFactory vms.Factory[T],
	sb subnets.Subnet,
) (*chain[T], error) {
	vm, err := vmFactory.New(ctx.Log)
	if err != nil {
		return nil, fmt.Errorf("error while creating m: %w", err)
	}

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Initializing,
	})

	meterDB, err := meterdb.New("db", ctx.Registerer, m.db)
	if err != nil {
		return nil, err
	}
	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(vmDBPrefix, prefixDB)
	bootstrappingDB := prefixdb.New(bootstrappingDB, prefixDB)

	blocked, err := queue.NewWithMissing(bootstrappingDB, "block", ctx.Registerer)
	if err != nil {
		return nil, err
	}

	// Passes messages from the consensus engine to the network
	messageSender, err := sender.New(
		ctx,
		m.msgCreator,
		m.net,
		m.router,
		m.timeoutManager,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize sender: %w", err)
	}

	if m.TracingEnabled {
		messageSender = sender.Trace(messageSender, m.tracer)
	}

	err = m.blockAcceptorGroup.RegisterAcceptor(
		ctx.ChainID,
		"gossip",
		messageSender,
		false,
	)
	if err != nil { // Set up the event dispatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	var (
		bootstrapFunc   func()
		subnetConnector               = validators.UnhandledSubnetConnector
		wrappedVM       block.ChainVM = vm
	)
	// If [m.validatorState] is nil then we are creating the P-Chain. Since the
	// P-Chain is the first chain to be created, we can use it to initialize
	// required interfaces for the other chains
	if m.validatorState == nil {
		valState, ok := wrappedVM.(validators.State)
		if !ok {
			return nil, fmt.Errorf("expected validators.State but got %T", m)
		}

		if m.TracingEnabled {
			valState = validators.Trace(valState, "platformvm", m.tracer)
		}

		// Notice that this context is left unlocked. This is because the
		// lock will already be held when accessing these values on the
		// P-chain.
		ctx.ValidatorState = valState

		// Initialize the validator state for future chains.
		m.validatorState = validators.NewLockedState(&ctx.Lock, valState)
		if m.TracingEnabled {
			m.validatorState = validators.Trace(m.validatorState, "lockedState", m.tracer)
		}

		if !m.SybilProtectionEnabled {
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

		// Set up the subnet connector for the P-Chain
		subnetConnector, ok = wrappedVM.(validators.SubnetConnector)
		if !ok {
			return nil, fmt.Errorf("expected validators.SubnetConnector but got %T", m)
		}
	}

	// Initialize the ProposerVM and the m wrapped inside it
	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	if subnetCfg, ok := m.subnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	m.log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	chainAlias := m.aliaser.PrimaryAliasOrDefault(ctx.ChainID)

	if m.TracingEnabled {
		wrappedVM = tracedvm.NewBlockVM(vm, chainAlias, m.tracer)
	}

	wrappedVM = proposervm.New(
		wrappedVM,
		proposervm.Config{
			ActivationTime:      m.ApricotPhase4Time,
			DurangoTime:         version.GetDurangoTime(m.NetworkID),
			MinimumPChainHeight: m.ApricotPhase4MinPChainHeight,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.stakingSigner,
			StakingCertLeaf:     m.stakingCert,
		},
	)

	if m.MeterVMEnabled {
		wrappedVM = metervm.NewBlockVM(vm)
	}
	if m.TracingEnabled {
		wrappedVM = tracedvm.NewBlockVM(vm, "proposervm", m.tracer)
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	if err := vm.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		params.GenesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		msgChan,
		nil,
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
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.resourceTracker,
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
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var consensus smcon.Consensus = &smcon.Topological{}
	if m.TracingEnabled {
		consensus = smcon.Trace(consensus, m.tracer)
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
	engine, err := smeng.New(engineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	if m.TracingEnabled {
		engine = smeng.TraceEngine(engine, m.tracer)
	}

	// create smbootstrap gear
	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        beacons,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         messageSender,
		BootstrapTracker:               sb,
		Timer:                          h,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
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

	if m.TracingEnabled {
		bootstrapper = common.TraceBootstrapableEngine(bootstrapper, m.tracer)
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
		stateSyncer = common.TraceStateSyncer(stateSyncer, m.tracer)
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
	if err := m.health.RegisterHealthCheck(chainAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &chain[T]{
		Name:      chainAlias,
		Context:   ctx,
		VM:        vm,
		WrappedVM: wrappedVM,
		Handler:   h,
	}, nil
}

type platformBuilder struct {
	snowmanBuilder[*VM]
}

func (p platformBuilder) build(
	m *chainManager,
	ctx *snow.ConsensusContext,
	params chainParameters,
	vdrs,
	beacons validators.Manager,
	vmFactory vms.Factory[*VM],
	sb subnets.Subnet,
) (*chain[*VM], error) {
	return p.snowmanBuilder.build(m, ctx, params, vdrs, beacons, vmFactory, sb)
}

type avalancheBuilder struct{}

func (x avalancheBuilder) build(
	m *chainManager,
	ctx *snow.ConsensusContext,
	params chainParameters,
	vdrs,
	_ validators.Manager,
	vmFactory vms.Factory[*avm.VM],
	sb subnets.Subnet,
) (*chain[*linearizeOnInitializeVM], error) {
	vm, err := vmFactory.New(ctx.Log)
	if err != nil {
		return nil, err
	}

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.Initializing,
	})

	meterDB, err := meterdb.New("db", ctx.Registerer, m.db)
	if err != nil {
		return nil, err
	}
	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(vmDBPrefix, prefixDB)
	vertexDB := prefixdb.New(vertexDBPrefix, prefixDB)
	vertexBootstrappingDB := prefixdb.New(vertexBootstrappingDBPrefix, prefixDB)
	txBootstrappingDB := prefixdb.New(txBootstrappingDBPrefix, prefixDB)
	blockBootstrappingDB := prefixdb.New(blockBootstrappingDBPrefix, prefixDB)

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
		m.msgCreator,
		m.net,
		m.router,
		m.timeoutManager,
		p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche sender: %w", err)
	}

	if m.TracingEnabled {
		avalancheMessageSender = sender.Trace(avalancheMessageSender, m.tracer)
	}

	err = m.vertexAcceptorGroup.RegisterAcceptor(
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
		m.msgCreator,
		m.net,
		m.router,
		m.timeoutManager,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche sender: %w", err)
	}

	if m.TracingEnabled {
		snowmanMessageSender = sender.Trace(snowmanMessageSender, m.tracer)
	}

	err = m.blockAcceptorGroup.RegisterAcceptor(
		ctx.ChainID,
		"gossip",
		snowmanMessageSender,
		false,
	)
	if err != nil { // Set up the event dispatcher
		return nil, fmt.Errorf("problem initializing event dispatcher: %w", err)
	}

	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var wrappedVM vertex.LinearizableVMWithEngine
	wrappedVM = vm
	if m.MeterVMEnabled {
		wrappedVM = metervm.NewVertexVM(vm)
	}
	if m.TracingEnabled {
		wrappedVM = tracedvm.NewVertexVM(vm, m.tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := state.NewSerializer(
		state.SerializerConfig{
			ChainID:     ctx.ChainID,
			VM:          wrappedVM,
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
	fxs := []*common.Fx{
		{
			ID: secp256k1fx.ID,
			Fx: (&secp256k1fx.Factory{}).New(),
		},
		{
			ID: nftfx.ID,
			Fx: (&nftfx.Factory{}).New(),
		},
		{
			ID: propertyfx.ID,
			Fx: (&propertyfx.Factory{}).New(),
		},
	}

	// The only difference between using avalancheMessageSender and
	// snowmanMessageSender here is where the metrics will be placed. Because we
	// end up using this sender after the linearization, we pass in
	// snowmanMessageSender here.
	err = wrappedVM.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		params.GenesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		msgChan,
		fxs,
		snowmanMessageSender,
	)
	if err != nil {
		return nil, fmt.Errorf("error during m's Initialize: %w", err)
	}

	// Initialize the ProposerVM and the m wrapped inside it
	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	if subnetCfg, ok := m.subnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	m.log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	chainAlias := m.aliaser.PrimaryAliasOrDefault(ctx.ChainID)

	// Note: this does not use [dagVM] to ensure we use the [m]'s height index.
	untracedVMWrappedInsideProposerVM := NewLinearizeOnInitializeVM(vm)

	var vmWrappedInsideProposerVM block.ChainVM = untracedVMWrappedInsideProposerVM
	if m.TracingEnabled {
		vmWrappedInsideProposerVM = tracedvm.NewBlockVM(vmWrappedInsideProposerVM, chainAlias, m.tracer)
	}

	// Note: vmWrappingProposerVM is the VM that the Snowman engines should be
	// using.
	var vmWrappingProposerVM block.ChainVM = proposervm.New(
		vmWrappedInsideProposerVM,
		proposervm.Config{
			ActivationTime:      m.ApricotPhase4Time,
			DurangoTime:         version.GetDurangoTime(m.NetworkID),
			MinimumPChainHeight: m.ApricotPhase4MinPChainHeight,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.stakingSigner,
			StakingCertLeaf:     m.stakingCert,
		},
	)

	if m.MeterVMEnabled {
		vmWrappingProposerVM = metervm.NewBlockVM(vmWrappingProposerVM)
	}
	if m.TracingEnabled {
		vmWrappingProposerVM = tracedvm.NewBlockVM(vmWrappingProposerVM, "proposervm", m.tracer)
	}

	// Note: linearizableVM is the VM that the Avalanche engines should be
	// using.
	linearizableVM := &initializeOnLinearizeVM{
		DAGVM:          wrappedVM,
		vmToInitialize: vmWrappingProposerVM,
		vmToLinearize:  untracedVMWrappedInsideProposerVM,

		registerer:   snowmanRegisterer,
		ctx:          ctx.Context,
		db:           vmDB,
		genesisBytes: params.GenesisData,
		upgradeBytes: chainConfig.Upgrade,
		configBytes:  chainConfig.Config,
		toEngine:     msgChan,
		fxs:          []*common.Fx{}, //TODO
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
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.resourceTracker,
		validators.UnhandledSubnetConnector, // avalanche chains don't use subnet connector
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
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize snow base message handler: %w", err)
	}

	var snowmanConsensus smcon.Consensus = &smcon.Topological{}
	if m.TracingEnabled {
		snowmanConsensus = smcon.Trace(snowmanConsensus, m.tracer)
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

	if m.TracingEnabled {
		snowmanEngine = smeng.TraceEngine(snowmanEngine, m.tracer)
	}

	// create smbootstrap gear
	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         snowmanMessageSender,
		BootstrapTracker:               sb,
		Timer:                          h,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		Blocked:                        blockBlocker,
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
		snowmanBootstrapper = common.TraceBootstrapableEngine(snowmanBootstrapper, m.tracer)
	}

	avaGetHandler, err := avagetter.New(
		vtxManager,
		avalancheMessageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		ctx.AvalancheRegisterer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize avalanche base message handler: %w", err)
	}

	// create engine gear
	avalancheEngine := aveng.New(ctx, avaGetHandler, linearizableVM)
	if m.TracingEnabled {
		avalancheEngine = common.TraceEngine(avalancheEngine, m.tracer)
	}

	// create smbootstrap gear
	avalancheBootstrapperConfig := avbootstrap.Config{
		AllGetsServer:                  avaGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		StartupTracker:                 startupTracker,
		Sender:                         avalancheMessageSender,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		VtxBlocked:                     vtxBlocker,
		TxBlocked:                      txBlocker,
		Manager:                        vtxManager,
		VM:                             linearizableVM,
	}
	if ctx.ChainID == m.xChainID {
		avalancheBootstrapperConfig.StopVertexID = version.CortinaXChainStopVertexID[ctx.NetworkID]
	}

	avalancheBootstrapper, err := avbootstrap.New(
		avalancheBootstrapperConfig,
		snowmanBootstrapper.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing avalanche bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		avalancheBootstrapper = common.TraceBootstrapableEngine(avalancheBootstrapper, m.tracer)
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
	if err := m.health.RegisterHealthCheck(chainAlias, h, ctx.SubnetID.String()); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &chain[*linearizeOnInitializeVM]{
		Name:      chainAlias,
		Context:   ctx,
		VM:        untracedVMWrappedInsideProposerVM,
		WrappedVM: wrappedVM,
		Handler:   h,
	}, nil
}
