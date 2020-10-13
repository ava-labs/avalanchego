// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/state"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"

	avcon "github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	aveng "github.com/ava-labs/avalanchego/snow/engine/avalanche"
	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	smbootstrap "github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
)

const (
	defaultChannelSize = 1024
)

// Manager manages the chains running on this node.
// It can:
//   * Create a chain
//   * Add a registrant. When a chain is created, each registrant calls
//     RegisterChain with the new chain as the argument.
//   * Get the aliases associated with a given chain.
//   * Get the ID of the chain associated with a given alias.
type Manager interface {
	// Return the router this Manager is using to route consensus messages to chains
	Router() router.Router

	// Create a chain in the future
	CreateChain(ChainParameters)

	// Create a chain now
	ForceCreateChain(ChainParameters)

	// Add a registrant [r]. Every time a chain is
	// created, [r].RegisterChain([new chain]) is called
	AddRegistrant(Registrant)

	// Given an alias, return the ID of the chain associated with that alias
	Lookup(string) (ids.ID, error)

	// Given an alias, return the ID of the VM associated with that alias
	LookupVM(string) (ids.ID, error)

	// Return the aliases associated with a chain
	Aliases(ids.ID) []string

	// Add an alias to a chain
	Alias(ids.ID, string) error

	// Returns the ID of the subnet that is validating the provided chain
	SubnetID(chainID ids.ID) (ids.ID, error)

	// Returns true iff the chain with the given ID exists and is finished bootstrapping
	IsBootstrapped(ids.ID) bool

	Shutdown()
}

// ChainParameters defines the chain being created
type ChainParameters struct {
	ID          ids.ID   // The ID of the chain being created
	SubnetID    ids.ID   // ID of the subnet that validates this chain
	GenesisData []byte   // The genesis data of this chain's ledger
	VMAlias     string   // The ID of the vm this chain is running
	FxAliases   []string // The IDs of the feature extensions this chain is running

	CustomBeacons validators.Set // Should only be set if the default beacons can't be used.
}

type chain struct {
	Engine  common.Engine
	Handler *router.Handler
	Ctx     *snow.Context
	VM      interface{}
	Beacons validators.Set
}

// ManagerConfig ...
type ManagerConfig struct {
	StakingEnabled          bool // True iff the network has staking enabled
	MaxNonStakerPendingMsgs uint32
	StakerMSGPortion        float64
	StakerCPUPortion        float64
	Log                     logging.Logger
	LogFactory              logging.Factory
	VMManager               vms.Manager // Manage mappings from vm ID --> vm
	DecisionEvents          *triggers.EventDispatcher
	ConsensusEvents         *triggers.EventDispatcher
	DB                      database.Database
	Router                  router.Router      // Routes incoming messages to the appropriate chain
	Net                     network.Network    // Sends consensus messages to other validators
	ConsensusParams         avcon.Parameters   // The consensus parameters (alpha, beta, etc.) for new chains
	Validators              validators.Manager // Validators validating on this chain
	NodeID                  ids.ShortID        // The ID of this node
	NetworkID               uint32             // ID of the network this node is connected to
	Server                  *api.Server        // Handles HTTP API calls
	Keystore                *keystore.Keystore
	AtomicMemory            *atomic.Memory
	AVAXAssetID             ids.ID
	XChainID                ids.ID
	CriticalChains          ids.Set          // Chains that can't exit gracefully
	TimeoutManager          *timeout.Manager // Manages request timeouts when sending messages to other validators
	HealthService           *health.Health
}

type manager struct {
	// Note: The string representation of a chain's ID is also considered to be an alias of the chain
	// That is, [chainID].String() is an alias for the chain, too
	ids.Aliaser
	ManagerConfig

	registrants []Registrant // Those notified when a chain is created

	unblocked     bool
	blockedChains []ChainParameters

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[[32]byte]*router.Handler
}

// New returns a new Manager where:
//     <db> is this node's database
//     <sender> sends messages to other validators
//     <validators> validate this chain
// TODO: Make this function take less arguments
func New(config *ManagerConfig) Manager {
	m := &manager{
		ManagerConfig: *config,
		chains:        make(map[[32]byte]*router.Handler),
	}
	m.Initialize()
	return m
}

