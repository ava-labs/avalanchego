// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	coreth "github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/auth"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/rocksdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/indexer"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/registry"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ipcsapi "github.com/ava-labs/avalanchego/api/ipcs"
)

var (
	genesisHashKey  = []byte("genesisID")
	indexerDBPrefix = []byte{0x00}

	errInvalidTLSKey = errors.New("invalid TLS key")
	errShuttingDown  = errors.New("server shutting down")
)

// Node is an instance of an Avalanche node.
type Node struct {
	Log        logging.Logger
	LogFactory logging.Factory

	// This node's unique ID used when communicating with other nodes
	// (in consensus, for example)
	ID ids.NodeID

	// Storage for this node
	DBManager manager.Manager
	DB        database.Database

	// Profiles the process. Nil if continuous profiling is disabled.
	profiler profiler.ContinuousProfiler

	// Indexes blocks, transactions and blocks
	indexer indexer.Indexer

	// Handles calls to Keystore API
	keystore keystore.Keystore

	// Manages shared memory
	sharedMemory atomic.Memory

	// Monitors node health and runs health checks
	health health.Health

	// Build and parse messages, for both network layer and chain manager
	msgCreator message.Creator

	// Manages creation of blockchains and routing messages to them
	chainManager chains.Manager

	// Manages validator benching
	benchlistManager benchlist.Manager

	uptimeCalculator uptime.LockedCalculator

	// dispatcher for events as they happen in consensus
	DecisionAcceptorGroup  snow.AcceptorGroup
	ConsensusAcceptorGroup snow.AcceptorGroup

	IPCs *ipcs.ChainIPCs

	// Net runs the networking stack
	networkNamespace string
	Net              network.Network

	// this node's initial connections to the network
	beacons validators.Set

	// current validators of the network
	vdrs validators.Manager

	// Handles HTTP API calls
	APIServer server.Server

	// This node's configuration
	Config *Config

	// ensures that we only close the node once.
	shutdownOnce sync.Once

	// True if node is shutting down or is done shutting down
	shuttingDown utils.AtomicBool

	// Sets the exit code
	shuttingDownExitCode utils.AtomicInterface

	// Incremented only once on initialization.
	// Decremented when node is done shutting down.
	DoneShuttingDown sync.WaitGroup

	// Metrics Registerer
	MetricsRegisterer *prometheus.Registry
	MetricsGatherer   metrics.MultiGatherer

	// VM endpoint registry
	VMRegistry registry.VMRegistry

	resourceManager resource.Manager

	// Tracks the CPU/disk usage caused by processing
	// messages of each peer.
	resourceTracker tracker.ResourceTracker

	// Specifies how much CPU usage each peer can cause before
	// we rate-limit them.
	cpuTargeter tracker.Targeter

	// Specifies how much disk usage each peer can cause before
	// we rate-limit them.
	diskTargeter tracker.Targeter
}

/*
 ******************************************************************************
 *************************** P2P Networking Section ***************************
 ******************************************************************************
 */

// Initialize the networking layer.
// Assumes [n.CPUTracker] and [n.CPUTargeter] have been initialized.
func (n *Node) initNetworking(primaryNetVdrs validators.Set) error {
	currentIPPort := n.Config.IPPort.IPPort()
	listener, err := net.Listen(constants.NetworkType, fmt.Sprintf(":%d", currentIPPort.Port))
	if err != nil {
		return err
	}
	// Wrap listener so it will only accept a certain number of incoming connections per second
	listener = throttling.NewThrottledListener(listener, n.Config.NetworkConfig.ThrottlerConfig.MaxInboundConnsPerSec)

	ipPort, err := ips.ToIPPort(listener.Addr().String())
	if err != nil {
		n.Log.Info("this node's IP is set to: %q", currentIPPort)
	} else {
		ipPort = ips.IPPort{
			IP:   currentIPPort.IP,
			Port: ipPort.Port,
		}
		n.Log.Info("this node's IP is set to: %q", ipPort)
	}

	tlsKey, ok := n.Config.StakingTLSCert.PrivateKey.(crypto.Signer)
	if !ok {
		return errInvalidTLSKey
	}

	tlsConfig := peer.TLSConfig(n.Config.StakingTLSCert)

	// Configure benchlist
	n.Config.BenchlistConfig.Validators = n.vdrs
	n.Config.BenchlistConfig.Benchable = n.Config.ConsensusRouter
	n.Config.BenchlistConfig.StakingEnabled = n.Config.EnableStaking
	n.benchlistManager = benchlist.NewManager(&n.Config.BenchlistConfig)

	n.uptimeCalculator = uptime.NewLockedCalculator()

	consensusRouter := n.Config.ConsensusRouter
	if !n.Config.EnableStaking {
		if err := primaryNetVdrs.AddWeight(n.ID, n.Config.DisabledStakingWeight); err != nil {
			return err
		}
		consensusRouter = &insecureValidatorManager{
			Router: consensusRouter,
			vdrs:   primaryNetVdrs,
			weight: n.Config.DisabledStakingWeight,
		}
	}

	bootstrapWeight := n.beacons.Weight()
	reqWeight := (3*bootstrapWeight + 3) / 4

	if reqWeight > 0 {
		// Set a timer that will fire after a given timeout unless we connect
		// to a sufficient portion of stake-weighted nodes. If the timeout
		// fires, the node will shutdown.
		timer := timer.NewTimer(func() {
			// If the timeout fires and we're already shutting down, nothing to do.
			if !n.shuttingDown.GetValue() {
				n.Log.Debug("node %s failed to connect to bootstrap nodes %s in time", n.ID, n.beacons)
				n.Log.Fatal("Failed to connect to bootstrap nodes. Node shutting down...")
				go n.Shutdown(1)
			}
		})

		go timer.Dispatch()
		timer.SetTimeoutIn(n.Config.BootstrapBeaconConnectionTimeout)

		consensusRouter = &beaconManager{
			Router:         consensusRouter,
			timer:          timer,
			beacons:        n.beacons,
			requiredWeight: reqWeight,
		}
	}

	// add node configs to network config
	n.Config.NetworkConfig.Namespace = n.networkNamespace
	n.Config.NetworkConfig.MyNodeID = n.ID
	n.Config.NetworkConfig.MyIPPort = n.Config.IPPort
	n.Config.NetworkConfig.NetworkID = n.Config.NetworkID
	n.Config.NetworkConfig.Validators = n.vdrs
	n.Config.NetworkConfig.Beacons = n.beacons
	n.Config.NetworkConfig.TLSConfig = tlsConfig
	n.Config.NetworkConfig.TLSKey = tlsKey
	n.Config.NetworkConfig.WhitelistedSubnets = n.Config.WhitelistedSubnets
	n.Config.NetworkConfig.UptimeCalculator = n.uptimeCalculator
	n.Config.NetworkConfig.UptimeRequirement = n.Config.UptimeRequirement
	n.Config.NetworkConfig.ResourceTracker = n.resourceTracker
	n.Config.NetworkConfig.CPUTargeter = n.cpuTargeter
	n.Config.NetworkConfig.DiskTargeter = n.diskTargeter

	n.Net, err = network.NewNetwork(
		&n.Config.NetworkConfig,
		n.msgCreator,
		n.MetricsRegisterer,
		n.Log,
		listener,
		dialer.NewDialer(constants.NetworkType, n.Config.NetworkConfig.DialerConfig, n.Log),
		consensusRouter,
		n.benchlistManager,
	)

	return err
}

