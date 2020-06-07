// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"fmt"
	"time"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/api/keystore"
	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/network"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/engine/avalanche"
	"github.com/ava-labs/gecko/snow/engine/avalanche/state"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/sender"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms"

	avacon "github.com/ava-labs/gecko/snow/consensus/avalanche"
	avaeng "github.com/ava-labs/gecko/snow/engine/avalanche"

	smcon "github.com/ava-labs/gecko/snow/consensus/snowman"
	smeng "github.com/ava-labs/gecko/snow/engine/snowman"
)

const (
	defaultChannelSize = 1000
	requestTimeout     = 4 * time.Second
	gossipFrequency    = 10 * time.Second
	shutdownTimeout    = 1 * time.Second
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

type manager struct {
	// Note: The string representation of a chain's ID is also considered to be an alias of the chain
	// That is, [chainID].String() is an alias for the chain, too
	ids.Aliaser

	stakingEnabled  bool // True iff the network has staking enabled
	log             logging.Logger
	logFactory      logging.Factory
	vmManager       vms.Manager // Manage mappings from vm ID --> vm
	decisionEvents  *triggers.EventDispatcher
	consensusEvents *triggers.EventDispatcher
	db              database.Database
	chainRouter     router.Router      // Routes incoming messages to the appropriate chain
	net             network.Network    // Sends consensus messages to other validators
	timeoutManager  *timeout.Manager   // Manages request timeouts when sending messages to other validators
	consensusParams avacon.Parameters  // The consensus parameters (alpha, beta, etc.) for new chains
	validators      validators.Manager // Validators validating on this chain
	registrants     []Registrant       // Those notified when a chain is created
	nodeID          ids.ShortID        // The ID of this node
	networkID       uint32             // ID of the network this node is connected to
	server          *api.Server        // Handles HTTP API calls
	keystore        *keystore.Keystore
	sharedMemory    *atomic.SharedMemory

	unblocked     bool
	blockedChains []ChainParameters
}

// New returns a new Manager where:
//     <db> is this node's database
//     <sender> sends messages to other validators
//     <validators> validate this chain
// TODO: Make this function take less arguments
func New(
	stakingEnabled bool,
	log logging.Logger,
	logFactory logging.Factory,
	vmManager vms.Manager,
	decisionEvents *triggers.EventDispatcher,
	consensusEvents *triggers.EventDispatcher,
	db database.Database,
	router router.Router,
	net network.Network,
	consensusParams avacon.Parameters,
	validators validators.Manager,
	nodeID ids.ShortID,
	networkID uint32,
	server *api.Server,
	keystore *keystore.Keystore,
	sharedMemory *atomic.SharedMemory,
) Manager {
	timeoutManager := timeout.Manager{}
	timeoutManager.Initialize(requestTimeout)
	go log.RecoverAndPanic(timeoutManager.Dispatch)

	router.Initialize(log, &timeoutManager, gossipFrequency, shutdownTimeout)

	m := &manager{
		stakingEnabled:  stakingEnabled,
		log:             log,
		logFactory:      logFactory,
		vmManager:       vmManager,
		decisionEvents:  decisionEvents,
		consensusEvents: consensusEvents,
		db:              db,
		chainRouter:     router,
		net:             net,
		timeoutManager:  &timeoutManager,
		consensusParams: consensusParams,
		validators:      validators,
		nodeID:          nodeID,
		networkID:       networkID,
		server:          server,
		keystore:        keystore,
		sharedMemory:    sharedMemory,
	}
	m.Initialize()
	return m
}

// Router that this chain manager is using to route consensus messages to chains
func (m *manager) Router() router.Router { return m.chainRouter }

// Create a chain
func (m *manager) CreateChain(chain ChainParameters) {
	if !m.unblocked {
		m.blockedChains = append(m.blockedChains, chain)
	} else {
		m.ForceCreateChain(chain)
	}
}

// Create a chain
func (m *manager) ForceCreateChain(chain ChainParameters) {
	m.log.Info("creating chain:\n"+
		"    ID: %s\n"+
		"    VMID:%s",
		chain.ID,
		chain.VMAlias,
	)

	// Assert that there isn't already a chain with an alias in [chain].Aliases
	// (Recall that the string repr. of a chain's ID is also an alias for a chain)
	if alias, isRepeat := m.isChainWithAlias(chain.ID.String()); isRepeat {
		m.log.Error("there is already a chain with alias '%s'. Chain not created.", alias)
		return
	}

	vmID, err := m.vmManager.Lookup(chain.VMAlias)
	if err != nil {
		m.log.Error("error while looking up VM: %s", err)
		return
	}

	// Get a factory for the vm we want to use on our chain
	vmFactory, err := m.vmManager.GetVMFactory(vmID)
	if err != nil {
		m.log.Error("error while getting vmFactory: %s", err)
		return
	}

	// Create the chain
	vm, err := vmFactory.New()
	if err != nil {
		m.log.Error("error while creating vm: %s", err)
		return
	}
	// TODO: Shutdown VM if an error occurs

	fxs := make([]*common.Fx, len(chain.FxAliases))
	for i, fxAlias := range chain.FxAliases {
		fxID, err := m.vmManager.Lookup(fxAlias)
		if err != nil {
			m.log.Error("error while looking up Fx: %s", err)
			return
		}

		// Get a factory for the fx we want to use on our chain
		fxFactory, err := m.vmManager.GetVMFactory(fxID)
		if err != nil {
			m.log.Error("error while getting fxFactory: %s", err)
			return
		}

		fx, err := fxFactory.New()
		if err != nil {
			m.log.Error("error while creating fx: %s", err)
			return
		}

		// Create the fx
		fxs[i] = &common.Fx{
			ID: fxID,
			Fx: fx,
		}
	}

	// Create the log and context of the chain
	chainLog, err := m.logFactory.MakeChain(chain.ID, "")
	if err != nil {
		m.log.Error("error while creating chain's log %s", err)
		return
	}

	ctx := &snow.Context{
		NetworkID:           m.networkID,
		ChainID:             chain.ID,
		Log:                 chainLog,
		DecisionDispatcher:  m.decisionEvents,
		ConsensusDispatcher: m.consensusEvents,
		NodeID:              m.nodeID,
		HTTP:                m.server,
		Keystore:            m.keystore.NewBlockchainKeyStore(chain.ID),
		SharedMemory:        m.sharedMemory.NewBlockchainSharedMemory(chain.ID),
		BCLookup:            m,
	}
	consensusParams := m.consensusParams
	if alias, err := m.PrimaryAlias(ctx.ChainID); err == nil {
		consensusParams.Namespace = fmt.Sprintf("gecko_%s", alias)
	} else {
		consensusParams.Namespace = fmt.Sprintf("gecko_%s", ctx.ChainID)
	}

	// The validators of this blockchain
	var validators validators.Set // Validators validating this blockchain
	var ok bool
	if m.stakingEnabled {
		validators, ok = m.validators.GetValidatorSet(chain.SubnetID)
	} else { // Staking is disabled. Every peer validates every subnet.
		validators, ok = m.validators.GetValidatorSet(ids.Empty) // ids.Empty is the default subnet ID. TODO: Move to const package so we can use it here.
	}
	if !ok {
		m.log.Error("couldn't get validator set of subnet with ID %s. The subnet may not exist", chain.SubnetID)
		return
	}

	beacons := validators
	if chain.CustomBeacons != nil {
		beacons = chain.CustomBeacons
	}

	switch vm := vm.(type) {
	case avalanche.DAGVM:
		err := m.createAvalancheChain(
			ctx,
			chain.GenesisData,
			validators,
			beacons,
			vm,
			fxs,
			consensusParams,
		)
		if err != nil {
			m.log.Error("error while creating new avalanche vm %s", err)
			return
		}
	case smeng.ChainVM:
		err := m.createSnowmanChain(
			ctx,
			chain.GenesisData,
			validators,
			beacons,
			vm,
			fxs,
			consensusParams.Parameters,
		)
		if err != nil {
			m.log.Error("error while creating new snowman vm %s", err)
			return
		}
	default:
		m.log.Error("the vm should have type avalanche.DAGVM or snowman.ChainVM. Chain not created")
		return
	}

	// Associate the newly created chain with its default alias
	m.log.AssertNoError(m.Alias(chain.ID, chain.ID.String()))

	// Notify those that registered to be notified when a new chain is created
	m.notifyRegistrants(ctx, vm)
}

// Implements Manager.AddRegistrant
func (m *manager) AddRegistrant(r Registrant) { m.registrants = append(m.registrants, r) }

func (m *manager) unblockChains() {
	m.unblocked = true
	blocked := m.blockedChains
	m.blockedChains = nil
	for _, chain := range blocked {
		m.ForceCreateChain(chain)
	}
}

// Create a DAG-based blockchain that uses Avalanche
func (m *manager) createAvalancheChain(
	ctx *snow.Context,
	genesisData []byte,
	validators,
	beacons validators.Set,
	vm avalanche.DAGVM,
	fxs []*common.Fx,
	consensusParams avacon.Parameters,
) error {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	db := prefixdb.New(ctx.ChainID.Bytes(), m.db)
	vmDB := prefixdb.New([]byte("vm"), db)
	vertexDB := prefixdb.New([]byte("vertex"), db)
	vertexBootstrappingDB := prefixdb.New([]byte("vertex_bootstrapping"), db)
	txBootstrappingDB := prefixdb.New([]byte("tx_bootstrapping"), db)

	vtxBlocker, err := queue.New(vertexBootstrappingDB)
	if err != nil {
		return err
	}
	txBlocker, err := queue.New(txBootstrappingDB)
	if err != nil {
		return err
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	if err := vm.Initialize(ctx, vmDB, genesisData, msgChan, fxs); err != nil {
		return fmt.Errorf("error during vm's Initialize: %w", err)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxState := &state.Serializer{}
	vtxState.Initialize(ctx, vm, vertexDB)

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}
	sender.Initialize(ctx, m.net, m.chainRouter, m.timeoutManager)

	// The engine handles consensus
	engine := avaeng.Transitive{
		Config: avaeng.Config{
			BootstrapConfig: avaeng.BootstrapConfig{
				Config: common.Config{
					Context: ctx,
				},
			},
		},
	}

	bootstrapWeight := uint64(0)
	for _, beacon := range beacons.List() {
		newWeight, err := math.Add64(bootstrapWeight, beacon.Weight())
		if err != nil {
			return err
		}
		bootstrapWeight = newWeight
	}

	engine.Initialize(avaeng.Config{
		BootstrapConfig: avaeng.BootstrapConfig{
			Config: common.Config{
				Context:    ctx,
				Validators: validators,
				Beacons:    beacons,
				Alpha:      bootstrapWeight/2 + 1, // must be > 50%
				Sender:     &sender,
			},
			VtxBlocked: vtxBlocker,
			TxBlocked:  txBlocker,
			State:      vtxState,
			VM:         vm,
		},
		Params:    consensusParams,
		Consensus: &avacon.Topological{},
	})

	// Asynchronously passes messages from the network to the consensus engine
	handler := &router.Handler{}
	handler.Initialize(
		&engine,
		msgChan,
		defaultChannelSize,
		fmt.Sprintf("%s_handler", consensusParams.Namespace),
		consensusParams.Metrics,
	)

	// Allows messages to be routed to the new chain
	m.chainRouter.AddChain(handler)
	go ctx.Log.RecoverAndPanic(handler.Dispatch)

	reqWeight := (3*bootstrapWeight + 3) / 4
	if reqWeight == 0 {
		engine.Startup()
	} else {
		go m.net.RegisterHandler(&awaiter{
			vdrs:      beacons,
			reqWeight: reqWeight, // 75% must be connected to
			ctx:       ctx,
			eng:       &engine,
		})
	}

	return nil
}

// Create a linear chain using the Snowman consensus engine
func (m *manager) createSnowmanChain(
	ctx *snow.Context,
	genesisData []byte,
	validators,
	beacons validators.Set,
	vm smeng.ChainVM,
	fxs []*common.Fx,
	consensusParams snowball.Parameters,
) error {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	db := prefixdb.New(ctx.ChainID.Bytes(), m.db)
	vmDB := prefixdb.New([]byte("vm"), db)
	bootstrappingDB := prefixdb.New([]byte("bootstrapping"), db)

	blocked, err := queue.New(bootstrappingDB)
	if err != nil {
		return err
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan common.Message, defaultChannelSize)

	// Initialize the VM
	if err := vm.Initialize(ctx, vmDB, genesisData, msgChan, fxs); err != nil {
		return err
	}

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}
	sender.Initialize(ctx, m.net, m.chainRouter, m.timeoutManager)

	bootstrapWeight := uint64(0)
	for _, beacon := range beacons.List() {
		newWeight, err := math.Add64(bootstrapWeight, beacon.Weight())
		if err != nil {
			return err
		}
		bootstrapWeight = newWeight
	}

	// The engine handles consensus
	engine := smeng.Transitive{}
	engine.Initialize(smeng.Config{
		BootstrapConfig: smeng.BootstrapConfig{
			Config: common.Config{
				Context:    ctx,
				Validators: validators,
				Beacons:    beacons,
				Alpha:      bootstrapWeight/2 + 1, // must be > 50%
				Sender:     &sender,
			},
			Blocked:      blocked,
			VM:           vm,
			Bootstrapped: m.unblockChains,
		},
		Params:    consensusParams,
		Consensus: &smcon.Topological{},
	})

	// Asynchronously passes messages from the network to the consensus engine
	handler := &router.Handler{}
	handler.Initialize(
		&engine,
		msgChan,
		defaultChannelSize,
		fmt.Sprintf("%s_handler", consensusParams.Namespace),
		consensusParams.Metrics,
	)

	// Allow incoming messages to be routed to the new chain
	m.chainRouter.AddChain(handler)
	go ctx.Log.RecoverAndPanic(handler.Dispatch)

	reqWeight := (3*bootstrapWeight + 3) / 4
	if reqWeight == 0 {
		engine.Startup()
	} else {
		go m.net.RegisterHandler(&awaiter{
			vdrs:      beacons,
			reqWeight: reqWeight, // 75% must be connected to
			ctx:       ctx,
			eng:       &engine,
		})
	}
	return nil
}

// Shutdown stops all the chains
func (m *manager) Shutdown() { m.chainRouter.Shutdown() }

// LookupVM returns the ID of the VM associated with an alias
func (m *manager) LookupVM(alias string) (ids.ID, error) { return m.vmManager.Lookup(alias) }

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
