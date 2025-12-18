// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"crypto"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/indexer"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/nat"
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
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/rpcsigner"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/registry"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"

	databasefactory "github.com/ava-labs/avalanchego/database/factory"
	coreth "github.com/ava-labs/avalanchego/graft/coreth/plugin/factory"
	avmconfig "github.com/ava-labs/avalanchego/vms/avm/config"
	platformconfig "github.com/ava-labs/avalanchego/vms/platformvm/config"
)

const (
	stakingPortName = constants.AppName + "-staking"
	httpPortName    = constants.AppName + "-http"

	ipResolutionTimeout = 30 * time.Second

	apiNamespace             = constants.PlatformName + metric.NamespaceSeparator + "api"
	benchlistNamespace       = constants.PlatformName + metric.NamespaceSeparator + "benchlist"
	dbNamespace              = constants.PlatformName + metric.NamespaceSeparator + "db"
	healthNamespace          = constants.PlatformName + metric.NamespaceSeparator + "health"
	meterDBNamespace         = constants.PlatformName + metric.NamespaceSeparator + "meterdb"
	networkNamespace         = constants.PlatformName + metric.NamespaceSeparator + "network"
	processNamespace         = constants.PlatformName + metric.NamespaceSeparator + "process"
	requestsNamespace        = constants.PlatformName + metric.NamespaceSeparator + "requests"
	resourceTrackerNamespace = constants.PlatformName + metric.NamespaceSeparator + "resource_tracker"
	responsesNamespace       = constants.PlatformName + metric.NamespaceSeparator + "responses"
	rpcchainvmNamespace      = constants.PlatformName + metric.NamespaceSeparator + "rpcchainvm"
	systemResourcesNamespace = constants.PlatformName + metric.NamespaceSeparator + "system_resources"
	upgradeNamespace         = constants.PlatformName + metric.NamespaceSeparator + "upgrade"
)

var (
	genesisHashKey     = []byte("genesisID")
	ungracefulShutdown = []byte("ungracefulShutdown")

	indexerDBPrefix = []byte{0x00}

	errInvalidTLSKey        = errors.New("invalid TLS key")
	errShuttingDown         = errors.New("server shutting down")
	errNoValidators         = errors.New("no validators in the current validator set")
	errUpgradeNeeded        = errors.New("unknown network upgrade detected")
	errUpgradeWithinTheDay  = errors.New("unknown network upgrade detected - update as soon as possible")
	errUpgradeWithinTheHour = errors.New("imminent network upgrade detected - update immediately")
)