type insecureValidatorManager struct {
	router.Router
	vdrs   validators.Set
	weight uint64
}

func (i *insecureValidatorManager) Connected(vdrID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	if constants.PrimaryNetworkID == subnetID {
		_ = i.vdrs.AddWeight(vdrID, i.weight)
	}
	i.Router.Connected(vdrID, nodeVersion, subnetID)
}

func (i *insecureValidatorManager) Disconnected(vdrID ids.NodeID) {
	// Shouldn't error unless the set previously had an error, which should
	// never happen as described above
	_ = i.vdrs.RemoveWeight(vdrID, i.weight)
	i.Router.Disconnected(vdrID)
}

type beaconManager struct {
	router.Router
	timer          *timer.Timer
	beacons        validators.Set
	requiredWeight uint64
	totalWeight    uint64
}

func (b *beaconManager) Connected(vdrID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	if constants.PrimaryNetworkID == subnetID {
		// TODO: this is always 1, beacons can be reduced to ShortSet?
		weight, ok := b.beacons.GetWeight(vdrID)
		if !ok {
			b.Router.Connected(vdrID, nodeVersion, subnetID)
			return
		}
		weight, err := math.Add64(weight, b.totalWeight)
		if err != nil {
			b.timer.Cancel()
			b.Router.Connected(vdrID, nodeVersion, subnetID)
			return
		}
		b.totalWeight = weight
		if b.totalWeight >= b.requiredWeight {
			b.timer.Cancel()
		}
	}
	b.Router.Connected(vdrID, nodeVersion, subnetID)
}

func (b *beaconManager) Disconnected(vdrID ids.NodeID) {
	if weight, ok := b.beacons.GetWeight(vdrID); ok {
		// TODO: Account for weight changes in a more robust manner.

		// Sub64 should rarely error since only validators that have added their
		// weight can become disconnected. Because it is possible that there are
		// changes to the validators set, we utilize that Sub64 returns 0 on
		// error.
		b.totalWeight, _ = math.Sub64(b.totalWeight, weight)
	}
	b.Router.Disconnected(vdrID)
}

// Dispatch starts the node's servers.
// Returns when the node exits.
func (n *Node) Dispatch() error {
	// Start the HTTP API server
	go n.Log.RecoverAndPanic(func() {
		var err error
		if n.Config.HTTPSEnabled {
			n.Log.Debug("initializing API server with TLS")
			err = n.APIServer.DispatchTLS(n.Config.HTTPSCert, n.Config.HTTPSKey)
		} else {
			n.Log.Debug("initializing API server without TLS")
			err = n.APIServer.Dispatch()
		}
		// When [n].Shutdown() is called, [n.APIServer].Close() is called.
		// This causes [n.APIServer].Dispatch() to return an error.
		// If that happened, don't log/return an error here.
		if !n.shuttingDown.GetValue() {
			n.Log.Fatal("API server dispatch failed with %s", err)
		}
		// If the API server isn't running, shut down the node.
		// If node is already shutting down, this does nothing.
		n.Shutdown(1)
	})

	// Add state sync nodes to the peer network
	for i, peerIP := range n.Config.StateSyncIPs {
		n.Net.ManuallyTrack(n.Config.StateSyncIDs[i], peerIP)
	}

	// Add bootstrap nodes to the peer network
	for i, peerIP := range n.Config.BootstrapIPs {
		n.Net.ManuallyTrack(n.Config.BootstrapIDs[i], peerIP)
	}

	// Start P2P connections
	err := n.Net.Dispatch()

	// If the P2P server isn't running, shut down the node.
	// If node is already shutting down, this does nothing.
	n.Shutdown(1)

	// Wait until the node is done shutting down before returning
	n.DoneShuttingDown.Wait()
	return err
}