// Router that this chain manager is using to route consensus messages to chains
func (m *manager) Router() router.Router { return m.ManagerConfig.Router }

// Create a chain
func (m *manager) CreateChain(chain ChainParameters) {
	if !m.unblocked {
		m.blockedChains = append(m.blockedChains, chain)
	} else {
		m.ForceCreateChain(chain)
	}
}

// Create a chain
func (m *manager) ForceCreateChain(chainParams ChainParameters) {
	// Assert that there isn't already a chain with an alias in [chain].Aliases
	// (Recall that the string repr. of a chain's ID is also an alias for a chain)
	if alias, isRepeat := m.isChainWithAlias(chainParams.ID.String()); isRepeat {
		m.Log.Debug("there is already a chain with alias '%s'. Chain not created.",
			alias)
		return
	}

	m.Log.Info("creating chain:\n"+
		"    ID: %s\n"+
		"    VMID:%s",
		chainParams.ID,
		chainParams.VMAlias,
	)

	chain, err := m.buildChain(chainParams)
	if err != nil {
		m.Log.Error("Error while creating new chain: %s", err)
		return
	}
	chainID := chainParams.ID.Key()

	m.chainsLock.Lock()
	m.chains[chainID] = chain.Handler
	m.chainsLock.Unlock()

	// Associate the newly created chain with its default alias
	m.Log.AssertNoError(m.Alias(chainParams.ID, chainParams.ID.String()))

	// Notify those that registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Ctx, chain.VM)
}

// Create a chain
func (m *manager) buildChain(chainParams ChainParameters) (*chain, error) {
	vmID, err := m.VMManager.Lookup(chainParams.VMAlias)
	if err != nil {
		return nil, fmt.Errorf("error while looking up VM: %w", err)
	}

	primaryAlias, err := m.PrimaryAlias(chainParams.ID)
	if err != nil {
		primaryAlias = chainParams.ID.String()
	}

	// Create the log and context of the chain
	chainLog, err := m.LogFactory.MakeChain(primaryAlias, "")
	if err != nil {
		return nil, fmt.Errorf("error while creating chain's log %w", err)
	}

	ctx := &snow.Context{
		NetworkID:           m.NetworkID,
		SubnetID:            chainParams.SubnetID,
		ChainID:             chainParams.ID,
		NodeID:              m.NodeID,
		XChainID:            m.XChainID,
		AVAXAssetID:         m.AVAXAssetID,
		Log:                 chainLog,
		DecisionDispatcher:  m.DecisionEvents,
		ConsensusDispatcher: m.ConsensusEvents,
		Keystore:            m.Keystore.NewBlockchainKeyStore(chainParams.ID),
		SharedMemory:        m.AtomicMemory.NewSharedMemory(chainParams.ID),
		BCLookup:            m,
		SNLookup:            m,
		Namespace:           fmt.Sprintf("%s_%s_vm", constants.PlatformName, primaryAlias),
		Metrics:             m.ConsensusParams.Metrics,
	}

	// Get a factory for the vm we want to use on our chain
	vmFactory, err := m.VMManager.GetVMFactory(vmID)
	if err != nil {
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}

	// Create the chain
	vm, err := vmFactory.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm: %w", err)
	}
	// TODO: Shutdown VM if an error occurs

	fxs := make([]*common.Fx, len(chainParams.FxAliases))
	for i, fxAlias := range chainParams.FxAliases {
		fxID, err := m.VMManager.Lookup(fxAlias)
		if err != nil {
			return nil, fmt.Errorf("error while looking up Fx: %w", err)
		}

		// Get a factory for the fx we want to use on our chain
		fxFactory, err := m.VMManager.GetVMFactory(fxID)
		if err != nil {
			return nil, fmt.Errorf("error while getting fxFactory: %w", err)
		}

		fx, err := fxFactory.New(ctx)
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
	consensusParams.Namespace = fmt.Sprintf("%s_%s", constants.PlatformName, primaryAlias)

	// The validators of this blockchain
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
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new snowman vm %w", err)
		}
	default:
		return nil, fmt.Errorf("the vm should have type avalanche.DAGVM or snowman.ChainVM. Chain not created")
	}

	// Register the chain with the timeout manager
	if err := m.TimeoutManager.RegisterChain(ctx, consensusParams.Namespace); err != nil {
		return nil, err
	}

	// Allows messages to be routed to the new chain
	m.ManagerConfig.Router.AddChain(chain.Handler)

	// If the X or P Chain panics, do not attempt to recover
	if m.CriticalChains.Contains(chainParams.ID) {
		go ctx.Log.RecoverAndPanic(chain.Handler.Dispatch)
	} else {
		go ctx.Log.RecoverAndExit(chain.Handler.Dispatch, func() {
			ctx.Log.Error("Chain with ID: %s was shutdown due to a panic", chainParams.ID)
		})
	}
	return chain, nil
}