// New returns an instance of Node
func New(
	config *node.Config,
	logFactory logging.Factory,
	logger logging.Logger,
) (*Node, error) {
	tlsCert := config.StakingTLSCert.Leaf
	stakingCert, err := staking.ParseCertificate(tlsCert.Raw)
	if err != nil {
		return nil, fmt.Errorf("invalid staking certificate: %w", err)
	}

	n := &Node{
		Log:              logger,
		LogFactory:       logFactory,
		StakingTLSSigner: config.StakingTLSCert.PrivateKey.(crypto.Signer),
		StakingTLSCert:   stakingCert,
		ID:               ids.NodeIDFromCert(stakingCert),
		Config:           config,
	}

	n.StakingSigner, err = newStakingSigner(config.StakingSignerConfig)
	if err != nil {
		return nil, fmt.Errorf("problem initializing staking signer: %w", err)
	}

	pop, err := signer.NewProofOfPossession(n.StakingSigner)
	if err != nil {
		return nil, fmt.Errorf("problem creating proof of possession: %w", err)
	}

	logger.Info("initializing node",
		zap.Stringer("version", version.Current),
		zap.String("commit", version.GitCommit),
		zap.Stringer("nodeID", n.ID),
		zap.Stringer("stakingKeyType", tlsCert.PublicKeyAlgorithm),
		zap.Reflect("nodePOP", pop),
		zap.Reflect("providedFlags", n.Config.ProvidedFlags),
		zap.Reflect("config", n.Config),
	)

	n.VMFactoryLog, err = logFactory.Make("vm-factory")
	if err != nil {
		return nil, fmt.Errorf("problem creating vm logger: %w", err)
	}

	n.VMAliaser = ids.NewAliaser()
	for vmID, aliases := range config.VMAliases {
		for _, alias := range aliases {
			if err := n.VMAliaser.Alias(vmID, alias); err != nil {
				return nil, err
			}
		}
	}
	n.VMManager = vms.NewManager(n.VMFactoryLog, n.VMAliaser)

	if err := n.initBootstrappers(); err != nil { // Configure the bootstrappers
		return nil, fmt.Errorf("problem initializing node beacons: %w", err)
	}

	// Set up tracer
	n.tracer, err = trace.New(n.Config.TraceConfig)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize tracer: %w", err)
	}

	if err := n.initMetrics(); err != nil {
		return nil, fmt.Errorf("couldn't initialize metrics: %w", err)
	}

	n.initNAT()
	if err := n.initAPIServer(); err != nil { // Start the API Server
		return nil, fmt.Errorf("couldn't initialize API server: %w", err)
	}

	if err := n.initMetricsAPI(); err != nil { // Start the Metrics API
		return nil, fmt.Errorf("couldn't initialize metrics API: %w", err)
	}

	if err := n.initDatabase(); err != nil { // Set up the node's database
		return nil, fmt.Errorf("problem initializing database: %w", err)
	}

	n.initSharedMemory() // Initialize shared memory

	// message.Creator is shared between networking, chainManager and the engine.
	// It must be initiated before networking (initNetworking), chain manager (initChainManager)
	// and the engine (initChains) but after the metrics (initMetricsAPI)
	// message.Creator currently record metrics under network namespace

	networkRegisterer, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		networkNamespace,
	)
	if err != nil {
		return nil, err
	}

	n.msgCreator, err = message.NewCreator(
		networkRegisterer,
		n.Config.NetworkConfig.CompressionType,
		n.Config.NetworkConfig.MaximumInboundMessageTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("problem initializing message creator: %w", err)
	}

	n.vdrs = validators.NewManager()
	if !n.Config.SybilProtectionEnabled {
		logger.Warn("sybil control is not enforced")
		n.vdrs = newOverriddenManager(constants.PrimaryNetworkID, n.vdrs)
	}
	if err := n.initResourceManager(); err != nil {
		return nil, fmt.Errorf("problem initializing resource manager: %w", err)
	}
	n.initCPUTargeter(&config.CPUTargeterConfig)
	n.initDiskTargeter(&config.DiskTargeterConfig)
	if err := n.initNetworking(networkRegisterer); err != nil { // Set up networking layer.
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

	StakingTLSSigner crypto.Signer
	StakingTLSCert   *staking.Certificate
	StakingSigner    bls.Signer

	// Storage for this node
	DB database.Database

	router     nat.Router
	portMapper *nat.Mapper
	ipUpdater  dynamicip.Updater

	chainRouter router.Router

	// Profiles the process. Nil if continuous profiling is disabled.
	profiler profiler.ContinuousProfiler

	// Indexes blocks, transactions and blocks
	indexer indexer.Indexer

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

	// Net runs the networking stack
	Net network.Network

	// The staking address will optionally be written to a process context
	// file to enable other nodes to be configured to use this node as a
	// beacon.
	stakingAddress netip.AddrPort

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
	Config *node.Config

	tracer trace.Tracer

	// ensures that we only close the node once.
	shutdownOnce sync.Once

	// True if node is shutting down or is done shutting down
	shuttingDown utils.Atomic[bool]

	// Sets the exit code
	shuttingDownExitCode utils.Atomic[int]

	// Metrics Registerer
	MetricsGatherer        metrics.MultiGatherer
	MeterDBMetricsGatherer metrics.MultiGatherer

	VMAliaser ids.Aliaser
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

	// Closed when a sufficient amount of bootstrap nodes are connected to
	onSufficientlyConnected chan struct{}
}

/*
 ******************************************************************************
 *************************** P2P Networking Section ***************************
 ******************************************************************************
 */