/*
 ******************************************************************************
 *********************** End P2P Networking Section ***************************
 ******************************************************************************
 */

func (n *Node) initDatabase() error {
	// start the db manager
	var (
		dbManager manager.Manager
		err       error
	)
	switch n.Config.DatabaseConfig.Name {
	case rocksdb.Name:
		path := filepath.Join(n.Config.DatabaseConfig.Path, rocksdb.Name)
		dbManager, err = manager.NewRocksDB(path, n.Config.DatabaseConfig.Config, n.Log, version.CurrentDatabase, "db_internal", n.MetricsRegisterer)
	case leveldb.Name:
		dbManager, err = manager.NewLevelDB(n.Config.DatabaseConfig.Path, n.Config.DatabaseConfig.Config, n.Log, version.CurrentDatabase, "db_internal", n.MetricsRegisterer)
	case memdb.Name:
		dbManager = manager.NewMemDB(version.CurrentDatabase)
	default:
		err = fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			n.Config.DatabaseConfig.Name,
			leveldb.Name,
			rocksdb.Name,
			memdb.Name,
		)
	}
	if err != nil {
		return err
	}

	meterDBManager, err := dbManager.NewMeterDBManager("db", n.MetricsRegisterer)
	if err != nil {
		return err
	}

	n.DBManager = meterDBManager

	currentDB := dbManager.Current()
	n.Log.Info("current database version: %s", currentDB.Version)
	n.DB = currentDB.Database

	rawExpectedGenesisHash := hashing.ComputeHash256(n.Config.GenesisBytes)

	rawGenesisHash, err := n.DB.Get(genesisHashKey)
	if err == database.ErrNotFound {
		rawGenesisHash = rawExpectedGenesisHash
		err = n.DB.Put(genesisHashKey, rawGenesisHash)
	}
	if err != nil {
		return err
	}

	genesisHash, err := ids.ToID(rawGenesisHash)
	if err != nil {
		return err
	}
	expectedGenesisHash, err := ids.ToID(rawExpectedGenesisHash)
	if err != nil {
		return err
	}

	if genesisHash != expectedGenesisHash {
		return fmt.Errorf("db contains invalid genesis hash. DB Genesis: %s Generated Genesis: %s", genesisHash, expectedGenesisHash)
	}
	return nil
}

// Set the node IDs of the peers this node should first connect to
func (n *Node) initBeacons() error {
	n.beacons = validators.NewSet()
	for _, peerID := range n.Config.BootstrapIDs {
		if err := n.beacons.AddWeight(peerID, 1); err != nil {
			return err
		}
	}
	return nil
}

// Create the EventDispatcher used for hooking events
// into the general process flow.
func (n *Node) initEventDispatchers() {
	n.DecisionAcceptorGroup = snow.NewAcceptorGroup(n.Log)
	n.ConsensusAcceptorGroup = snow.NewAcceptorGroup(n.Log)
}

func (n *Node) initIPCs() error {
	chainIDs := make([]ids.ID, len(n.Config.IPCDefaultChainIDs))
	for i, chainID := range n.Config.IPCDefaultChainIDs {
		id, err := ids.FromString(chainID)
		if err != nil {
			return err
		}
		chainIDs[i] = id
	}

	var err error
	n.IPCs, err = ipcs.NewChainIPCs(n.Log, n.Config.IPCPath, n.Config.NetworkID, n.ConsensusAcceptorGroup, n.DecisionAcceptorGroup, chainIDs)
	return err
}

// Initialize [n.indexer].
// Should only be called after [n.DB], [n.DecisionAcceptorGroup],
// [n.ConsensusAcceptorGroup], [n.Log], [n.APIServer], [n.chainManager] are
// initialized
func (n *Node) initIndexer() error {
	txIndexerDB := prefixdb.New(indexerDBPrefix, n.DB)
	var err error
	n.indexer, err = indexer.NewIndexer(indexer.Config{
		IndexingEnabled:        n.Config.IndexAPIEnabled,
		AllowIncompleteIndex:   n.Config.IndexAllowIncomplete,
		DB:                     txIndexerDB,
		Log:                    n.Log,
		DecisionAcceptorGroup:  n.DecisionAcceptorGroup,
		ConsensusAcceptorGroup: n.ConsensusAcceptorGroup,
		APIServer:              n.APIServer,
		ShutdownF:              func() { n.Shutdown(0) }, // TODO put exit code here
	})
	if err != nil {
		return fmt.Errorf("couldn't create index for txs: %w", err)
	}

	// Chain manager will notify indexer when a chain is created
	n.chainManager.AddRegistrant(n.indexer)

	return nil
}

// Initializes the Platform chain.
// Its genesis data specifies the other chains that should be created.
func (n *Node) initChains(genesisBytes []byte) {
	n.Log.Info("initializing chains")

	// Create the Platform Chain
	n.chainManager.ForceCreateChain(chains.ChainParameters{
		ID:            constants.PlatformChainID,
		SubnetID:      constants.PrimaryNetworkID,
		GenesisData:   genesisBytes, // Specifies other chains to create
		VMAlias:       constants.PlatformVMID.String(),
		CustomBeacons: n.beacons,
	})
}

