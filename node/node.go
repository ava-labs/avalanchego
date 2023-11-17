// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"crypto"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"

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
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebble"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
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
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/registry"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ipcsapi "github.com/ava-labs/avalanchego/api/ipcs"
	avmconfig "github.com/ava-labs/avalanchego/vms/avm/config"
	platformconfig "github.com/ava-labs/avalanchego/vms/platformvm/config"
)

var (
	genesisHashKey     = []byte("genesisID")
	ungracefulShutdown = []byte("ungracefulShutdown")

	indexerDBPrefix  = []byte{0x00}
	keystoreDBPrefix = []byte("keystore")

	errInvalidTLSKey = errors.New("invalid TLS key")
	errShuttingDown  = errors.New("server shutting down")
)

// New returns an instance of Node
func New(
	config *Config,
	logFactory logging.Factory,
	logger logging.Logger,
) (*Node, error) {
	tlsCert := config.StakingTLSCert.Leaf
	stakingCert := staking.CertificateFromX509(tlsCert)
	if err := staking.ValidateCertificate(stakingCert); err != nil {
		return nil, fmt.Errorf("invalid staking certificate: %w", err)
	}

	n := &Node{
		Log:        logger,
		LogFactory: logFactory,
		ID:         ids.NodeIDFromCert(stakingCert),
		Config:     config,
	}

	n.DoneShuttingDown.Add(1)

	pop := signer.NewProofOfPossession(n.Config.StakingSigningKey)
	logger.Info("initializing node",
		zap.Stringer("version", version.CurrentApp),
		zap.Stringer("nodeID", n.ID),
		zap.Stringer("stakingKeyType", tlsCert.PublicKeyAlgorithm),
		zap.Reflect("nodePOP", pop),
		zap.Reflect("providedFlags", n.Config.ProvidedFlags),
		zap.Reflect("config", n.Config),
	)

	var err error
	n.VMFactoryLog, err = logFactory.Make("vm-factory")
	if err != nil {
		return nil, fmt.Errorf("problem creating vm logger: %w", err)
	}

	n.VMManager = vms.NewManager(n.VMFactoryLog, config.VMAliaser)

	if err := n.initBootstrappers(); err != nil { // Configure the bootstrappers
		return nil, fmt.Errorf("problem initializing node beacons: %w", err)
	}

	// Set up tracer
	n.tracer, err = trace.New(n.Config.TraceConfig)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize tracer: %w", err)
	}

	if n.Config.TraceConfig.Enabled {
		n.Config.ConsensusRouter = router.Trace(n.Config.ConsensusRouter, n.tracer)
	}

	n.initMetrics()

	if err := n.initAPIServer(); err != nil { // Start the API Server
		return nil, fmt.Errorf("couldn't initialize API server: %w", err)
	}

	if err := n.initMetricsAPI(); err != nil { // Start the Metrics API
		return nil, fmt.Errorf("couldn't initialize metrics API: %w", err)
	}

	if err := n.initDatabase(); err != nil { // Set up the node's database
		return nil, fmt.Errorf("problem initializing database: %w", err)
	}

	if err := n.initKeystoreAPI(); err != nil { // Start the Keystore API
		return nil, fmt.Errorf("couldn't initialize keystore API: %w", err)
	}

	n.initSharedMemory() // Initialize shared memory

	// message.Creator is shared between networking, chainManager and the engine.
	// It must be initiated before networking (initNetworking), chain manager (initChainManager)
	// and the engine (initChains) but after the metrics (initMetricsAPI)
	// message.Creator currently record metrics under network namespace
	n.networkNamespace = "network"
	n.msgCreator, err = message.NewCreator(
		n.Log,
		n.MetricsRegisterer,
		n.networkNamespace,
		n.Config.NetworkConfig.CompressionType,
		n.Config.NetworkConfig.MaximumInboundMessageTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("problem initializing message creator: %w", err)
	}

	n.vdrs = validators.NewManager()
	if !n.Config.SybilProtectionEnabled {
		n.vdrs = newOverriddenManager(constants.PrimaryNetworkID, n.vdrs)
	}
	if err := n.initResourceManager(n.MetricsRegisterer); err != nil {
		return nil, fmt.Errorf("problem initializing resource manager: %w", err)
	}
	n.initCPUTargeter(&config.CPUTargeterConfig)
	n.initDiskTargeter(&config.DiskTargeterConfig)
	if err := n.initNetworking(); err != nil { // Set up networking layer.
		return nil, fmt.Errorf("problem initializing networking: %w", err)
	}

	n.initEventDispatchers()

	// Start the Health API
	// Has to be initialized before chain manager
	// [n.Net] must already be set
	if err := n.initHealthAPI(); err != nil {
		return nil, fmt.Errorf("couldn't initialize health API: %w", err)
	}
	if err := n.addDefaultVMAliases(); err != nil {
		return nil, fmt.Errorf("couldn't initialize API aliases: %w", err)
	}
	if err := n.initChainManager(n.Config.AvaxAssetID); err != nil { // Set up the chain manager
		return nil, fmt.Errorf("couldn't initialize chain manager: %w", err)
	}
	if err := n.initVMs(); err != nil { // Initialize the VM registry.
		return nil, fmt.Errorf("couldn't initialize VM registry: %w", err)
	}
	if err := n.initAdminAPI(); err != nil { // Start the Admin API
		return nil, fmt.Errorf("couldn't initialize admin API: %w", err)
	}
	if err := n.initInfoAPI(); err != nil { // Start the Info API
		return nil, fmt.Errorf("couldn't initialize info API: %w", err)
	}
	if err := n.initIPCs(); err != nil { // Start the IPCs
		return nil, fmt.Errorf("couldn't initialize IPCs: %w", err)
	}
	if err := n.initIPCAPI(); err != nil { // Start the IPC API
		return nil, fmt.Errorf("couldn't initialize the IPC API: %w", err)
	}
	if err := n.initChainAliases(n.Config.GenesisBytes); err != nil {
		return nil, fmt.Errorf("couldn't initialize chain aliases: %w", err)
	}
	if err := n.initAPIAliases(n.Config.GenesisBytes); err != nil {
		return nil, fmt.Errorf("couldn't initialize API aliases: %w", err)
	}
	if err := n.initIndexer(); err != nil {
		return nil, fmt.Errorf("couldn't initialize indexer: %w", err)
	}

	n.health.Start(context.TODO(), n.Config.HealthCheckFreq)
	n.initProfiler()

	// Start the Platform chain
	if err := n.initChains(n.Config.GenesisBytes); err != nil {
		return nil, fmt.Errorf("couldn't initialize chains: %w", err)
	}
	return n, nil
}