// Initialize the networking layer.
// Assumes [n.vdrs], [n.CPUTracker], and [n.CPUTargeter] have been initialized.
func (n *Node) initNetworking(reg prometheus.Registerer) error {
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
	listenAddress := net.JoinHostPort(n.Config.ListenHost, strconv.FormatUint(uint64(n.Config.ListenPort), 10))
	listener, err := net.Listen(constants.NetworkType, listenAddress)
	if err != nil {
		return err
	}
	// Wrap listener so it will only accept a certain number of incoming connections per second
	listener = throttling.NewThrottledListener(listener, n.Config.NetworkConfig.ThrottlerConfig.MaxInboundConnsPerSec)

	// Record the bound address to enable inclusion in process context file.
	n.stakingAddress, err = ips.ParseAddrPort(listener.Addr().String())
	if err != nil {
		return err
	}

	var (
		stakingPort = n.stakingAddress.Port()
		publicAddr  netip.Addr
		atomicIP    *utils.Atomic[netip.AddrPort]
	)
	switch {
	case n.Config.PublicIP != "":
		// Use the specified public IP.
		publicAddr, err = ips.ParseAddr(n.Config.PublicIP)
		if err != nil {
			return fmt.Errorf("invalid public IP address %q: %w", n.Config.PublicIP, err)
		}
		atomicIP = utils.NewAtomic(netip.AddrPortFrom(
			publicAddr,
			stakingPort,
		))
		n.ipUpdater = dynamicip.NewNoUpdater()
	case n.Config.PublicIPResolutionService != "":
		// Use dynamic IP resolution.
		resolver, err := dynamicip.NewResolver(n.Config.PublicIPResolutionService)
		if err != nil {
			return fmt.Errorf("couldn't create IP resolver: %w", err)
		}

		// Use that to resolve our public IP.
		ctx, cancel := context.WithTimeout(context.Background(), ipResolutionTimeout)
		publicAddr, err = resolver.Resolve(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("couldn't resolve public IP: %w", err)
		}
		atomicIP = utils.NewAtomic(netip.AddrPortFrom(
			publicAddr,
			stakingPort,
		))
		n.ipUpdater = dynamicip.NewUpdater(atomicIP, resolver, n.Config.PublicIPResolutionFreq)
	default:
		publicAddr, err = n.router.ExternalIP()
		if err != nil {
			return fmt.Errorf("public IP / IP resolution service not given and failed to resolve IP with NAT: %w", err)
		}
		atomicIP = utils.NewAtomic(netip.AddrPortFrom(
			publicAddr,
			stakingPort,
		))
		n.ipUpdater = dynamicip.NewNoUpdater()
	}

	if !ips.IsPublic(publicAddr) {
		n.Log.Warn("P2P IP is private, you will not be publicly discoverable",
			zap.Stringer("ip", publicAddr),
		)
	}

	// Regularly update our public IP and port mappings.
	n.portMapper.Map(
		stakingPort,
		stakingPort,
		stakingPortName,
		atomicIP,
		n.Config.PublicIPResolutionFreq,
	)
	go n.ipUpdater.Dispatch(n.Log)

	n.Log.Info("initializing networking",
		zap.Stringer("ip", atomicIP.Get()),
	)

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

	// We allow nodes to gossip unknown ACPs in case the current ACPs constant
	// becomes out of date.
	var unknownACPs set.Set[uint32]
	for acp := range n.Config.NetworkConfig.SupportedACPs {
		if !constants.CurrentACPs.Contains(acp) {
			unknownACPs.Add(acp)
		}
	}
	for acp := range n.Config.NetworkConfig.ObjectedACPs {
		if !constants.CurrentACPs.Contains(acp) {
			unknownACPs.Add(acp)
		}
	}
	if unknownACPs.Len() > 0 {
		n.Log.Warn("gossiping unknown ACPs",
			zap.Reflect("acps", unknownACPs),
		)
	}

	tlsConfig := peer.TLSConfig(n.Config.StakingTLSCert, n.tlsKeyLogWriterCloser)

	// Create chain router
	n.chainRouter = &router.ChainRouter{}
	if n.Config.TraceConfig.ExporterConfig.Type != trace.Disabled {
		n.chainRouter = router.Trace(n.chainRouter, n.tracer)
	}

	// Configure benchlist
	n.Config.BenchlistConfig.Validators = n.vdrs
	n.Config.BenchlistConfig.Benchable = n.chainRouter
	n.Config.BenchlistConfig.BenchlistRegisterer = metrics.NewLabelGatherer(chains.ChainLabel)

	err = n.MetricsGatherer.Register(
		benchlistNamespace,
		n.Config.BenchlistConfig.BenchlistRegisterer,
	)
	if err != nil {
		return err
	}

	n.benchlistManager = benchlist.NewManager(&n.Config.BenchlistConfig)

	n.uptimeCalculator = uptime.NewLockedCalculator()

	consensusRouter := n.chainRouter
	if !n.Config.SybilProtectionEnabled {
		// Sybil protection is disabled so we don't have a txID that added us as
		// a validator. Because each validator needs a txID associated with it,
		// we hack one together by just padding our nodeID with zeroes.
		dummyTxID := ids.Empty
		copy(dummyTxID[:], n.ID.Bytes())

		err := n.vdrs.AddStaker(
			constants.PrimaryNetworkID,
			n.ID,
			n.StakingSigner.PublicKey(),
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

	n.onSufficientlyConnected = make(chan struct{})
	numBootstrappers := n.bootstrappers.NumValidators(constants.PrimaryNetworkID)
	requiredConns := (3*numBootstrappers + 3) / 4

	if requiredConns > 0 {
		consensusRouter = &beaconManager{
			Router:                  consensusRouter,
			beacons:                 n.bootstrappers,
			requiredConns:           int64(requiredConns),
			onSufficientlyConnected: n.onSufficientlyConnected,
		}
	} else {
		close(n.onSufficientlyConnected)
	}

	// add node configs to network config
	n.Config.NetworkConfig.MyNodeID = n.ID
	n.Config.NetworkConfig.MyIPPort = atomicIP
	n.Config.NetworkConfig.NetworkID = n.Config.NetworkID
	n.Config.NetworkConfig.Validators = n.vdrs
	n.Config.NetworkConfig.Beacons = n.bootstrappers
	n.Config.NetworkConfig.TLSConfig = tlsConfig
	n.Config.NetworkConfig.TLSKey = tlsKey
	n.Config.NetworkConfig.BLSKey = n.StakingSigner
	n.Config.NetworkConfig.TrackedSubnets = n.Config.TrackedSubnets
	n.Config.NetworkConfig.UptimeCalculator = n.uptimeCalculator
	n.Config.NetworkConfig.UptimeRequirement = n.Config.UptimeRequirement
	n.Config.NetworkConfig.ResourceTracker = n.resourceTracker
	n.Config.NetworkConfig.CPUTargeter = n.cpuTargeter
	n.Config.NetworkConfig.DiskTargeter = n.diskTargeter

	n.Net, err = network.NewNetwork(
		&n.Config.NetworkConfig,
		n.Config.UpgradeConfig.GraniteTime,
		n.msgCreator,
		reg,
		n.Log,
		listener,
		dialer.NewDialer(constants.NetworkType, n.Config.NetworkConfig.DialerConfig, n.Log),
		consensusRouter,
	)

	return err
}

// Write process context to the configured path. Supports the use of
// dynamically chosen network ports with local network orchestration.
func (n *Node) writeProcessContext() error {
	n.Log.Info("writing process context", zap.String("path", n.Config.ProcessContextFilePath))

	// Write the process context to disk
	processContext := &node.ProcessContext{
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
		// If node is already shutting down, this does not trigger shutdown again,
		// and blocks until Shutdown returns.
		n.Shutdown(1)
	})

	// Log a warning if we aren't able to connect to a sufficient portion of
	// nodes.
	go func() {
		timer := time.NewTimer(n.Config.BootstrapBeaconConnectionTimeout)
		defer timer.Stop()

		select {
		case <-timer.C:
			if n.shuttingDown.Get() {
				return
			}
			n.Log.Warn("failed to connect to bootstrap nodes",
				zap.Stringer("bootstrappers", n.bootstrappers),
				zap.Duration("duration", n.Config.BootstrapBeaconConnectionTimeout),
			)
		case <-n.onSufficientlyConnected:
		}
	}()

	// Add state sync nodes to the peer network
	for i, peerIP := range n.Config.StateSyncIPs {
		n.Net.ManuallyTrack(n.Config.StateSyncIDs[i], peerIP)
	}

	// Add bootstrap nodes to the peer network
	for _, bootstrapper := range n.Config.Bootstrappers {
		n.Net.ManuallyTrack(bootstrapper.ID, bootstrapper.IP)
	}

	// Start P2P connections
	retErr := n.Net.Dispatch()

	// If the P2P server isn't running, shut down the node.
	// If node is already shutting down, this does not trigger shutdown again,
	// and blocks until Shutdown returns.
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

	// Remove the process context file to communicate to an orchestrator
	// that the node is no longer running.
	if err := os.Remove(n.Config.ProcessContextFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		n.Log.Error("removal of process context file failed",
			zap.String("path", n.Config.ProcessContextFilePath),
			zap.Error(err),
		)
	}

	return retErr
}

/*
 ******************************************************************************
 *********************** End P2P Networking Section ***************************
 ******************************************************************************
 */

func (n *Node) initDatabase() error {
	var dbFolderName string
	switch n.Config.DatabaseConfig.Name {
	case leveldb.Name:
		// Prior to v1.10.15, the only on-disk database was leveldb, and its
		// files went to [dbPath]/[networkID]/v1.4.5.
		dbFolderName = version.CurrentDatabase
	case pebbledb.Name:
		dbFolderName = "pebble"
	default:
		dbFolderName = "db"
	}
	// dbFolderName is appended to the database path given in the config
	dbFullPath := filepath.Join(n.Config.DatabaseConfig.Path, dbFolderName)

	dbReg, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		dbNamespace,
	)
	if err != nil {
		return err
	}

	db, err := databasefactory.New(
		n.Config.DatabaseConfig.Name,
		dbFullPath,
		n.Config.DatabaseConfig.ReadOnly,
		n.Config.DatabaseConfig.Config,
		dbReg,
		n.Log,
	)
	if err != nil {
		return fmt.Errorf("couldn't create database: %w", err)
	}

	meterDBReg, err := metrics.MakeAndRegister(
		n.MeterDBMetricsGatherer,
		"all",
	)
	if err != nil {
		return err
	}

	n.DB, err = meterdb.New(meterDBReg, db)
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

func (n *Node) initMetrics() error {
	n.MetricsGatherer = metrics.NewPrefixGatherer()
	n.MeterDBMetricsGatherer = metrics.NewLabelGatherer(chains.ChainLabel)
	return n.MetricsGatherer.Register(
		meterDBNamespace,
		n.MeterDBMetricsGatherer,
	)
}

func (n *Node) initNAT() {
	n.Log.Info("initializing NAT")

	if n.Config.PublicIP == "" && n.Config.PublicIPResolutionService == "" {
		n.router = nat.GetRouter()
		if !n.router.SupportsNAT() {
			n.Log.Warn("UPnP and NAT-PMP router attach failed, " +
				"you may not be listening publicly. " +
				"Please confirm the settings in your router")
		}
	} else {
		n.router = nat.NewNoRouter()
	}

	n.portMapper = nat.NewPortMapper(n.Log, n.router)
}

// initAPIServer initializes the server that handles HTTP calls
func (n *Node) initAPIServer() error {
	n.Log.Info("initializing API server")

	// An empty host is treated as a wildcard to match all addresses, so it is
	// considered public.
	hostIsPublic := n.Config.HTTPHost == ""
	if !hostIsPublic {
		ip, err := ips.Lookup(n.Config.HTTPHost)
		if err != nil {
			n.Log.Fatal("failed to lookup HTTP host",
				zap.String("host", n.Config.HTTPHost),
				zap.Error(err),
			)
			return err
		}
		hostIsPublic = ips.IsPublic(ip)

		n.Log.Debug("finished HTTP host lookup",
			zap.String("host", n.Config.HTTPHost),
			zap.Stringer("ip", ip),
			zap.Bool("isPublic", hostIsPublic),
		)
	}

	listenAddress := net.JoinHostPort(n.Config.HTTPHost, strconv.FormatUint(uint64(n.Config.HTTPPort), 10))
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}

	addrStr := listener.Addr().String()
	addrPort, err := ips.ParseAddrPort(addrStr)
	if err != nil {
		return err
	}

	// Don't open the HTTP port if the HTTP server is private
	if hostIsPublic {
		n.Log.Warn("HTTP server is binding to a potentially public host. "+
			"You may be vulnerable to a DoS attack if your HTTP port is publicly accessible",
			zap.String("host", n.Config.HTTPHost),
		)

		n.portMapper.Map(
			addrPort.Port(),
			addrPort.Port(),
			httpPortName,
			nil,
			n.Config.PublicIPResolutionFreq,
		)
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

	apiRegisterer, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		apiNamespace,
	)
	if err != nil {
		return err
	}

	n.APIServer, err = server.New(
		n.Log,
		listener,
		n.Config.HTTPAllowedOrigins,
		n.Config.ShutdownTimeout,
		n.ID,
		n.Config.TraceConfig.ExporterConfig.Type != trace.Disabled,
		n.tracer,
		apiRegisterer,
		n.Config.HTTPConfig.HTTPConfig,
		n.Config.HTTPAllowedHosts,
	)
	return err
}