// initAPIServer initializes the server that handles HTTP calls
func (n *Node) initAPIServer() error {
	n.Log.Info("initializing API server")
	n.APIServer = server.New()

	if !n.Config.APIRequireAuthToken {
		n.APIServer.Initialize(
			n.Log,
			n.LogFactory,
			n.Config.HTTPHost,
			n.Config.HTTPPort,
			n.Config.APIAllowedOrigins,
			n.Config.ShutdownTimeout,
			n.ID,
		)
		return nil
	}

	a, err := auth.New(n.Log, "auth", n.Config.APIAuthPassword)
	if err != nil {
		return err
	}

	n.APIServer.Initialize(
		n.Log,
		n.LogFactory,
		n.Config.HTTPHost,
		n.Config.HTTPPort,
		n.Config.APIAllowedOrigins,
		n.Config.ShutdownTimeout,
		n.ID,
		a,
	)

	// only create auth service if token authorization is required
	n.Log.Info("API authorization is enabled. Auth tokens must be passed in the header of API requests, except requests to the auth service.")
	authService, err := a.CreateHandler()
	if err != nil {
		return err
	}
	handler := &common.HTTPHandler{
		LockOptions: common.NoLock,
		Handler:     authService,
	}
	return n.APIServer.AddRoute(handler, &sync.RWMutex{}, "auth", "")
}

// Add the default VM aliases
func (n *Node) addDefaultVMAliases() error {
	n.Log.Info("adding the default VM aliases")
	vmAliases := genesis.GetVMAliases()

	for vmID, aliases := range vmAliases {
		for _, alias := range aliases {
			if err := n.Config.VMManager.Alias(vmID, alias); err != nil {
				return err
			}
		}
	}
	return nil
}

// Create the chainManager and register the following VMs:
// AVM, Simple Payments DAG, Simple Payments Chain, and Platform VM
// Assumes n.DBManager, n.vdrs all initialized (non-nil)
func (n *Node) initChainManager(avaxAssetID ids.ID) error {
	createAVMTx, err := genesis.VMGenesis(n.Config.GenesisBytes, constants.AVMID)
	if err != nil {
		return err
	}
	xChainID := createAVMTx.ID()

	createEVMTx, err := genesis.VMGenesis(n.Config.GenesisBytes, constants.EVMID)
	if err != nil {
		return err
	}
	cChainID := createEVMTx.ID()

	// If any of these chains die, the node shuts down
	criticalChains := ids.Set{}
	criticalChains.Add(
		constants.PlatformChainID,
		xChainID,
		cChainID,
	)

	// Manages network timeouts
	timeoutManager, err := timeout.NewManager(
		&n.Config.AdaptiveTimeoutConfig,
		n.benchlistManager,
		"requests",
		n.MetricsRegisterer,
	)
	if err != nil {
		return err
	}
	go n.Log.RecoverAndPanic(timeoutManager.Dispatch)

	// Routes incoming messages from peers to the appropriate chain
	err = n.Config.ConsensusRouter.Initialize(
		n.ID,
		n.Log,
		n.msgCreator,
		timeoutManager,
		n.Config.ConsensusShutdownTimeout,
		criticalChains,
		n.Config.WhitelistedSubnets,
		n.Shutdown,
		n.Config.RouterHealthConfig,
		"requests",
		n.MetricsRegisterer,
	)
	if err != nil {
		return fmt.Errorf("couldn't initialize chain router: %w", err)
	}

	n.chainManager = chains.New(&chains.ManagerConfig{
		StakingEnabled:                          n.Config.EnableStaking,
		StakingCert:                             n.Config.StakingTLSCert,
		Log:                                     n.Log,
		LogFactory:                              n.LogFactory,
		VMManager:                               n.Config.VMManager,
		DecisionAcceptorGroup:                   n.DecisionAcceptorGroup,
		ConsensusAcceptorGroup:                  n.ConsensusAcceptorGroup,
		DBManager:                               n.DBManager,
		MsgCreator:                              n.msgCreator,
		Router:                                  n.Config.ConsensusRouter,
		Net:                                     n.Net,
		ConsensusParams:                         n.Config.ConsensusParams,
		Validators:                              n.vdrs,
		NodeID:                                  n.ID,
		NetworkID:                               n.Config.NetworkID,
		Server:                                  n.APIServer,
		Keystore:                                n.keystore,
		AtomicMemory:                            &n.sharedMemory,
		AVAXAssetID:                             avaxAssetID,
		XChainID:                                xChainID,
		CriticalChains:                          criticalChains,
		TimeoutManager:                          timeoutManager,
		Health:                                  n.health,
		WhitelistedSubnets:                      n.Config.WhitelistedSubnets,
		RetryBootstrap:                          n.Config.RetryBootstrap,
		RetryBootstrapWarnFrequency:             n.Config.RetryBootstrapWarnFrequency,
		ShutdownNodeFunc:                        n.Shutdown,
		MeterVMEnabled:                          n.Config.MeterVMEnabled,
		Metrics:                                 n.MetricsGatherer,
		SubnetConfigs:                           n.Config.SubnetConfigs,
		ChainConfigs:                            n.Config.ChainConfigs,
		ConsensusGossipFrequency:                n.Config.ConsensusGossipFrequency,
		GossipConfig:                            n.Config.GossipConfig,
		BootstrapMaxTimeGetAncestors:            n.Config.BootstrapMaxTimeGetAncestors,
		BootstrapAncestorsMaxContainersSent:     n.Config.BootstrapAncestorsMaxContainersSent,
		BootstrapAncestorsMaxContainersReceived: n.Config.BootstrapAncestorsMaxContainersReceived,
		ApricotPhase4Time:                       version.GetApricotPhase4Time(n.Config.NetworkID),
		ApricotPhase4MinPChainHeight:            version.GetApricotPhase4MinPChainHeight(n.Config.NetworkID),
		ResourceTracker:                         n.resourceTracker,
		StateSyncBeacons:                        n.Config.StateSyncIDs,
	})

	// Notify the API server when new chains are created
	n.chainManager.AddRegistrant(n.APIServer)
	return nil
}