// Implements Manager.AddRegistrant
func (m *manager) AddRegistrant(r Registrant) { m.registrants = append(m.registrants, r) }

func (m *manager) unblockChains() {
	m.unblocked = true
	blocked := m.blockedChains
	m.blockedChains = nil
	for _, chainParams := range blocked {
		m.ForceCreateChain(chainParams)
	}
}

// Create a DAG-based blockchain that uses Avalanche
func (m *manager) createAvalancheChain(
	ctx *snow.Context,
	genesisData []byte,
	validators,
	beacons validators.Set,
	vm vertex.DAGVM,
	fxs []*common.Fx,
	consensusParams avcon.Parameters,
	bootstrapWeight uint64,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	db := prefixdb.New(ctx.ChainID.Bytes(), m.DB)
	vmDB := prefixdb.New([]byte("vm"), db)
	vertexDB := prefixdb.New([]byte("vertex"), db)
	vertexBootstrappingDB := prefixdb.New([]byte("vertex_bs"), db)
	txBootstrappingDB := prefixdb.New([]byte("tx_bs"), db)

	vtxBlocker, err := queue.New(vertexBootstrappingDB)
	if err != nil {
		return nil, err
	}
	txBlocker, err := queue.New(txBootstrappingDB)
	if err != nil {
		return nil, err
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	if err := vm.Initialize(ctx, vmDB, genesisData, msgChan, fxs); err != nil {
		return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := &state.Serializer{}
	vtxManager.Initialize(ctx, vm, vertexDB)

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}
	sender.Initialize(ctx, m.Net, m.ManagerConfig.Router, m.TimeoutManager)

	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	// The engine handles consensus
	engine := &aveng.Transitive{}
	if err := engine.Initialize(aveng.Config{
		Config: avbootstrap.Config{
			Config: common.Config{
				Ctx:          ctx,
				Validators:   validators,
				Beacons:      beacons,
				SampleK:      sampleK,
				StartupAlpha: (3*bootstrapWeight + 3) / 4,
				Alpha:        bootstrapWeight/2 + 1, // must be > 50%
				Sender:       &sender,
			},
			VtxBlocked: vtxBlocker,
			TxBlocked:  txBlocker,
			Manager:    vtxManager,
			VM:         vm,
		},
		Params:    consensusParams,
		Consensus: &avcon.Topological{},
	}); err != nil {
		return nil, fmt.Errorf("error initializing avalanche engine: %w", err)
	}

	// Register health checks
	chainAlias, err := m.PrimaryAlias(ctx.ChainID)
	if err != nil {
		chainAlias = ctx.ChainID.String()
	}
	wrapperHc := &healthCheckWrapper{
		chain: chainAlias,
		lock:  &ctx.Lock,
		check: engine.Health,
	}
	if err := m.HealthService.RegisterCheck(wrapperHc); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	// Asynchronously passes messages from the network to the consensus engine
	handler := &router.Handler{}
	handler.Initialize(
		engine,
		validators,
		msgChan,
		defaultChannelSize,
		m.MaxNonStakerPendingMsgs,
		m.StakerMSGPortion,
		m.StakerCPUPortion,
		fmt.Sprintf("%s_handler", consensusParams.Namespace),
		consensusParams.Metrics,
	)

	return &chain{
		Engine:  engine,
		Handler: handler,
		VM:      vm,
		Ctx:     ctx,
	}, nil
}

// Create a linear chain using the Snowman consensus engine
func (m *manager) createSnowmanChain(
	ctx *snow.Context,
	genesisData []byte,
	validators,
	beacons validators.Set,
	vm block.ChainVM,
	fxs []*common.Fx,
	consensusParams snowball.Parameters,
	bootstrapWeight uint64,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	db := prefixdb.New(ctx.ChainID.Bytes(), m.DB)
	vmDB := prefixdb.New([]byte("vm"), db)
	bootstrappingDB := prefixdb.New([]byte("bs"), db)

	blocked, err := queue.New(bootstrappingDB)
	if err != nil {
		return nil, err
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	// Initialize the VM
	if err := vm.Initialize(ctx, vmDB, genesisData, msgChan, fxs); err != nil {
		return nil, err
	}

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}
	sender.Initialize(ctx, m.Net, m.ManagerConfig.Router, m.TimeoutManager)

	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	// The engine handles consensus
	engine := &smeng.Transitive{}
	if err := engine.Initialize(smeng.Config{
		Config: smbootstrap.Config{
			Config: common.Config{
				Ctx:          ctx,
				Validators:   validators,
				Beacons:      beacons,
				SampleK:      sampleK,
				StartupAlpha: (3*bootstrapWeight + 3) / 4,
				Alpha:        bootstrapWeight/2 + 1, // must be > 50%
				Sender:       &sender,
			},
			Blocked:      blocked,
			VM:           vm,
			Bootstrapped: m.unblockChains,
		},
		Params:    consensusParams,
		Consensus: &smcon.Topological{},
	}); err != nil {
		return nil, fmt.Errorf("error initializing snowman engine: %w", err)
	}

	// Asynchronously passes messages from the network to the consensus engine
	handler := &router.Handler{}
	handler.Initialize(
		engine,
		validators,
		msgChan,
		defaultChannelSize,
		m.MaxNonStakerPendingMsgs,
		m.StakerMSGPortion,
		m.StakerCPUPortion,
		fmt.Sprintf("%s_handler", consensusParams.Namespace),
		consensusParams.Metrics,
	)

	// Register health checks
	chainAlias, err := m.PrimaryAlias(ctx.ChainID)
	if err != nil {
		chainAlias = ctx.ChainID.String()
	}

	wrapperHc := &healthCheckWrapper{
		chain: chainAlias,
		lock:  &ctx.Lock,
		check: engine.Health,
	}
	if err := m.HealthService.RegisterCheck(wrapperHc); err != nil {
		return nil, fmt.Errorf("couldn't add health check for chain %s: %w", chainAlias, err)
	}

	return &chain{
		Engine:  engine,
		Handler: handler,
		VM:      vm,
		Ctx:     ctx,
	}, nil
}

func (m *manager) SubnetID(chainID ids.ID) (ids.ID, error) {
	m.chainsLock.Lock()
	defer m.chainsLock.Unlock()

	chain, exists := m.chains[chainID.Key()]
	if !exists {
		return ids.ID{}, errors.New("unknown chain ID")
	}
	return chain.Context().SubnetID, nil
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	chain, exists := m.chains[id.Key()]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	return chain.Engine().IsBootstrapped()
}

// Shutdown stops all the chains
func (m *manager) Shutdown() {
	m.ManagerConfig.Router.Shutdown()
}

// LookupVM returns the ID of the VM associated with an alias
func (m *manager) LookupVM(alias string) (ids.ID, error) { return m.VMManager.Lookup(alias) }

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *manager) notifyRegistrants(ctx *snow.Context, vm interface{}) {
	for _, registrant := range m.registrants {
		registrant.RegisterChain(ctx, vm)
	}
}

// Returns:
// 1) the alias that already exists, or the empty string if there is none
// 2) true iff there exists a chain such that the chain has an alias in [aliases]
func (m *manager) isChainWithAlias(aliases ...string) (string, bool) {
	for _, alias := range aliases {
		if _, err := m.Lookup(alias); err == nil {
			return alias, true
		}
	}
	return "", false
}

// Wraps a health check.
// Grabs [Lock] before executing the health check
type healthCheckWrapper struct {
	check func() (interface{}, error)

	// Grabs/releases this before/after health check func
	lock *sync.RWMutex

	// Alias/ID of chain this health check is for
	chain string
}

// Name is this health check's formatted name
func (hc *healthCheckWrapper) Name() string {
	return hc.chain
}

// Execute executes the health check function with the lock
func (hc *healthCheckWrapper) Execute() (interface{}, error) {
	hc.lock.Lock()
	defer hc.lock.Unlock()
	return hc.check()
}