// Add the default VM aliases
func (n *Node) addDefaultVMAliases() error {
	n.Log.Info("adding the default VM aliases")

	for vmID, aliases := range genesis.VMAliases {
		for _, alias := range aliases {
			if err := n.VMAliaser.Alias(vmID, alias); err != nil {
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

	requestsReg, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		requestsNamespace,
	)
	if err != nil {
		return err
	}

	responseReg, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		responsesNamespace,
	)
	if err != nil {
		return err
	}

	n.timeoutManager, err = timeout.NewManager(
		&n.Config.AdaptiveTimeoutConfig,
		n.benchlistManager,
		requestsReg,
		responseReg,
	)
	if err != nil {
		return err
	}
	go n.Log.RecoverAndPanic(n.timeoutManager.Dispatch)

	// Routes incoming messages from peers to the appropriate chain
	err = n.chainRouter.Initialize(
		n.ID,
		n.Log,
		n.timeoutManager,
		n.Config.ConsensusShutdownTimeout,
		criticalChains,
		n.Config.SybilProtectionEnabled,
		n.Config.TrackedSubnets,
		n.Shutdown,
		n.Config.RouterHealthConfig,
		requestsReg,
	)
	if err != nil {
		return fmt.Errorf("couldn't initialize chain router: %w", err)
	}

	subnets, err := chains.NewSubnets(n.ID, n.Config.SubnetConfigs)
	if err != nil {
		return fmt.Errorf("failed to initialize subnets: %w", err)
	}

	n.chainManager, err = chains.New(
		&chains.ManagerConfig{
			SybilProtectionEnabled:                  n.Config.SybilProtectionEnabled,
			StakingTLSSigner:                        n.StakingTLSSigner,
			StakingTLSCert:                          n.StakingTLSCert,
			StakingBLSKey:                           n.StakingSigner,
			Log:                                     n.Log,
			LogFactory:                              n.LogFactory,
			VMManager:                               n.VMManager,
			BlockAcceptorGroup:                      n.BlockAcceptorGroup,
			TxAcceptorGroup:                         n.TxAcceptorGroup,
			VertexAcceptorGroup:                     n.VertexAcceptorGroup,
			DB:                                      n.DB,
			MsgCreator:                              n.msgCreator,
			Router:                                  n.chainRouter,
			Net:                                     n.Net,
			Validators:                              n.vdrs,
			PartialSyncPrimaryNetwork:               n.Config.PartialSyncPrimaryNetwork,
			NodeID:                                  n.ID,
			NetworkID:                               n.Config.NetworkID,
			Server:                                  n.APIServer,
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
			MeterDBMetrics:                          n.MeterDBMetricsGatherer,
			SubnetConfigs:                           n.Config.SubnetConfigs,
			ChainConfigs:                            n.Config.ChainConfigs,
			FrontierPollFrequency:                   n.Config.FrontierPollFrequency,
			ConsensusAppConcurrency:                 n.Config.ConsensusAppConcurrency,
			BootstrapMaxTimeGetAncestors:            n.Config.BootstrapMaxTimeGetAncestors,
			BootstrapAncestorsMaxContainersSent:     n.Config.BootstrapAncestorsMaxContainersSent,
			BootstrapAncestorsMaxContainersReceived: n.Config.BootstrapAncestorsMaxContainersReceived,
			Upgrades:                                n.Config.UpgradeConfig,
			ResourceTracker:                         n.resourceTracker,
			StateSyncBeacons:                        n.Config.StateSyncIDs,
			TracingEnabled:                          n.Config.TraceConfig.ExporterConfig.Type != trace.Disabled,
			Tracer:                                  n.tracer,
			ChainDataDir:                            n.Config.ChainDataDir,
			Subnets:                                 subnets,
		},
	)
	if err != nil {
		return err
	}

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

	// Register the VMs that Avalanche supports
	err := errors.Join(
		n.VMManager.RegisterFactory(context.TODO(), constants.PlatformVMID, &platformvm.Factory{
			Internal: platformconfig.Internal{
				Chains:                    n.chainManager,
				Validators:                vdrs,
				UptimeLockedCalculator:    n.uptimeCalculator,
				SybilProtectionEnabled:    n.Config.SybilProtectionEnabled,
				PartialSyncPrimaryNetwork: n.Config.PartialSyncPrimaryNetwork,
				TrackedSubnets:            n.Config.TrackedSubnets,
				DynamicFeeConfig:          n.Config.DynamicFeeConfig,
				ValidatorFeeConfig:        n.Config.ValidatorFeeConfig,
				UptimePercentage:          n.Config.UptimeRequirement,
				MinValidatorStake:         n.Config.MinValidatorStake,
				MaxValidatorStake:         n.Config.MaxValidatorStake,
				MinDelegatorStake:         n.Config.MinDelegatorStake,
				MinDelegationFee:          n.Config.MinDelegationFee,
				MinStakeDuration:          n.Config.MinStakeDuration,
				MaxStakeDuration:          n.Config.MaxStakeDuration,
				RewardConfig:              n.Config.RewardConfig,
				UpgradeConfig:             n.Config.UpgradeConfig,
				UseCurrentHeight:          n.Config.UseCurrentHeight,
			},
		}),
		n.VMManager.RegisterFactory(context.TODO(), constants.AVMID, &avm.Factory{
			Config: avmconfig.Config{
				Upgrades:         n.Config.UpgradeConfig,
				TxFee:            n.Config.TxFee,
				CreateAssetTxFee: n.Config.CreateAssetTxFee,
			},
		}),
		n.VMManager.RegisterFactory(context.TODO(), constants.EVMID, &coreth.Factory{}),
	)
	if err != nil {
		return err
	}

	// initialize vm runtime manager
	n.runtimeManager = runtime.NewManager()

	rpcchainvmMetricsGatherer := metrics.NewLabelGatherer(chains.ChainLabel)
	if err := n.MetricsGatherer.Register(rpcchainvmNamespace, rpcchainvmMetricsGatherer); err != nil {
		return err
	}

	// initialize the vm registry
	n.VMRegistry = registry.NewVMRegistry(registry.VMRegistryConfig{
		VMGetter: registry.NewVMGetter(registry.VMGetterConfig{
			FileReader:      filesystem.NewReader(),
			Manager:         n.VMManager,
			PluginDirectory: n.Config.PluginDir,
			CPUTracker:      n.resourceManager,
			RuntimeTracker:  n.runtimeManager,
			MetricsGatherer: rpcchainvmMetricsGatherer,
		}),
		VMManager: n.VMManager,
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

// initSharedMemory initializes the shared memory for cross chain interaction
func (n *Node) initSharedMemory() {
	n.Log.Info("initializing SharedMemory")
	sharedMemoryDB := prefixdb.New([]byte("shared memory"), n.DB)
	n.sharedMemory = atomic.NewMemory(sharedMemoryDB)
}

// initMetricsAPI initializes the Metrics API
// Assumes n.APIServer is already set
func (n *Node) initMetricsAPI() error {
	if !n.Config.MetricsAPIEnabled {
		n.Log.Info("skipping metrics API initialization because it has been disabled")
		return nil
	}

	processReg, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		processNamespace,
	)
	if err != nil {
		return err
	}

	// Current state of process metrics.
	processCollector := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
	if err := processReg.Register(processCollector); err != nil {
		return err
	}

	// Go process metrics using debug.GCStats.
	goCollector := collectors.NewGoCollector()
	if err := processReg.Register(goCollector); err != nil {
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
			DB:           n.DB,
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

	pop, err := signer.NewProofOfPossession(n.StakingSigner)
	if err != nil {
		return fmt.Errorf("problem creating proof of possession: %w", err)
	}

	service, err := info.NewService(
		info.Parameters{
			Version:   version.Current,
			NodeID:    n.ID,
			NodePOP:   pop,
			NetworkID: n.Config.NetworkID,
			VMManager: n.VMManager,
			Upgrades:  n.Config.UpgradeConfig,

			TxFee:            n.Config.TxFee,
			CreateAssetTxFee: n.Config.CreateAssetTxFee,
		},
		n.Log,
		n.vdrs,
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
	healthReg, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		healthNamespace,
	)
	if err != nil {
		return err
	}

	n.health, err = health.New(n.Log, healthReg)
	if err != nil {
		return err
	}

	if !n.Config.HealthAPIEnabled {
		n.Log.Info("skipping health API initialization because it has been disabled")
		return nil
	}

	n.Log.Info("initializing Health API")
	err = n.health.RegisterHealthCheck("network", n.Net, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register network health check: %w", err)
	}

	err = n.health.RegisterHealthCheck("router", n.chainRouter, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register router health check: %w", err)
	}

	// TODO: add database health to liveness check
	err = n.health.RegisterHealthCheck("database", n.DB, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register database health check: %w", err)
	}

	diskSpaceCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		// confirm that the node has enough disk space to continue operating
		// if there is too little disk space remaining, first report unhealthy and then shutdown the node

		availableDiskBytes := n.resourceTracker.DiskTracker().AvailableDiskBytes()
		availableDiskPercentage := n.resourceTracker.DiskTracker().AvailableDiskPercentage()

		var err error

		if availableDiskPercentage < n.Config.RequiredAvailableDiskSpacePercentage {
			n.Log.Fatal("low on disk space. Shutting down...",
				zap.Uint64("availableDiskBytes", availableDiskBytes),
				zap.Uint64("remainingDiskPercentage", availableDiskPercentage),
				zap.Uint64("requiredDiskPercentage", n.Config.RequiredAvailableDiskSpacePercentage),
			)
			go n.Shutdown(1)
			err = fmt.Errorf("remaining available disk space percentage (%d%%) is below minimum required available space percentage (%d%%)", availableDiskPercentage, n.Config.RequiredAvailableDiskSpacePercentage)
		} else if availableDiskPercentage < n.Config.WarningAvailableDiskSpacePercentage {
			err = fmt.Errorf("remaining available disk space percentage (%d%%) is below warning threshold available space percentage (%d%%)", availableDiskPercentage, n.Config.WarningAvailableDiskSpacePercentage)
		}

		return map[string]interface{}{
			"availableDiskBytes":      availableDiskBytes,
			"availableDiskPercentage": availableDiskPercentage,
		}, err
	})

	err = n.health.RegisterHealthCheck("diskspace", diskSpaceCheck, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register resource health check: %w", err)
	}

	wrongBLSKeyCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		vdr, ok := n.vdrs.GetValidator(constants.PrimaryNetworkID, n.ID)
		if !ok {
			return "node is not a validator", nil
		}

		vdrPK := vdr.PublicKey
		if vdrPK == nil {
			return "validator doesn't have a BLS key", nil
		}

		nodePK := n.StakingSigner.PublicKey()
		if nodePK.Equals(vdrPK) {
			return "node has the correct BLS key", nil
		}
		return nil, fmt.Errorf("node has BLS key 0x%x, but is registered to the validator set with 0x%x",
			bls.PublicKeyToCompressedBytes(nodePK),
			bls.PublicKeyToCompressedBytes(vdrPK),
		)
	})

	err = n.health.RegisterHealthCheck("bls", wrongBLSKeyCheck, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register bls health check: %w", err)
	}

	upgradeReg, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		upgradeNamespace,
	)
	if err != nil {
		return fmt.Errorf("couldn't create upgrade metrics register: %w", err)
	}

	timeUntilUpgradeMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "time_until",
		Help: "Time until an upcoming network upgrade (ns). +Inf means the upgrade is unscheduled.",
	})
	infinity := math.Inf(1)
	timeUntilUpgradeMetric.Set(infinity)
	if err := upgradeReg.Register(timeUntilUpgradeMetric); err != nil {
		return fmt.Errorf("couldn't register time until upgrade metric: %w", err)
	}

	// TODO: This healthcheck calls both n.vdrs.GetMap and n.Net.PeerInfo which
	// are expensive calls. This could be rewritten as an event based monitor to
	// avoid expensive iteration.
	var (
		localUpgradeTime     = n.Config.UpgradeConfig.GraniteTime
		localUpgradeTimeUnix = uint64(localUpgradeTime.Unix())
		lastLogTime          time.Time
	)
	futureUpgradeCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		var (
			currentValidators = n.vdrs.GetMap(constants.PrimaryNetworkID)
			totalWeight       uint64
		)
		for _, vdr := range currentValidators {
			totalWeight += vdr.Weight
		}
		if totalWeight == 0 {
			return nil, errNoValidators
		}

		var (
			peers               = n.Net.PeerInfo(maps.Keys(currentValidators))
			upgradeTimes        = make(map[uint64]uint64) // upgrade time -> stake weight
			modeUpgradeTimeUnix uint64
			modeUpgradeWeight   uint64
		)
		for _, peer := range peers {
			vdr := currentValidators[peer.ID]
			upgradeWeight := upgradeTimes[peer.UpgradeTime]
			upgradeWeight += vdr.Weight
			upgradeTimes[peer.UpgradeTime] = upgradeWeight

			if upgradeWeight > modeUpgradeWeight {
				modeUpgradeTimeUnix = peer.UpgradeTime
				modeUpgradeWeight = upgradeWeight
			}
		}

		modeUpgradeWeightPortion := float64(modeUpgradeWeight) / float64(totalWeight)
		result := map[string]interface{}{
			"localUpgradeTime":            localUpgradeTime,
			"modeUpgradeTime":             time.Unix(int64(modeUpgradeTimeUnix), 0).UTC(),
			"modeUpgradeWeightPercentage": 100 * modeUpgradeWeightPortion,
			"numUpgradeTimes":             len(upgradeTimes),
		}
		if localUpgradeTimeUnix >= modeUpgradeTimeUnix || modeUpgradeWeightPortion < .5 {
			timeUntilUpgradeMetric.Set(infinity)
			return result, nil
		}

		const (
			day  = 24 * time.Hour
			week = 7 * day
		)
		modeUpgradeTime := time.Unix(int64(modeUpgradeTimeUnix), 0)
		timeUntilUpgrade := time.Until(modeUpgradeTime)
		timeUntilUpgradeMetric.Set(float64(timeUntilUpgrade))
		result["timeUntilUpgrade"] = timeUntilUpgrade.String()

		var (
			logFrequency time.Duration
			log          func(msg string, fields ...zap.Field)
			err          error
		)
		switch {
		case timeUntilUpgrade > week:
			logFrequency = 12 * time.Hour
			log = n.Log.Info
		case timeUntilUpgrade > 3*day:
			logFrequency = 12 * time.Hour
			log = n.Log.Warn
		case timeUntilUpgrade > day:
			logFrequency = time.Hour
			log = n.Log.Warn
			err = errUpgradeNeeded
		case timeUntilUpgrade > time.Hour:
			logFrequency = time.Hour
			log = n.Log.Error
			err = errUpgradeWithinTheDay
		default:
			logFrequency = 0 // log at the rate of the health check
			log = n.Log.Error
			err = errUpgradeWithinTheHour
		}

		if time.Since(lastLogTime) >= logFrequency {
			log("unknown upgrade detected - this node should be updated to a compatible version",
				zap.String("latestReleaseURL", "https://github.com/ava-labs/avalanchego/releases/latest"),
				zap.Time("upgradeTime", modeUpgradeTime),
				zap.Duration("timeUntilUpgrade", timeUntilUpgrade),
				zap.Error(err),
			)
			lastLogTime = time.Now()
		}
		return result, err
	})

	err = n.health.RegisterHealthCheck("futureupgrade", futureUpgradeCheck, health.ApplicationTag)
	if err != nil {
		return fmt.Errorf("couldn't register future upgrade health check: %w", err)
	}

	handler, err := health.NewGetAndPostHandler(n.Log, n.health)
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
		health.NewGetHandler(n.health.Readiness),
		"health",
		"/readiness",
	)
	if err != nil {
		return err
	}

	err = n.APIServer.AddRoute(
		health.NewGetHandler(n.health.Health),
		"health",
		"/health",
	)
	if err != nil {
		return err
	}

	return n.APIServer.AddRoute(
		health.NewGetHandler(n.health.Liveness),
		"health",
		"/liveness",
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
func (n *Node) initResourceManager() error {
	systemResourcesRegisterer, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		systemResourcesNamespace,
	)
	if err != nil {
		return err
	}
	resourceManager, err := resource.NewManager(
		n.Log,
		n.Config.DatabaseConfig.Path,
		n.Config.SystemTrackerFrequency,
		n.Config.SystemTrackerCPUHalflife,
		n.Config.SystemTrackerDiskHalflife,
		systemResourcesRegisterer,
	)
	if err != nil {
		return err
	}
	n.resourceManager = resourceManager
	n.resourceManager.TrackProcess(os.Getpid())

	resourceTrackerRegisterer, err := metrics.MakeAndRegister(
		n.MetricsGatherer,
		resourceTrackerNamespace,
	)
	if err != nil {
		return err
	}
	n.resourceTracker, err = tracker.NewResourceTracker(
		resourceTrackerRegisterer,
		n.resourceManager,
		&meter.ContinuousFactory{},
		n.Config.SystemTrackerProcessingHalflife,
	)
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

// newStakingSigner returns a BLS signer based on the provided validated configuration.
func newStakingSigner(cfg node.StakingSignerConfig) (bls.Signer, error) {
	if cfg.EphemeralSignerEnabled {
		signer, err := localsigner.New()
		if err != nil {
			return nil, fmt.Errorf("could not generate ephemeral signer: %w", err)
		}

		return signer, nil
	}

	if cfg.KeyContent != "" {
		signerKeyContent, err := base64.StdEncoding.DecodeString(cfg.KeyContent)
		if err != nil {
			return nil, fmt.Errorf("unable to decode base64 content: %w", err)
		}

		signer, err := localsigner.FromBytes(signerKeyContent)
		if err != nil {
			return nil, fmt.Errorf("could not parse signing key: %w", err)
		}

		return signer, nil
	}

	if cfg.RPCEndpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		signer, err := rpcsigner.NewClient(ctx, cfg.RPCEndpoint)
		if err != nil {
			return nil, fmt.Errorf("could not create rpc signer client: %w", err)
		}

		return signer, nil
	}

	if cfg.KeyPathIsSet {
		return localsigner.FromFile(cfg.KeyPath)
	}

	return localsigner.FromFileOrPersistNew(cfg.KeyPath)
}

// Shutdown this node
// May be called multiple times
// All calls to shutdownOnce.Do block until the first call returns
func (n *Node) Shutdown(exitCode int) {
	if !n.shuttingDown.Swap(true) { // only set the exit code once
		n.shuttingDownExitCode.Set(exitCode)
	}
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

	if n.StakingSigner != nil {
		if err := n.StakingSigner.Shutdown(); err != nil {
			n.Log.Debug("error during staking signer shutdown", zap.Error(err))
		}
	}
	if n.resourceManager != nil {
		n.resourceManager.Shutdown()
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
	n.portMapper.UnmapAllPorts()
	n.ipUpdater.Stop()
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

	if n.Config.TraceConfig.ExporterConfig.Type != trace.Disabled {
		n.Log.Info("shutting down tracing")
	}

	if err := n.tracer.Close(); err != nil {
		n.Log.Warn("error during tracer shutdown",
			zap.Error(err),
		)
	}

	n.Log.Info("finished node shutdown")
}

func (n *Node) ExitCode() int {
	return n.shuttingDownExitCode.Get()
}