// initVMs initializes the VMs Avalanche supports + any additional vms installed as plugins.
func (n *Node) initVMs() error {
	n.Log.Info("initializing VMs")

	vdrs := n.vdrs

	// If staking is disabled, ignore updates to Subnets' validator sets
	// Instead of updating node's validator manager, platform chain makes changes
	// to its own local validator manager (which isn't used for sampling)
	if !n.Config.EnableStaking {
		vdrs = validators.NewManager()
	}

	vmRegisterer := registry.NewVMRegisterer(registry.VMRegistererConfig{
		APIServer: n.APIServer,
		Log:       n.Log,
		VMManager: n.Config.VMManager,
	})

	// Register the VMs that Avalanche supports
	errs := wrappers.Errs{}
	errs.Add(
		vmRegisterer.Register(constants.PlatformVMID, &platformvm.Factory{
			Config: config.Config{
				Chains:                 n.chainManager,
				Validators:             vdrs,
				SubnetTracker:          n.Net,
				UptimeLockedCalculator: n.uptimeCalculator,
				StakingEnabled:         n.Config.EnableStaking,
				WhitelistedSubnets:     n.Config.WhitelistedSubnets,
				TxFee:                  n.Config.TxFee,
				CreateAssetTxFee:       n.Config.CreateAssetTxFee,
				CreateSubnetTxFee:      n.Config.CreateSubnetTxFee,
				CreateBlockchainTxFee:  n.Config.CreateBlockchainTxFee,
				UptimePercentage:       n.Config.UptimeRequirement,
				MinValidatorStake:      n.Config.MinValidatorStake,
				MaxValidatorStake:      n.Config.MaxValidatorStake,
				MinDelegatorStake:      n.Config.MinDelegatorStake,
				MinDelegationFee:       n.Config.MinDelegationFee,
				MinStakeDuration:       n.Config.MinStakeDuration,
				MaxStakeDuration:       n.Config.MaxStakeDuration,
				RewardConfig:           n.Config.RewardConfig,
				ApricotPhase3Time:      version.GetApricotPhase3Time(n.Config.NetworkID),
				ApricotPhase4Time:      version.GetApricotPhase4Time(n.Config.NetworkID),
				ApricotPhase5Time:      version.GetApricotPhase5Time(n.Config.NetworkID),
			},
		}),
		vmRegisterer.Register(constants.AVMID, &avm.Factory{
			TxFee:            n.Config.TxFee,
			CreateAssetTxFee: n.Config.CreateAssetTxFee,
		}),
		vmRegisterer.Register(constants.EVMID, &coreth.Factory{}),
		n.Config.VMManager.RegisterFactory(secp256k1fx.ID, &secp256k1fx.Factory{}),
		n.Config.VMManager.RegisterFactory(nftfx.ID, &nftfx.Factory{}),
		n.Config.VMManager.RegisterFactory(propertyfx.ID, &propertyfx.Factory{}),
	)
	if errs.Errored() {
		return errs.Err
	}

	// initialize the vm registry
	n.VMRegistry = registry.NewVMRegistry(registry.VMRegistryConfig{
		VMGetter: registry.NewVMGetter(registry.VMGetterConfig{
			FileReader:      filesystem.NewReader(),
			Manager:         n.Config.VMManager,
			PluginDirectory: n.Config.PluginDir,
			CPUTracker:      n.resourceManager,
		}),
		VMRegisterer: vmRegisterer,
	})

	// register any vms that need to be installed as plugins from disk
	_, failedVMs, err := n.VMRegistry.Reload()
	for failedVM, err := range failedVMs {
		n.Log.Error("failed to register %s: %s", failedVM, err)
	}
	return err
}

// initSharedMemory initializes the shared memory for cross chain interation
func (n *Node) initSharedMemory() error {
	n.Log.Info("initializing SharedMemory")
	sharedMemoryDB := prefixdb.New([]byte("shared memory"), n.DB)
	return n.sharedMemory.Initialize(n.Log, sharedMemoryDB)
}

// initKeystoreAPI initializes the keystore service, which is an on-node wallet.
// Assumes n.APIServer is already set
func (n *Node) initKeystoreAPI() error {
	n.Log.Info("initializing keystore")
	keystoreDB := n.DBManager.NewPrefixDBManager([]byte("keystore"))
	n.keystore = keystore.New(n.Log, keystoreDB)
	keystoreHandler, err := n.keystore.CreateHandler()
	if err != nil {
		return err
	}
	if !n.Config.KeystoreAPIEnabled {
		n.Log.Info("skipping keystore API initialization because it has been disabled")
		return nil
	}
	n.Log.Info("initializing keystore API")
	handler := &common.HTTPHandler{
		LockOptions: common.NoLock,
		Handler:     keystoreHandler,
	}
	return n.APIServer.AddRoute(handler, &sync.RWMutex{}, "keystore", "")
}