// Node is an instance of an Avalanche node.
type Node struct {
	Log          logging.Logger
	VMFactoryLog logging.Logger
	LogFactory   logging.Factory

	// This node's unique ID used when communicating with other nodes
	// (in consensus, for example)
	ID ids.NodeID

	// Storage for this node
	DB database.Database

	// Profiles the process. Nil if continuous profiling is disabled.
	profiler profiler.ContinuousProfiler

	// Indexes blocks, transactions and blocks
	indexer indexer.Indexer

	// Handles calls to Keystore API
	keystore keystore.Keystore

	// Manages shared memory
	sharedMemory *atomic.Memory

	// Monitors node health and runs health checks
	health health.Health

	// Build and parse messages, for both network layer and chain manager
	msgCreator message.Creator

	// Manages network timeouts
	timeoutManager timeout.Manager

	// Manages creation of blockchains and routing messages to them
	chainManager chains.Manager

	// Manages validator benching
	benchlistManager benchlist.Manager

	uptimeCalculator uptime.LockedCalculator

	// dispatcher for events as they happen in consensus
	BlockAcceptorGroup  snow.AcceptorGroup
	TxAcceptorGroup     snow.AcceptorGroup
	VertexAcceptorGroup snow.AcceptorGroup

	IPCs *ipcs.ChainIPCs

	// Net runs the networking stack
	networkNamespace string
	Net              network.Network

	// The staking address will optionally be written to a process context
	// file to enable other nodes to be configured to use this node as a
	// beacon.
	stakingAddress string

	// tlsKeyLogWriterCloser is a debug file handle that writes all the TLS
	// session keys. This value should only be non-nil during debugging.
	tlsKeyLogWriterCloser io.WriteCloser

	// this node's initial connections to the network
	bootstrappers validators.Manager

	// current validators of the network
	vdrs validators.Manager

	apiURI string

	// Handles HTTP API calls
	APIServer server.Server

	// This node's configuration
	Config *Config

	tracer trace.Tracer

	// ensures that we only close the node once.
	shutdownOnce sync.Once

	// True if node is shutting down or is done shutting down
	shuttingDown utils.Atomic[bool]

	// Sets the exit code
	shuttingDownExitCode utils.Atomic[int]

	// Incremented only once on initialization.
	// Decremented when node is done shutting down.
	DoneShuttingDown sync.WaitGroup

	// Metrics Registerer
	MetricsRegisterer *prometheus.Registry
	MetricsGatherer   metrics.MultiGatherer

	VMManager vms.Manager

	// VM endpoint registry
	VMRegistry registry.VMRegistry

	// Manages shutdown of a VM process
	runtimeManager runtime.Manager

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
// Assumes [n.vdrs], [n.CPUTracker], and [n.CPUTargeter] have been initialized.
func (n *Node) initNetworking() error {
	currentIPPort := n.Config.IPPort.IPPort()

	// Providing either loopback address - `::1` for ipv6 and `127.0.0.1` for ipv4 - as the listen
	// host will avoid the need for a firewall exception on recent MacOS:
	//
	//   - MacOS requires a manually-approved firewall exception [1] for each version of a given
	//   binary that wants to bind to all interfaces (i.e. with an address of `:[port]`). Each
	//   compiled version of avalanchego requires a separate exception to be allowed to bind to all
	//   interfaces.
	//
	//   - A firewall exception is not required to bind to a loopback interface, but the only way for
	//   Listen() to bind to loopback for both ipv4 and ipv6 is to bind to all interfaces [2] which
	//   requires an exception.
	//
	//   - Thus, the only way to start a node on MacOS without approving a firewall exception for the
	//   avalanchego binary is to bind to loopback by specifying the host to be `::1` or `127.0.0.1`.
	//
	// 1: https://apple.stackexchange.com/questions/393715/do-you-want-the-application-main-to-accept-incoming-network-connections-pop
	// 2: https://github.com/golang/go/issues/56998
	listenAddress := net.JoinHostPort(n.Config.ListenHost, strconv.FormatUint(uint64(currentIPPort.Port), 10))

	listener, err := net.Listen(constants.NetworkType, listenAddress)
	if err != nil {
		return err
	}
	// Wrap listener so it will only accept a certain number of incoming connections per second
	listener = throttling.NewThrottledListener(listener, n.Config.NetworkConfig.ThrottlerConfig.MaxInboundConnsPerSec)

	ipPort, err := ips.ToIPPort(listener.Addr().String())
	if err != nil {
		n.Log.Info("initializing networking",
			zap.Stringer("currentNodeIP", currentIPPort),
		)
	} else {
		ipPort = ips.IPPort{
			IP:   currentIPPort.IP,
			Port: ipPort.Port,
		}
		n.Log.Info("initializing networking",
			zap.Stringer("currentNodeIP", ipPort),
		)
	}

	// Record the bound address to enable inclusion in process context file.
	n.stakingAddress = listener.Addr().String()

	tlsKey, ok := n.Config.StakingTLSCert.PrivateKey.(crypto.Signer)
	if !ok {
		return errInvalidTLSKey
	}

	if n.Config.NetworkConfig.TLSKeyLogFile != "" {
		n.tlsKeyLogWriterCloser, err = perms.Create(n.Config.NetworkConfig.TLSKeyLogFile, perms.ReadWrite)
		if err != nil {
			return err
		}
		n.Log.Warn("TLS key logging is enabled",
			zap.String("filename", n.Config.NetworkConfig.TLSKeyLogFile),
		)
	}

	tlsConfig := peer.TLSConfig(n.Config.StakingTLSCert, n.tlsKeyLogWriterCloser)

	// Configure benchlist
	n.Config.BenchlistConfig.Validators = n.vdrs
	n.Config.BenchlistConfig.Benchable = n.Config.ConsensusRouter
	n.benchlistManager = benchlist.NewManager(&n.Config.BenchlistConfig)

	n.uptimeCalculator = uptime.NewLockedCalculator()

	consensusRouter := n.Config.ConsensusRouter
	if !n.Config.SybilProtectionEnabled {
		// Sybil protection is disabled so we don't have a txID that added us as
		// a validator. Because each validator needs a txID associated with it,
		// we hack one together by just padding our nodeID with zeroes.
		dummyTxID := ids.Empty
		copy(dummyTxID[:], n.ID.Bytes())

		err := n.vdrs.AddStaker(
			constants.PrimaryNetworkID,
			n.ID,
			bls.PublicFromSecretKey(n.Config.StakingSigningKey),
			dummyTxID,
			n.Config.SybilProtectionDisabledWeight,
		)
		if err != nil {
			return err
		}

		consensusRouter = &insecureValidatorManager{
			log:    n.Log,
			Router: consensusRouter,
			vdrs:   n.vdrs,
			weight: n.Config.SybilProtectionDisabledWeight,
		}
	}

	numBootstrappers := n.bootstrappers.Count(constants.PrimaryNetworkID)
	requiredConns := (3*numBootstrappers + 3) / 4

	if requiredConns > 0 {
		// Set a timer that will fire after a given timeout unless we connect
		// to a sufficient portion of nodes. If the timeout fires, the node will
		// shutdown.
		timer := timer.NewTimer(func() {
			// If the timeout fires and we're already shutting down, nothing to do.
			if !n.shuttingDown.Get() {
				n.Log.Warn("failed to connect to bootstrap nodes",
					zap.Stringer("bootstrappers", n.bootstrappers),
					zap.Duration("duration", n.Config.BootstrapBeaconConnectionTimeout),
				)
			}
		})

		go timer.Dispatch()
		timer.SetTimeoutIn(n.Config.BootstrapBeaconConnectionTimeout)

		consensusRouter = &beaconManager{
			Router:        consensusRouter,
			timer:         timer,
			beacons:       n.bootstrappers,
			requiredConns: int64(requiredConns),
		}
	}

	// initialize gossip tracker
	gossipTracker, err := peer.NewGossipTracker(n.MetricsRegisterer, n.networkNamespace)
	if err != nil {
		return err
	}

	// keep gossip tracker synchronized with the validator set
	n.vdrs.RegisterCallbackListener(constants.PrimaryNetworkID, &peer.GossipTrackerCallback{
		Log:           n.Log,
		GossipTracker: gossipTracker,
	})

	// add node configs to network config
	n.Config.NetworkConfig.Namespace = n.networkNamespace
	n.Config.NetworkConfig.MyNodeID = n.ID
	n.Config.NetworkConfig.MyIPPort = n.Config.IPPort
	n.Config.NetworkConfig.NetworkID = n.Config.NetworkID
	n.Config.NetworkConfig.Validators = n.vdrs
	n.Config.NetworkConfig.Beacons = n.bootstrappers
	n.Config.NetworkConfig.TLSConfig = tlsConfig
	n.Config.NetworkConfig.TLSKey = tlsKey
	n.Config.NetworkConfig.TrackedSubnets = n.Config.TrackedSubnets
	n.Config.NetworkConfig.UptimeCalculator = n.uptimeCalculator
	n.Config.NetworkConfig.UptimeRequirement = n.Config.UptimeRequirement
	n.Config.NetworkConfig.ResourceTracker = n.resourceTracker
	n.Config.NetworkConfig.CPUTargeter = n.cpuTargeter
	n.Config.NetworkConfig.DiskTargeter = n.diskTargeter
	n.Config.NetworkConfig.GossipTracker = gossipTracker

	n.Net, err = network.NewNetwork(
		&n.Config.NetworkConfig,
		n.msgCreator,
		n.MetricsRegisterer,
		n.Log,
		listener,
		dialer.NewDialer(constants.NetworkType, n.Config.NetworkConfig.DialerConfig, n.Log),
		consensusRouter,
	)

	return err
}

type NodeProcessContext struct {
	// The process id of the node
	PID int `json:"pid"`
	// URI to access the node API
	// Format: [https|http]://[host]:[port]
	URI string `json:"uri"`
	// Address other nodes can use to communicate with this node
	// Format: [host]:[port]
	StakingAddress string `json:"stakingAddress"`
}

// Write process context to the configured path. Supports the use of
// dynamically chosen network ports with local network orchestration.
func (n *Node) writeProcessContext() error {
	n.Log.Info("writing process context", zap.String("path", n.Config.ProcessContextFilePath))

	// Write the process context to disk
	processContext := &NodeProcessContext{
		PID:            os.Getpid(),
		URI:            n.apiURI,
		StakingAddress: n.stakingAddress, // Set by network initialization
	}
	bytes, err := json.MarshalIndent(processContext, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal process context: %w", err)
	}
	if err := perms.WriteFile(n.Config.ProcessContextFilePath, bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write process context: %w", err)
	}
	return nil
}

// Dispatch starts the node's servers.
// Returns when the node exits.
func (n *Node) Dispatch() error {
	if err := n.writeProcessContext(); err != nil {
		return err
	}

	// Start the HTTP API server
	go n.Log.RecoverAndPanic(func() {
		n.Log.Info("API server listening",
			zap.String("uri", n.apiURI),
		)
		err := n.APIServer.Dispatch()
		// When [n].Shutdown() is called, [n.APIServer].Close() is called.
		// This causes [n.APIServer].Dispatch() to return an error.
		// If that happened, don't log/return an error here.
		if !n.shuttingDown.Get() {
			n.Log.Fatal("API server dispatch failed",
				zap.Error(err),
			)
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
	for _, bootstrapper := range n.Config.Bootstrappers {
		n.Net.ManuallyTrack(bootstrapper.ID, ips.IPPort(bootstrapper.IP))
	}

	// Start P2P connections
	err := n.Net.Dispatch()

	// If the P2P server isn't running, shut down the node.
	// If node is already shutting down, this does nothing.
	n.Shutdown(1)

	if n.tlsKeyLogWriterCloser != nil {
		err := n.tlsKeyLogWriterCloser.Close()
		if err != nil {
			n.Log.Error("closing TLS key log file failed",
				zap.String("filename", n.Config.NetworkConfig.TLSKeyLogFile),
				zap.Error(err),
			)
		}
	}

	// Wait until the node is done shutting down before returning
	n.DoneShuttingDown.Wait()

	// Remove the process context file to communicate to an orchestrator
	// that the node is no longer running.
	if err := os.Remove(n.Config.ProcessContextFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		n.Log.Error("removal of process context file failed",
			zap.String("path", n.Config.ProcessContextFilePath),
			zap.Error(err),
		)
	}

	return err
}

/*
 ******************************************************************************
 *********************** End P2P Networking Section ***************************
 ******************************************************************************
 */

func (n *Node) initDatabase() error {
	// start the db
	switch n.Config.DatabaseConfig.Name {
	case leveldb.Name:
		// Prior to v1.10.15, the only on-disk database was leveldb, and its
		// files went to [dbPath]/[networkID]/v1.4.5.
		dbPath := filepath.Join(n.Config.DatabaseConfig.Path, version.CurrentDatabase.String())
		var err error
		n.DB, err = leveldb.New(dbPath, n.Config.DatabaseConfig.Config, n.Log, "db_internal", n.MetricsRegisterer)
		if err != nil {
			return fmt.Errorf("couldn't create leveldb at %s: %w", dbPath, err)
		}
	case memdb.Name:
		n.DB = memdb.New()
	case pebble.Name:
		dbPath := filepath.Join(n.Config.DatabaseConfig.Path, pebble.Name)
		var err error
		n.DB, err = pebble.New(dbPath, n.Config.DatabaseConfig.Config, n.Log, "db_internal", n.MetricsRegisterer)
		if err != nil {
			return fmt.Errorf("couldn't create pebbledb at %s: %w", dbPath, err)
		}
	default:
		return fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			n.Config.DatabaseConfig.Name,
			leveldb.Name,
			memdb.Name,
			pebble.Name,
		)
	}

	if n.Config.ReadOnly && n.Config.DatabaseConfig.Name != memdb.Name {
		n.DB = versiondb.New(n.DB)
	}

	var err error
	n.DB, err = meterdb.New("db", n.MetricsRegisterer, n.DB)
	if err != nil {
		return err
	}

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

	n.Log.Info("initializing database",
		zap.Stringer("genesisHash", genesisHash),
	)

	ok, err := n.DB.Has(ungracefulShutdown)
	if err != nil {
		return fmt.Errorf("failed to read ungraceful shutdown key: %w", err)
	}

	if ok {
		n.Log.Warn("detected previous ungraceful shutdown")
	}

	if err := n.DB.Put(ungracefulShutdown, nil); err != nil {
		return fmt.Errorf(
			"failed to write ungraceful shutdown key at: %w",
			err,
		)
	}

	return nil
}

// Set the node IDs of the peers this node should first connect to
func (n *Node) initBootstrappers() error {
	n.bootstrappers = validators.NewManager()
	for _, bootstrapper := range n.Config.Bootstrappers {
		// Note: The beacon connection manager will treat all beaconIDs as
		//       equal.
		// Invariant: We never use the TxID or BLS keys populated here.
		if err := n.bootstrappers.AddStaker(constants.PrimaryNetworkID, bootstrapper.ID, nil, ids.Empty, 1); err != nil {
			return err
		}
	}
	return nil
}

// Create the EventDispatcher used for hooking events
// into the general process flow.
func (n *Node) initEventDispatchers() {
	n.BlockAcceptorGroup = snow.NewAcceptorGroup(n.Log)
	n.TxAcceptorGroup = snow.NewAcceptorGroup(n.Log)
	n.VertexAcceptorGroup = snow.NewAcceptorGroup(n.Log)
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
	n.IPCs, err = ipcs.NewChainIPCs(
		n.Log,
		n.Config.IPCPath,
		n.Config.NetworkID,
		n.BlockAcceptorGroup,
		n.TxAcceptorGroup,
		n.VertexAcceptorGroup,
		chainIDs,
	)
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
		IndexingEnabled:      n.Config.IndexAPIEnabled,
		AllowIncompleteIndex: n.Config.IndexAllowIncomplete,
		DB:                   txIndexerDB,
		Log:                  n.Log,
		BlockAcceptorGroup:   n.BlockAcceptorGroup,
		TxAcceptorGroup:      n.TxAcceptorGroup,
		VertexAcceptorGroup:  n.VertexAcceptorGroup,
		APIServer:            n.APIServer,
		ShutdownF: func() {
			n.Shutdown(0) // TODO put exit code here
		},
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
func (n *Node) initChains(genesisBytes []byte) error {
	n.Log.Info("initializing chains")

	platformChain := chains.ChainParameters{
		ID:            constants.PlatformChainID,
		SubnetID:      constants.PrimaryNetworkID,
		GenesisData:   genesisBytes, // Specifies other chains to create
		VMID:          constants.PlatformVMID,
		CustomBeacons: n.bootstrappers,
	}

	// Start the chain creator with the Platform Chain
	return n.chainManager.StartChainCreator(platformChain)
}

func (n *Node) initMetrics() {
	n.MetricsRegisterer = prometheus.NewRegistry()
	n.MetricsGatherer = metrics.NewMultiGatherer()
}

// initAPIServer initializes the server that handles HTTP calls
func (n *Node) initAPIServer() error {
	n.Log.Info("initializing API server")

	listenAddress := net.JoinHostPort(n.Config.HTTPHost, strconv.FormatUint(uint64(n.Config.HTTPPort), 10))
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}

	protocol := "http"
	if n.Config.HTTPSEnabled {
		cert, err := tls.X509KeyPair(n.Config.HTTPSCert, n.Config.HTTPSKey)
		if err != nil {
			return err
		}
		config := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
		listener = tls.NewListener(listener, config)

		protocol = "https"
	}
	n.apiURI = fmt.Sprintf("%s://%s", protocol, listener.Addr())

	if !n.Config.APIRequireAuthToken {
		var err error
		n.APIServer, err = server.New(
			n.Log,
			n.LogFactory,
			listener,
			n.Config.HTTPAllowedOrigins,
			n.Config.ShutdownTimeout,
			n.ID,
			n.Config.TraceConfig.Enabled,
			n.tracer,
			"api",
			n.MetricsRegisterer,
			n.Config.HTTPConfig.HTTPConfig,
			n.Config.HTTPAllowedHosts,
		)
		return err
	}

	a, err := auth.New(n.Log, "auth", n.Config.APIAuthPassword)
	if err != nil {
		return err
	}

	n.APIServer, err = server.New(
		n.Log,
		n.LogFactory,
		listener,
		n.Config.HTTPAllowedOrigins,
		n.Config.ShutdownTimeout,
		n.ID,
		n.Config.TraceConfig.Enabled,
		n.tracer,
		"api",
		n.MetricsRegisterer,
		n.Config.HTTPConfig.HTTPConfig,
		n.Config.HTTPAllowedHosts,
		a,
	)
	if err != nil {
		return err
	}

	// only create auth service if token authorization is required
	n.Log.Info("API authorization is enabled. Auth tokens must be passed in the header of API requests, except requests to the auth service.")
	handler, err := a.CreateHandler()
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(handler, "auth", "")
}

// Add the default VM aliases
func (n *Node) addDefaultVMAliases() error {
	n.Log.Info("adding the default VM aliases")
	vmAliases := genesis.GetVMAliases()

	for vmID, aliases := range vmAliases {
		for _, alias := range aliases {
			if err := n.Config.VMAliaser.Alias(vmID, alias); err != nil {
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
	criticalChains := set.Of(
		constants.PlatformChainID,
		xChainID,
		cChainID,
	)

	n.timeoutManager, err = timeout.NewManager(
		&n.Config.AdaptiveTimeoutConfig,
		n.benchlistManager,
		"requests",
		n.MetricsRegisterer,
	)
	if err != nil {
		return err
	}
	go n.Log.RecoverAndPanic(n.timeoutManager.Dispatch)

	// Routes incoming messages from peers to the appropriate chain
	err = n.Config.ConsensusRouter.Initialize(
		n.ID,
		n.Log,
		n.timeoutManager,
		n.Config.ConsensusShutdownTimeout,
		criticalChains,
		n.Config.SybilProtectionEnabled,
		n.Config.TrackedSubnets,
		n.Shutdown,
		n.Config.RouterHealthConfig,
		"requests",
		n.MetricsRegisterer,
	)
	if err != nil {
		return fmt.Errorf("couldn't initialize chain router: %w", err)
	}

	n.chainManager = chains.New(&chains.ManagerConfig{
		SybilProtectionEnabled:                  n.Config.SybilProtectionEnabled,
		StakingTLSCert:                          n.Config.StakingTLSCert,
		StakingBLSKey:                           n.Config.StakingSigningKey,
		Log:                                     n.Log,
		LogFactory:                              n.LogFactory,
		VMManager:                               n.VMManager,
		BlockAcceptorGroup:                      n.BlockAcceptorGroup,
		TxAcceptorGroup:                         n.TxAcceptorGroup,
		VertexAcceptorGroup:                     n.VertexAcceptorGroup,
		DB:                                      n.DB,
		MsgCreator:                              n.msgCreator,
		Router:                                  n.Config.ConsensusRouter,
		Net:                                     n.Net,
		Validators:                              n.vdrs,
		PartialSyncPrimaryNetwork:               n.Config.PartialSyncPrimaryNetwork,
		NodeID:                                  n.ID,
		NetworkID:                               n.Config.NetworkID,
		Server:                                  n.APIServer,
		Keystore:                                n.keystore,
		AtomicMemory:                            n.sharedMemory,
		AVAXAssetID:                             avaxAssetID,
		XChainID:                                xChainID,
		CChainID:                                cChainID,
		CriticalChains:                          criticalChains,
		TimeoutManager:                          n.timeoutManager,
		Health:                                  n.health,
		ShutdownNodeFunc:                        n.Shutdown,
		MeterVMEnabled:                          n.Config.MeterVMEnabled,
		Metrics:                                 n.MetricsGatherer,
		SubnetConfigs:                           n.Config.SubnetConfigs,
		ChainConfigs:                            n.Config.ChainConfigs,
		AcceptedFrontierGossipFrequency:         n.Config.AcceptedFrontierGossipFrequency,
		ConsensusAppConcurrency:                 n.Config.ConsensusAppConcurrency,
		BootstrapMaxTimeGetAncestors:            n.Config.BootstrapMaxTimeGetAncestors,
		BootstrapAncestorsMaxContainersSent:     n.Config.BootstrapAncestorsMaxContainersSent,
		BootstrapAncestorsMaxContainersReceived: n.Config.BootstrapAncestorsMaxContainersReceived,
		ApricotPhase4Time:                       version.GetApricotPhase4Time(n.Config.NetworkID),
		ApricotPhase4MinPChainHeight:            version.ApricotPhase4MinPChainHeight[n.Config.NetworkID],
		ResourceTracker:                         n.resourceTracker,
		StateSyncBeacons:                        n.Config.StateSyncIDs,
		TracingEnabled:                          n.Config.TraceConfig.Enabled,
		Tracer:                                  n.tracer,
		ChainDataDir:                            n.Config.ChainDataDir,
	})

	// Notify the API server when new chains are created
	n.chainManager.AddRegistrant(n.APIServer)
	return nil
}

// initVMs initializes the VMs Avalanche supports + any additional vms installed as plugins.
func (n *Node) initVMs() error {
	n.Log.Info("initializing VMs")

	vdrs := n.vdrs

	// If sybil protection is disabled, we provide the P-chain its own local
	// validator manager that will not be used by the rest of the node. This
	// allows the node's validator sets to be determined by network connections.
	if !n.Config.SybilProtectionEnabled {
		vdrs = validators.NewManager()
	}

	vmRegisterer := registry.NewVMRegisterer(registry.VMRegistererConfig{
		APIServer:    n.APIServer,
		Log:          n.Log,
		VMFactoryLog: n.VMFactoryLog,
		VMManager:    n.VMManager,
	})

	// Register the VMs that Avalanche supports
	err := utils.Err(
		vmRegisterer.Register(context.TODO(), constants.PlatformVMID, &platformvm.Factory{
			Config: platformconfig.Config{
				Chains:                        n.chainManager,
				Validators:                    vdrs,
				UptimeLockedCalculator:        n.uptimeCalculator,
				SybilProtectionEnabled:        n.Config.SybilProtectionEnabled,
				PartialSyncPrimaryNetwork:     n.Config.PartialSyncPrimaryNetwork,
				TrackedSubnets:                n.Config.TrackedSubnets,
				TxFee:                         n.Config.TxFee,
				CreateAssetTxFee:              n.Config.CreateAssetTxFee,
				CreateSubnetTxFee:             n.Config.CreateSubnetTxFee,
				TransformSubnetTxFee:          n.Config.TransformSubnetTxFee,
				CreateBlockchainTxFee:         n.Config.CreateBlockchainTxFee,
				AddPrimaryNetworkValidatorFee: n.Config.AddPrimaryNetworkValidatorFee,
				AddPrimaryNetworkDelegatorFee: n.Config.AddPrimaryNetworkDelegatorFee,
				AddSubnetValidatorFee:         n.Config.AddSubnetValidatorFee,
				AddSubnetDelegatorFee:         n.Config.AddSubnetDelegatorFee,
				UptimePercentage:              n.Config.UptimeRequirement,
				MinValidatorStake:             n.Config.MinValidatorStake,
				MaxValidatorStake:             n.Config.MaxValidatorStake,
				MinDelegatorStake:             n.Config.MinDelegatorStake,
				MinDelegationFee:              n.Config.MinDelegationFee,
				MinStakeDuration:              n.Config.MinStakeDuration,
				MaxStakeDuration:              n.Config.MaxStakeDuration,
				RewardConfig:                  n.Config.RewardConfig,
				ApricotPhase3Time:             version.GetApricotPhase3Time(n.Config.NetworkID),
				ApricotPhase5Time:             version.GetApricotPhase5Time(n.Config.NetworkID),
				BanffTime:                     version.GetBanffTime(n.Config.NetworkID),
				CortinaTime:                   version.GetCortinaTime(n.Config.NetworkID),
				DTime:                         version.GetDTime(n.Config.NetworkID),
				UseCurrentHeight:              n.Config.UseCurrentHeight,
			},
		}),
		vmRegisterer.Register(context.TODO(), constants.AVMID, &avm.Factory{
			Config: avmconfig.Config{
				TxFee:            n.Config.TxFee,
				CreateAssetTxFee: n.Config.CreateAssetTxFee,
			},
		}),
		vmRegisterer.Register(context.TODO(), constants.EVMID, &coreth.Factory{}),
		n.VMManager.RegisterFactory(context.TODO(), secp256k1fx.ID, &secp256k1fx.Factory{}),
		n.VMManager.RegisterFactory(context.TODO(), nftfx.ID, &nftfx.Factory{}),
		n.VMManager.RegisterFactory(context.TODO(), propertyfx.ID, &propertyfx.Factory{}),
	)
	if err != nil {
		return err
	}

	// initialize vm runtime manager
	n.runtimeManager = runtime.NewManager()

	// initialize the vm registry
	n.VMRegistry = registry.NewVMRegistry(registry.VMRegistryConfig{
		VMGetter: registry.NewVMGetter(registry.VMGetterConfig{
			FileReader:      filesystem.NewReader(),
			Manager:         n.VMManager,
			PluginDirectory: n.Config.PluginDir,
			CPUTracker:      n.resourceManager,
			RuntimeTracker:  n.runtimeManager,
		}),
		VMRegisterer: vmRegisterer,
	})

	// register any vms that need to be installed as plugins from disk
	_, failedVMs, err := n.VMRegistry.Reload(context.TODO())
	for failedVM, err := range failedVMs {
		n.Log.Error("failed to register VM",
			zap.Stringer("vmID", failedVM),
			zap.Error(err),
		)
	}
	return err
}

// initSharedMemory initializes the shared memory for cross chain interation
func (n *Node) initSharedMemory() {
	n.Log.Info("initializing SharedMemory")
	sharedMemoryDB := prefixdb.New([]byte("shared memory"), n.DB)
	n.sharedMemory = atomic.NewMemory(sharedMemoryDB)
}

// initKeystoreAPI initializes the keystore service, which is an on-node wallet.
// Assumes n.APIServer is already set
func (n *Node) initKeystoreAPI() error {
	n.Log.Info("initializing keystore")
	n.keystore = keystore.New(n.Log, prefixdb.New(keystoreDBPrefix, n.DB))
	handler, err := n.keystore.CreateHandler()
	if err != nil {
		return err
	}
	if !n.Config.KeystoreAPIEnabled {
		n.Log.Info("skipping keystore API initialization because it has been disabled")
		return nil
	}
	n.Log.Warn("initializing deprecated keystore API")
	return n.APIServer.AddRoute(handler, "keystore", "")
}

// initMetricsAPI initializes the Metrics API
// Assumes n.APIServer is already set
func (n *Node) initMetricsAPI() error {
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
		promhttp.HandlerFor(
			n.MetricsGatherer,
			promhttp.HandlerOpts{},
		),
		"metrics",
		"",
	)
}

// initAdminAPI initializes the Admin API service
// Assumes n.log, n.chainManager, and n.ValidatorAPI already initialized
func (n *Node) initAdminAPI() error {
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
			VMManager:    n.VMManager,
			VMRegistry:   n.VMRegistry,
		},
	)
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(
		service,
		"admin",
		"",
	)
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
			n.Log.Fatal("continuous profiler failed",
				zap.Error(err),
			)
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

	service, err := info.NewService(
		info.Parameters{
			Version:                       version.CurrentApp,
			NodeID:                        n.ID,
			NodePOP:                       signer.NewProofOfPossession(n.Config.StakingSigningKey),
			NetworkID:                     n.Config.NetworkID,
			TxFee:                         n.Config.TxFee,
			CreateAssetTxFee:              n.Config.CreateAssetTxFee,
			CreateSubnetTxFee:             n.Config.CreateSubnetTxFee,
			TransformSubnetTxFee:          n.Config.TransformSubnetTxFee,
			CreateBlockchainTxFee:         n.Config.CreateBlockchainTxFee,
			AddPrimaryNetworkValidatorFee: n.Config.AddPrimaryNetworkValidatorFee,
			AddPrimaryNetworkDelegatorFee: n.Config.AddPrimaryNetworkDelegatorFee,
			AddSubnetValidatorFee:         n.Config.AddSubnetValidatorFee,
			AddSubnetDelegatorFee:         n.Config.AddSubnetDelegatorFee,
			VMManager:                     n.VMManager,
		},
		n.Log,
		n.chainManager,
		n.VMManager,
		n.Config.NetworkConfig.MyIPPort,
		n.Net,
		n.benchlistManager,
	)
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(
		service,
		"info",
		"",
	)
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
	err = healthChecker.RegisterHealthCheck("network", n.Net, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register network health check: %w", err)
	}

	err = healthChecker.RegisterHealthCheck("router", n.Config.ConsensusRouter, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register router health check: %w", err)
	}

	// TODO: add database health to liveness check
	err = healthChecker.RegisterHealthCheck("database", n.DB, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register database health check: %w", err)
	}

	diskSpaceCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		// confirm that the node has enough disk space to continue operating
		// if there is too little disk space remaining, first report unhealthy and then shutdown the node

		availableDiskBytes := n.resourceTracker.DiskTracker().AvailableDiskBytes()

		var err error
		if availableDiskBytes < n.Config.RequiredAvailableDiskSpace {
			n.Log.Fatal("low on disk space. Shutting down...",
				zap.Uint64("remainingDiskBytes", availableDiskBytes),
			)
			go n.Shutdown(1)
			err = fmt.Errorf("remaining available disk space (%d) is below minimum required available space (%d)", availableDiskBytes, n.Config.RequiredAvailableDiskSpace)
		} else if availableDiskBytes < n.Config.WarningThresholdAvailableDiskSpace {
			err = fmt.Errorf("remaining available disk space (%d) is below the warning threshold of disk space (%d)", availableDiskBytes, n.Config.WarningThresholdAvailableDiskSpace)
		}

		return map[string]interface{}{
			"availableDiskBytes": availableDiskBytes,
		}, err
	})

	err = n.health.RegisterHealthCheck("diskspace", diskSpaceCheck, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register resource health check: %w", err)
	}

	handler, err := health.NewGetAndPostHandler(n.Log, healthChecker)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		handler,
		"health",
		"",
	)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		health.NewGetHandler(healthChecker.Readiness),
		"health",
		"/readiness",
	)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		health.NewGetHandler(healthChecker.Health),
		"health",
		"/health",
	)
	if err != nil {
		return err
	}

	return n.APIServer.AddRoute(
		health.NewGetHandler(healthChecker.Liveness),
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
	n.Log.Warn("initializing deprecated ipc API")
	service, err := ipcsapi.NewService(n.Log, n.chainManager, n.IPCs)
	if err != nil {
		return err
	}
	return n.APIServer.AddRoute(
		service,
		"ipcs",
		"",
	)
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

	for chainID, aliases := range n.Config.ChainAliases {
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

// Initialize [n.resourceManager].
func (n *Node) initResourceManager(reg prometheus.Registerer) error {
	resourceManager, err := resource.NewManager(
		n.Log,
		n.Config.DatabaseConfig.Path,
		n.Config.SystemTrackerFrequency,
		n.Config.SystemTrackerCPUHalflife,
		n.Config.SystemTrackerDiskHalflife,
		reg,
	)
	if err != nil {
		return err
	}
	n.resourceManager = resourceManager
	n.resourceManager.TrackProcess(os.Getpid())

	n.resourceTracker, err = tracker.NewResourceTracker(reg, n.resourceManager, &meter.ContinuousFactory{}, n.Config.SystemTrackerProcessingHalflife)
	return err
}

// Initialize [n.cpuTargeter].
// Assumes [n.resourceTracker] is already initialized.
func (n *Node) initCPUTargeter(
	config *tracker.TargeterConfig,
) {
	n.cpuTargeter = tracker.NewTargeter(
		n.Log,
		config,
		n.vdrs,
		n.resourceTracker.CPUTracker(),
	)
}

// Initialize [n.diskTargeter].
// Assumes [n.resourceTracker] is already initialized.
func (n *Node) initDiskTargeter(
	config *tracker.TargeterConfig,
) {
	n.diskTargeter = tracker.NewTargeter(
		n.Log,
		config,
		n.vdrs,
		n.resourceTracker.DiskTracker(),
	)
}

// Shutdown this node
// May be called multiple times
func (n *Node) Shutdown(exitCode int) {
	if !n.shuttingDown.Get() { // only set the exit code once
		n.shuttingDownExitCode.Set(exitCode)
	}
	n.shuttingDown.Set(true)
	n.shutdownOnce.Do(n.shutdown)
}

func (n *Node) shutdown() {
	n.Log.Info("shutting down node",
		zap.Int("exitCode", n.ExitCode()),
	)

	if n.health != nil {
		// Passes if the node is not shutting down
		shuttingDownCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
			return map[string]interface{}{
				"isShuttingDown": true,
			}, errShuttingDown
		})

		err := n.health.RegisterHealthCheck("shuttingDown", shuttingDownCheck, health.ApplicationTag)
		if err != nil {
			n.Log.Debug("couldn't register shuttingDown health check",
				zap.Error(err),
			)
		}

		time.Sleep(n.Config.ShutdownWait)
	}

	if n.resourceManager != nil {
		n.resourceManager.Shutdown()
	}
	if n.IPCs != nil {
		if err := n.IPCs.Shutdown(); err != nil {
			n.Log.Debug("error during IPC shutdown",
				zap.Error(err),
			)
		}
	}
	n.timeoutManager.Stop()
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
		n.Log.Debug("error during API shutdown",
			zap.Error(err),
		)
	}
	if err := n.indexer.Close(); err != nil {
		n.Log.Debug("error closing tx indexer",
			zap.Error(err),
		)
	}

	// Ensure all runtimes are shutdown
	n.Log.Info("cleaning up plugin runtimes")
	n.runtimeManager.Stop(context.TODO())

	if n.DB != nil {
		if err := n.DB.Delete(ungracefulShutdown); err != nil {
			n.Log.Error(
				"failed to delete ungraceful shutdown key",
				zap.Error(err),
			)
		}

		if err := n.DB.Close(); err != nil {
			n.Log.Warn("error during DB shutdown",
				zap.Error(err),
			)
		}
	}

	if n.Config.TraceConfig.Enabled {
		n.Log.Info("shutting down tracing")
	}

	if err := n.tracer.Close(); err != nil {
		n.Log.Warn("error during tracer shutdown",
			zap.Error(err),
		)
	}

	n.DoneShuttingDown.Done()
	n.Log.Info("finished node shutdown")
}

func (n *Node) ExitCode() int {
	return n.shuttingDownExitCode.Get()
}