// initMetricsAPI initializes the Metrics API
// Assumes n.APIServer is already set
func (n *Node) initMetricsAPI() error {
	n.MetricsRegisterer = prometheus.NewRegistry()
	n.MetricsGatherer = metrics.NewMultiGatherer()

	if !n.Config.MetricsAPIEnabled {
		n.Log.Info("skipping metrics API initialization because it has been disabled")
		return nil
	}

	if err := n.MetricsGatherer.Register(constants.PlatformName, n.MetricsRegisterer); err != nil {
		return err
	}

	// Current state of process metrics.
	processCollector := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
	if err := n.MetricsRegisterer.Register(processCollector); err != nil {
		return err
	}

	// Go process metrics using debug.GCStats.
	goCollector := collectors.NewGoCollector()
	if err := n.MetricsRegisterer.Register(goCollector); err != nil {
		return err
	}

	n.Log.Info("initializing metrics API")

	return n.APIServer.AddRoute(
		&common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler: promhttp.HandlerFor(
				n.MetricsGatherer,
				promhttp.HandlerOpts{},
			),
		},
		&sync.RWMutex{},
		"metrics",
		"",
	)
}

// initAdminAPI initializes the Admin API service
// Assumes n.log, n.chainManager, and n.ValidatorAPI already initialized
func (n *Node) initAdminAPI() error {
	// Convert node config to map
	configJSON, err := json.Marshal(n.Config)
	if err != nil {
		return fmt.Errorf("couldn't marshal config: %w", err)
	}
	n.Log.Info("node config:\n%s", configJSON)
	if !n.Config.AdminAPIEnabled {
		n.Log.Info("skipping admin API initialization because it has been disabled")
		return nil
	}
	n.Log.Info("initializing admin API")
	service, err := admin.NewService(
		admin.Config{
			Log:          n.Log,
			ChainManager: n.chainManager,
			HTTPServer:   n.APIServer,
			ProfileDir:   n.Config.ProfilerConfig.Dir,
			LogFactory:   n.LogFactory,
			NodeConfig:   n.Config,
			VMManager:    n.Config.VMManager,
			VMRegistry:   n.VMRegistry,
		},
	)
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(service, &sync.RWMutex{}, "admin", "")
}

// initProfiler initializes the continuous profiling
func (n *Node) initProfiler() {
	if !n.Config.ProfilerConfig.Enabled {
		n.Log.Info("skipping profiler initialization because it has been disabled")
		return
	}

	n.Log.Info("initializing continuous profiler")
	n.profiler = profiler.NewContinuous(
		filepath.Join(n.Config.ProfilerConfig.Dir, "continuous"),
		n.Config.ProfilerConfig.Freq,
		n.Config.ProfilerConfig.MaxNumFiles,
	)
	go n.Log.RecoverAndPanic(func() {
		err := n.profiler.Dispatch()
		if err != nil {
			n.Log.Fatal("continuous profiler failed with %s", err)
		}
		n.Shutdown(1)
	})
}

func (n *Node) initInfoAPI() error {
	if !n.Config.InfoAPIEnabled {
		n.Log.Info("skipping info API initialization because it has been disabled")
		return nil
	}

	n.Log.Info("initializing info API")

	primaryValidators, _ := n.vdrs.GetValidators(constants.PrimaryNetworkID)
	service, err := info.NewService(
		info.Parameters{
			Version:               version.CurrentApp,
			NodeID:                n.ID,
			NetworkID:             n.Config.NetworkID,
			TxFee:                 n.Config.TxFee,
			CreateAssetTxFee:      n.Config.CreateAssetTxFee,
			CreateSubnetTxFee:     n.Config.CreateSubnetTxFee,
			CreateBlockchainTxFee: n.Config.CreateBlockchainTxFee,
			VMManager:             n.Config.VMManager,
		},
		n.Log,
		n.chainManager,
		n.Config.VMManager,
		n.Config.NetworkConfig.MyIPPort,
		n.Net,
		primaryValidators,
		n.benchlistManager,
	)
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(service, &sync.RWMutex{}, "info", "")
}

// initHealthAPI initializes the Health API service
// Assumes n.Log, n.Net, n.APIServer, n.HTTPLog already initialized
func (n *Node) initHealthAPI() error {
	healthChecker, err := health.New(n.Log, n.MetricsRegisterer)
	if err != nil {
		return err
	}
	n.health = healthChecker

	if !n.Config.HealthAPIEnabled {
		n.Log.Info("skipping health API initialization because it has been disabled")
		return nil
	}

	n.Log.Info("initializing Health API")
	err = healthChecker.RegisterHealthCheck("network", n.Net)
	if err != nil {
		return fmt.Errorf("couldn't register network health check: %w", err)
	}

	err = healthChecker.RegisterHealthCheck("router", n.Config.ConsensusRouter)
	if err != nil {
		return fmt.Errorf("couldn't register router health check: %w", err)
	}

	// TODO: add database health to liveness check
	err = healthChecker.RegisterHealthCheck("database", n.DB)
	if err != nil {
		return fmt.Errorf("couldn't register database health check: %w", err)
	}

	diskSpaceCheck := health.CheckerFunc(func() (interface{}, error) {
		// confirm that the node has enough disk space to continue operating
		// if there is too little disk space remaining, first report unhealthy and then shutdown the node

		availableDiskBytes := n.resourceTracker.DiskTracker().AvailableDiskBytes()

		var err error
		if availableDiskBytes < n.Config.RequiredAvailableDiskSpace {
			n.Log.Fatal("Node low on disk space [%d bytes available]. Node shutting down...", availableDiskBytes)
			go n.Shutdown(1)
			err = fmt.Errorf("remaining available disk space (%d) is below minimum required available space (%d)", availableDiskBytes, n.Config.RequiredAvailableDiskSpace)
		} else if availableDiskBytes < n.Config.WarningThresholdAvailableDiskSpace {
			err = fmt.Errorf("remaining available disk space (%d) is below the warning threshold of disk space (%d)", availableDiskBytes, n.Config.WarningThresholdAvailableDiskSpace)
		}

		return map[string]interface{}{
			"availableDiskBytes": availableDiskBytes,
		}, err
	})

	err = n.health.RegisterHealthCheck("diskspace", diskSpaceCheck)
	if err != nil {
		return fmt.Errorf("couldn't register resource health check: %w", err)
	}

	handler, err := health.NewGetAndPostHandler(n.Log, healthChecker)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		&common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler:     handler,
		},
		&sync.RWMutex{},
		"health",
		"",
	)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		&common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler:     health.NewGetHandler(healthChecker.Readiness),
		},
		&sync.RWMutex{},
		"health",
		"/readiness",
	)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		&common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler:     health.NewGetHandler(healthChecker.Health),
		},
		&sync.RWMutex{},
		"health",
		"/health",
	)
	if err != nil {
		return err
	}

	return n.APIServer.AddRoute(
		&common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler:     health.NewGetHandler(healthChecker.Liveness),
		},
		&sync.RWMutex{},
		"health",
		"/liveness",
	)
}

// initIPCAPI initializes the IPC API service
// Assumes n.log and n.chainManager already initialized
func (n *Node) initIPCAPI() error {
	if !n.Config.IPCAPIEnabled {
		n.Log.Info("skipping ipc API initialization because it has been disabled")
		return nil
	}
	n.Log.Info("initializing ipc API")
	service, err := ipcsapi.NewService(n.Log, n.chainManager, n.APIServer, n.IPCs)
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(service, &sync.RWMutex{}, "ipcs", "")
}

// Give chains aliases as specified by the genesis information
func (n *Node) initChainAliases(genesisBytes []byte) error {
	n.Log.Info("initializing chain aliases")
	_, chainAliases, err := genesis.Aliases(genesisBytes)
	if err != nil {
		return err
	}

	for chainID, aliases := range chainAliases {
		for _, alias := range aliases {
			if err := n.chainManager.Alias(chainID, alias); err != nil {
				return err
			}
		}
	}
	return nil
}

// APIs aliases as specified by the genesis information
func (n *Node) initAPIAliases(genesisBytes []byte) error {
	n.Log.Info("initializing API aliases")
	apiAliases, _, err := genesis.Aliases(genesisBytes)
	if err != nil {
		return err
	}

	for url, aliases := range apiAliases {
		if err := n.APIServer.AddAliases(url, aliases...); err != nil {
			return err
		}
	}
	return nil
}

// Initializes [n.vdrs] and returns the Primary Network validator set.
func (n *Node) initVdrs() (validators.Set, error) {
	n.vdrs = validators.NewManager()
	vdrSet := validators.NewSet()
	if err := n.vdrs.Set(constants.PrimaryNetworkID, vdrSet); err != nil {
		return vdrSet, fmt.Errorf("couldn't set primary network validators: %w", err)
	}
	return vdrSet, nil
}

// Initialize [n.resourceManager].
func (n *Node) initResourceManager(reg prometheus.Registerer) error {
	n.resourceManager = resource.NewManager(
		n.Config.DatabaseConfig.Path,
		n.Config.SystemTrackerFrequency,
		n.Config.SystemTrackerCPUHalflife,
		n.Config.SystemTrackerDiskHalflife,
	)
	n.resourceManager.TrackProcess(os.Getpid())

	var err error
	n.resourceTracker, err = tracker.NewResourceTracker(reg, n.resourceManager, &meter.ContinuousFactory{}, n.Config.SystemTrackerProcessingHalflife)
	return err
}

// Initialize [n.cpuTargeter].
// Assumes [n.resourceTracker] is already initialized.
func (n *Node) initCPUTargeter(
	config *tracker.TargeterConfig,
	vdrs validators.Set,
) {
	n.cpuTargeter = tracker.NewTargeter(
		config,
		vdrs,
		n.resourceTracker.CPUTracker(),
	)
}

// Initialize [n.diskTargeter].
// Assumes [n.resourceTracker] is already initialized.
func (n *Node) initDiskTargeter(
	config *tracker.TargeterConfig,
	vdrs validators.Set,
) {
	n.diskTargeter = tracker.NewTargeter(
		config,
		vdrs,
		n.resourceTracker.DiskTracker(),
	)
}

// Initialize this node
func (n *Node) Initialize(
	config *Config,
	logger logging.Logger,
	logFactory logging.Factory,
) error {
	n.Log = logger
	n.Config = config
	var err error
	n.ID = ids.NodeIDFromCert(n.Config.StakingTLSCert.Leaf)
	n.LogFactory = logFactory
	n.DoneShuttingDown.Add(1)

	n.Log.Info("node version is: %s", version.CurrentApp)
	n.Log.Info("node ID is: %s", n.ID)

	if err = n.initBeacons(); err != nil { // Configure the beacons
		return fmt.Errorf("problem initializing node beacons: %w", err)
	}

	if err := n.initAPIServer(); err != nil { // Start the API Server
		return fmt.Errorf("couldn't initialize API server: %w", err)
	}

	if err := n.initMetricsAPI(); err != nil { // Start the Metrics API
		return fmt.Errorf("couldn't initialize metrics API: %w", err)
	}

	if err := n.initDatabase(); err != nil { // Set up the node's database
		return fmt.Errorf("problem initializing database: %w", err)
	}

	if err := n.initKeystoreAPI(); err != nil { // Start the Keystore API
		return fmt.Errorf("couldn't initialize keystore API: %w", err)
	}

	if err := n.initSharedMemory(); err != nil { // Initialize shared memory
		return fmt.Errorf("problem initializing shared memory: %w", err)
	}

	// message.Creator is shared between networking, chainManager and the engine.
	// It must be initiated before networking (initNetworking), chain manager (initChainManager)
	// and the engine (initChains) but after the metrics (initMetricsAPI)
	// message.Creator currently record metrics under network namespace
	n.networkNamespace = "network"
	n.msgCreator, err = message.NewCreator(n.MetricsRegisterer,
		n.Config.NetworkConfig.CompressionEnabled,
		n.networkNamespace,
		n.Config.NetworkConfig.MaximumInboundMessageTimeout,
	)
	if err != nil {
		return fmt.Errorf("problem initializing message creator: %w", err)
	}

	primaryNetVdrs, err := n.initVdrs()
	if err != nil {
		return fmt.Errorf("problem initializing validators: %w", err)
	}
	if err := n.initResourceManager(n.MetricsRegisterer); err != nil {
		return fmt.Errorf("problem initializing resource manager: %w", err)
	}
	n.initCPUTargeter(&config.CPUTargeterConfig, primaryNetVdrs)
	n.initDiskTargeter(&config.DiskTargeterConfig, primaryNetVdrs)
	if err = n.initNetworking(primaryNetVdrs); err != nil { // Set up networking layer.
		return fmt.Errorf("problem initializing networking: %w", err)
	}

	n.initEventDispatchers()

	// Start the Health API
	// Has to be initialized before chain manager
	// [n.Net] must already be set
	if err := n.initHealthAPI(); err != nil {
		return fmt.Errorf("couldn't initialize health API: %w", err)
	}
	if err := n.addDefaultVMAliases(); err != nil {
		return fmt.Errorf("couldn't initialize API aliases: %w", err)
	}
	if err := n.initChainManager(n.Config.AvaxAssetID); err != nil { // Set up the chain manager
		return fmt.Errorf("couldn't initialize chain manager: %w", err)
	}
	if err := n.initVMs(); err != nil { // Initialize the VM registry.
		return fmt.Errorf("couldn't initialize VM registry: %w", err)
	}
	if err := n.initAdminAPI(); err != nil { // Start the Admin API
		return fmt.Errorf("couldn't initialize admin API: %w", err)
	}
	if err := n.initInfoAPI(); err != nil { // Start the Info API
		return fmt.Errorf("couldn't initialize info API: %w", err)
	}
	if err := n.initIPCs(); err != nil { // Start the IPCs
		return fmt.Errorf("couldn't initialize IPCs: %w", err)
	}
	if err := n.initIPCAPI(); err != nil { // Start the IPC API
		return fmt.Errorf("couldn't initialize the IPC API: %w", err)
	}
	if err := n.initChainAliases(n.Config.GenesisBytes); err != nil {
		return fmt.Errorf("couldn't initialize chain aliases: %w", err)
	}
	if err := n.initAPIAliases(n.Config.GenesisBytes); err != nil {
		return fmt.Errorf("couldn't initialize API aliases: %w", err)
	}
	if err := n.initIndexer(); err != nil {
		return fmt.Errorf("couldn't initialize indexer: %w", err)
	}

	n.health.Start(n.Config.HealthCheckFreq)
	n.initProfiler()

	// Start the Platform chain
	n.initChains(n.Config.GenesisBytes)
	return nil
}

// Shutdown this node
// May be called multiple times
func (n *Node) Shutdown(exitCode int) {
	if !n.shuttingDown.GetValue() { // only set the exit code once
		n.shuttingDownExitCode.SetValue(exitCode)
	}
	n.shuttingDown.SetValue(true)
	n.shutdownOnce.Do(n.shutdown)
}

func (n *Node) shutdown() {
	n.Log.Info("shutting down node with exit code %d", n.ExitCode())

	if n.health != nil {
		// Passes if the node is not shutting down
		shuttingDownCheck := health.CheckerFunc(func() (interface{}, error) {
			return map[string]interface{}{
				"isShuttingDown": true,
			}, errShuttingDown
		})

		err := n.health.RegisterHealthCheck("shuttingDown", shuttingDownCheck)
		if err != nil {
			n.Log.Debug("couldn't register shuttingDown health check: %s", err)
		}

		time.Sleep(n.Config.ShutdownWait)
	}

	if n.resourceManager != nil {
		n.resourceManager.Shutdown()
	}
	if n.IPCs != nil {
		if err := n.IPCs.Shutdown(); err != nil {
			n.Log.Debug("error during IPC shutdown: %s", err)
		}
	}
	if n.chainManager != nil {
		n.chainManager.Shutdown()
	}
	if n.profiler != nil {
		n.profiler.Shutdown()
	}
	if n.Net != nil {
		n.Net.StartClose()
	}
	if err := n.APIServer.Shutdown(); err != nil {
		n.Log.Debug("error during API shutdown: %s", err)
	}
	if err := n.indexer.Close(); err != nil {
		n.Log.Debug("error closing tx indexer: %s", err)
	}

	// Make sure all plugin subprocesses are killed
	n.Log.Info("cleaning up plugin subprocesses")
	plugin.CleanupClients()

	if n.DBManager != nil {
		if err := n.DBManager.Close(); err != nil {
			n.Log.Warn("error during DB shutdown: %s", err)
		}
	}

	n.DoneShuttingDown.Done()
	n.Log.Info("finished node shutdown")
}

func (n *Node) ExitCode() int {
	if exitCode, ok := n.shuttingDownExitCode.GetValue().(int); ok {
		return exitCode
	}
	return 0
}
