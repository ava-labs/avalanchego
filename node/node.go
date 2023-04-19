// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
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
	"github.com/ava-labs/avalanchego/utils/wrappers"
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
	genesisHashKey  = []byte("genesisID")
	indexerDBPrefix = []byte{0x00}

	errInvalidTLSKey = errors.New("invalid TLS key")
	errShuttingDown  = errors.New("server shutting down")
)

// Node is an instance of an Avalanche node.
type Node struct {
	// Router that is used to handle incoming consensus messages
	consensusRouter router.Router

	Log          logging.Logger
	VMFactoryLog logging.Logger
	LogFactory   logging.Factory

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
	sharedMemory *atomic.Memory

	// Monitors node health and runs health checks
	health health.Health

	// Build and parse messages, for both network layer and chain manager
	msgCreator message.Creator

	// Manages creation of blockchains and routing messages to them
	chainManager chains.Manager

	// Manages valdiator benching
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

	// tlsKeyLogWriterCloser is a debug file handle that writes all the TLS
	// session keys. This value should only be non-nil during debugging.
	tlsKeyLogWriterCloser io.WriteCloser

	// this node's initial connections to the network
	beacons validators.Set

	// current validators of the network
	vdrs validators.Manager

	// Handles HTTP API calls
	APIServer server.Server

	// This node's configuration
	Config *Config

	tracer trace.Tracer

	// ensures that we only close the node once.
	shutdownOnce sync.Once

	// True if node is shutting down or is done shutting down
	shuttingDown *utils.Atomic[bool]

	// Sets the exit code
	shuttingDownExitCode utils.Atomic[int]

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

	timeoutManager timeout.Manager

	criticalChains set.Set[ids.ID]

	indexerCtx      context.Context
	chainManagerCtx context.Context
}

// New returns an instance of Node
func New(
	config *Config,
	logger logging.Logger,
	logFactory logging.Factory,
) (*Node, error) {
	metricsRegisterer := prometheus.NewRegistry()
	metricsGatherer := metrics.NewMultiGatherer()

	nodeID := ids.NodeIDFromCert(config.StakingTLSCert.Leaf)
	pop := signer.NewProofOfPossession(config.StakingSigningKey)
	logger.Info("initializing node",
		zap.Stringer("version", version.CurrentApp),
		zap.Stringer("nodeID", nodeID),
		zap.Reflect("nodePOP", pop),
		zap.Reflect("providedFlags", config.ProvidedFlags),
		zap.Reflect("config", config),
	)

	vmFactoryLog, err := logFactory.Make("vm-factory")
	if err != nil {
		return nil, fmt.Errorf("problem creating vm logger: %w", err)
	}

	vmManager := vms.NewManager(vmFactoryLog, config.VMAliaser)

	// initialize beacons
	beacons := validators.NewSet()
	for _, peerID := range config.BootstrapIDs {
		// Note: The beacon connection manager will treat all beaconIDs as
		//       equal.
		// Invariant: We never use the TxID or BLS keys populated here.
		if err := beacons.Add(peerID, nil, ids.Empty, 1); err != nil {
			return nil, err
		}
	}

	tracer, err := trace.New(config.TraceConfig)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize tracer: %w", err)
	}

	consensusRouter := config.ConsensusRouter
	if config.TraceConfig.Enabled {
		consensusRouter = router.Trace(consensusRouter, tracer)
	}

	logger.Info("initializing API server")
	var apiServer server.Server

	if !config.APIAuthConfig.APIRequireAuthToken {
		apiServer, err = server.New(
			logger,
			logFactory,
			config.HTTPConfig.HTTPHost,
			config.HTTPConfig.HTTPPort,
			config.HTTPConfig.APIAllowedOrigins,
			config.HTTPConfig.ShutdownTimeout,
			nodeID,
			config.TraceConfig.Enabled,
			tracer,
			"api",
			metricsRegisterer,
			config.HTTPConfig.HTTPConfig,
		)
	} else {
		authWrapper, err := auth.New(logger, "auth", config.APIAuthConfig.APIAuthPassword)
		if err != nil {
			return nil, err
		}

		// only create auth service if token authorization is required
		logger.Info("API authorization is enabled. Auth tokens must be passed in the header of API requests, except requests to the auth service.")
		authService, err := authWrapper.CreateHandler()
		if err != nil {
			return nil, err
		}
		handler := &common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler:     authService,
		}
		apiServer, err = server.New(
			logger,
			logFactory,
			config.HTTPConfig.HTTPHost,
			config.HTTPConfig.HTTPPort,
			config.HTTPConfig.APIAllowedOrigins,
			config.HTTPConfig.ShutdownTimeout,
			nodeID,
			config.TraceConfig.Enabled,
			tracer,
			"api",
			metricsRegisterer,
			config.HTTPConfig.HTTPConfig,
			authWrapper,
		)
		if err != nil {
			return nil, err
		}
		if err := apiServer.AddRoute(handler, &sync.RWMutex{}, "auth", ""); err != nil {
			return nil, err
		}
	}

	if !config.MetricsAPIEnabled {
		logger.Info("skipping metrics API initialization because it has " +
			"been disabled")
	} else {
		if err := metricsGatherer.Register(constants.PlatformName, metricsRegisterer); err != nil {
			return nil, err
		}

		// Current state of process metrics.
		processCollector := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
		if err := metricsRegisterer.Register(processCollector); err != nil {
			return nil, err
		}

		// Go process metrics using debug.GCStats.
		goCollector := collectors.NewGoCollector()
		if err := metricsRegisterer.Register(goCollector); err != nil {
			return nil, err
		}

		logger.Info("initializing metrics API")

		if err := apiServer.AddRoute(
			&common.HTTPHandler{
				LockOptions: common.NoLock,
				Handler: promhttp.HandlerFor(
					metricsGatherer,
					promhttp.HandlerOpts{},
				),
			},
			&sync.RWMutex{},
			"metrics",
			"",
		); err != nil {
			return nil, err
		}
	}

	var dbManager manager.Manager

	switch config.Name {
	case leveldb.Name:
		dbManager, err = manager.NewLevelDB(
			config.Path,
			config.Config,
			logger,
			version.CurrentDatabase,
			"db_internal",
			metricsRegisterer,
		)
	case memdb.Name:
		dbManager = manager.NewMemDB(version.CurrentDatabase)
	default:
		err = fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s}",
			config.Name,
			leveldb.Name,
			memdb.Name,
		)
	}
	if err != nil {
		return nil, err
	}

	meterDBManager, err := dbManager.NewMeterDBManager(
		"db",
		metricsRegisterer,
	)
	if err != nil {
		return nil, err
	}

	dbManager = meterDBManager

	currentDB := dbManager.Current()
	logger.Info("initializing database",
		zap.Stringer("dbVersion", currentDB.Version),
	)
	db := currentDB.Database

	rawExpectedGenesisHash := hashing.ComputeHash256(config.GenesisBytes)

	rawGenesisHash, err := db.Get(genesisHashKey)
	if err == database.ErrNotFound {
		rawGenesisHash = rawExpectedGenesisHash
		err = db.Put(genesisHashKey, rawGenesisHash)
	}
	if err != nil {
		return nil, err
	}

	genesisHash, err := ids.ToID(rawGenesisHash)
	if err != nil {
		return nil, err
	}
	expectedGenesisHash, err := ids.ToID(rawExpectedGenesisHash)
	if err != nil {
		return nil, err
	}

	if genesisHash != expectedGenesisHash {
		return nil, fmt.Errorf("db contains invalid genesis hash. DB Genesis: %s Generated Genesis: %s", genesisHash, expectedGenesisHash)
	}

	logger.Info("initializing keystore")
	keystoreDB := dbManager.NewPrefixDBManager([]byte("keystore"))
	keystore := keystore.New(logger, keystoreDB)
	keystoreHandler, err := keystore.CreateHandler()
	if err != nil {
		return nil, err
	}
	if !config.KeystoreAPIEnabled {
		logger.Info("skipping keystore API initialization because it has been disabled")
	} else {
		logger.Warn("initializing deprecated keystore API")
		handler := &common.HTTPHandler{
			LockOptions: common.NoLock,
			Handler:     keystoreHandler,
		}
		if err := apiServer.AddRoute(handler, &sync.RWMutex{}, "keystore", ""); err != nil {
			return nil, err
		}
	}

	logger.Info("initializing SharedMemory")
	sharedMemoryDB := prefixdb.New([]byte("shared memory"), db)

	sharedMemory := atomic.NewMemory(sharedMemoryDB)

	// message.Creator is shared between networking, chainManager and the engine.
	// message.Creator currently record metrics under network namespace
	networkNamespace := "network"
	msgCreator, err := message.NewCreator(
		logger,
		metricsRegisterer,
		networkNamespace,
		constants.DefaultNetworkCompressionType,
		config.NetworkConfig.MaximumInboundMessageTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("problem initializing message creator: %w", err)
	}

	validatorManager := validators.NewManager()
	primaryNetworkValidators := validators.NewSet()
	_ = validatorManager.Add(constants.PrimaryNetworkID, primaryNetworkValidators)

	resourceManager := resource.NewManager(
		config.Path,
		config.SystemTrackerFrequency,
		config.SystemTrackerCPUHalflife,
		config.SystemTrackerDiskHalflife,
	)
	resourceManager.TrackProcess(os.Getpid())

	resourceTracker, err := tracker.NewResourceTracker(metricsRegisterer, resourceManager, &meter.ContinuousFactory{}, config.SystemTrackerProcessingHalflife)
	if err != nil {
		return nil, err
	}

	cpuTargeter := tracker.NewTargeter(&config.CPUTargeterConfig, primaryNetworkValidators, resourceTracker.CPUTracker())
	diskTargeter := tracker.NewTargeter(&config.CPUTargeterConfig, primaryNetworkValidators, resourceTracker.DiskTracker())

	currentIPPort := config.IPPort.IPPort()
	listener, err := net.Listen(constants.NetworkType, fmt.Sprintf(":%d", currentIPPort.Port))
	if err != nil {
		return nil, err
	}
	// Wrap listener so it will only accept a certain number of incoming connections per second
	listener = throttling.NewThrottledListener(listener, config.NetworkConfig.ThrottlerConfig.MaxInboundConnsPerSec)

	ipPort, err := ips.ToIPPort(listener.Addr().String())
	if err != nil {
		logger.Info("initializing networking",
			zap.Stringer("currentNodeIP", currentIPPort),
		)
	} else {
		ipPort = ips.IPPort{
			IP:   currentIPPort.IP,
			Port: ipPort.Port,
		}
		logger.Info("initializing networking",
			zap.Stringer("currentNodeIP", ipPort),
		)
	}

	if !config.EnableStaking {
		// Staking is disabled so we don't have a txID that added us as a
		// validator. Because each validator needs a txID associated with it, we
		// hack one together by just padding our nodeID with zeroes.
		dummyTxID := ids.Empty
		copy(dummyTxID[:], nodeID[:])

		err := primaryNetworkValidators.Add(
			nodeID,
			bls.PublicFromSecretKey(config.StakingSigningKey),
			dummyTxID,
			config.DisabledStakingWeight,
		)
		if err != nil {
			return nil, err
		}

		consensusRouter = &insecureValidatorManager{
			Router: consensusRouter,
			vdrs:   primaryNetworkValidators,
			weight: config.DisabledStakingWeight,
		}
	}

	numBeacons := beacons.Len()
	requiredConns := (3*numBeacons + 3) / 4

	shuttingDown := &utils.Atomic[bool]{}

	if requiredConns > 0 {
		// Set a timer that will fire after a given timeout unless we connect
		// to a sufficient portion of nodes. If the timeout fires, the node will
		// shutdown.
		timer := timer.NewTimer(func() {
			// If the timeout fires and we're already shutting down, nothing to do.
			if !shuttingDown.Get() {
				logger.Warn("failed to connect to bootstrap nodes",
					zap.Stringer("beacons", beacons),
					zap.Duration("duration", config.BootstrapBeaconConnectionTimeout),
				)
			}
		})

		go timer.Dispatch()
		timer.SetTimeoutIn(config.BootstrapBeaconConnectionTimeout)

		consensusRouter = &beaconManager{
			Router:        consensusRouter,
			timer:         timer,
			beacons:       beacons,
			requiredConns: int64(requiredConns),
		}
	}

	// initialize gossip tracker
	gossipTracker, err := peer.NewGossipTracker(metricsRegisterer, networkNamespace)
	if err != nil {
		return nil, err
	}

	// keep gossip tracker synchronized with the validator set
	primaryNetworkValidators.RegisterCallbackListener(&peer.GossipTrackerCallback{
		Log:           logger,
		GossipTracker: gossipTracker,
	})

	tlsKey, ok := config.StakingTLSCert.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errInvalidTLSKey
	}

	var tlsKeyLogWriterCloser io.WriteCloser
	if config.NetworkConfig.TLSKeyLogFile != "" {
		tlsKeyLogWriterCloser, err = perms.Create(config.NetworkConfig.TLSKeyLogFile, perms.ReadWrite)
		if err != nil {
			return nil, err
		}
		logger.Warn("TLS key logging is enabled",
			zap.String("filename", config.NetworkConfig.TLSKeyLogFile),
		)
	}

	tlsConfig := peer.TLSConfig(config.StakingTLSCert, tlsKeyLogWriterCloser)
	uptimeCalculator := uptime.NewLockedCalculator()

	// add node configs to network config
	config.NetworkConfig.Namespace = networkNamespace
	config.NetworkConfig.MyNodeID = nodeID
	config.NetworkConfig.MyIPPort = config.IPPort
	config.NetworkConfig.NetworkID = config.NetworkID
	config.NetworkConfig.Validators = validatorManager
	config.NetworkConfig.Beacons = beacons
	config.NetworkConfig.TLSConfig = tlsConfig
	config.NetworkConfig.TLSKey = tlsKey
	config.NetworkConfig.TrackedSubnets = config.TrackedSubnets
	config.NetworkConfig.UptimeCalculator = uptimeCalculator
	config.NetworkConfig.UptimeRequirement = config.UptimeRequirement
	config.NetworkConfig.ResourceTracker = resourceTracker
	config.NetworkConfig.CPUTargeter = cpuTargeter
	config.NetworkConfig.DiskTargeter = diskTargeter
	config.NetworkConfig.GossipTracker = gossipTracker

	network, err := network.NewNetwork(
		&config.NetworkConfig,
		msgCreator,
		metricsRegisterer,
		logger,
		listener,
		dialer.NewDialer(constants.NetworkType, config.NetworkConfig.DialerConfig, logger),
		consensusRouter,
	)
	if err != nil {
		return nil, err
	}

	blockAcceptorGroup := snow.NewAcceptorGroup(logger)
	txAcceptorGroup := snow.NewAcceptorGroup(logger)
	vertexAcceptorGroup := snow.NewAcceptorGroup(logger)

	logger.Info("adding the default VM aliases")
	vmAliases := genesis.GetVMAliases()

	for vmID, aliases := range vmAliases {
		for _, alias := range aliases {
			if err := config.VMAliaser.Alias(vmID, alias); err != nil {
				return nil, err
			}
		}
	}

	chainIDs := make([]ids.ID, len(config.IPCDefaultChainIDs))
	for i, chainID := range config.IPCDefaultChainIDs {
		id, err := ids.FromString(chainID)
		if err != nil {
			return nil, err
		}
		chainIDs[i] = id
	}

	chainIPCs, err := ipcs.NewChainIPCs(
		logger,
		config.IPCPath,
		config.NetworkID,
		blockAcceptorGroup,
		txAcceptorGroup,
		vertexAcceptorGroup,
		chainIDs,
	)
	if err != nil {
		return nil, err
	}

	createAVMTx, err := genesis.VMGenesis(config.GenesisBytes, constants.AVMID)
	if err != nil {
		return nil, err
	}
	xChainID := createAVMTx.ID()

	createEVMTx, err := genesis.VMGenesis(config.GenesisBytes, constants.EVMID)
	if err != nil {
		return nil, err
	}
	cChainID := createEVMTx.ID()

	benchlistManager := benchlist.NewManager(config.BenchlistConfig, consensusRouter, validatorManager, config.EnableStaking)

	// Manages network timeouts
	timeoutManager, err := timeout.NewManager(
		&config.AdaptiveTimeoutConfig,
		benchlistManager,
		"requests",
		metricsRegisterer,
	)
	if err != nil {
		return nil, err
	}

	// If any of these chains die, the node shuts down
	criticalChains := set.Set[ids.ID]{}
	criticalChains.Add(
		constants.PlatformChainID,
		xChainID,
		cChainID,
	)

	health, err := health.New(logger, metricsRegisterer)
	if err != nil {
		return nil, err
	}

	chainManagerCtx, chainManagerCancel := context.WithCancel(context.Background())
	chainManager := chains.New(&chains.ManagerConfig{
		StakingEnabled:                          config.EnableStaking,
		StakingCert:                             config.StakingTLSCert,
		StakingBLSKey:                           config.StakingSigningKey,
		Log:                                     logger,
		LogFactory:                              logFactory,
		VMManager:                               vmManager,
		BlockAcceptorGroup:                      blockAcceptorGroup,
		TxAcceptorGroup:                         txAcceptorGroup,
		VertexAcceptorGroup:                     vertexAcceptorGroup,
		DBManager:                               dbManager,
		MsgCreator:                              msgCreator,
		Router:                                  consensusRouter,
		Net:                                     network,
		Validators:                              validatorManager,
		NodeID:                                  nodeID,
		NetworkID:                               config.NetworkID,
		Server:                                  apiServer,
		Keystore:                                keystore,
		AtomicMemory:                            sharedMemory,
		AVAXAssetID:                             config.AvaxAssetID,
		XChainID:                                xChainID,
		CChainID:                                cChainID,
		CriticalChains:                          criticalChains,
		TimeoutManager:                          timeoutManager,
		Health:                                  health,
		RetryBootstrap:                          config.RetryBootstrap,
		RetryBootstrapWarnFrequency:             config.RetryBootstrapWarnFrequency,
		MeterVMEnabled:                          config.MeterVMEnabled,
		Metrics:                                 metricsGatherer,
		SubnetConfigs:                           config.SubnetConfigs,
		ChainConfigs:                            config.ChainConfigs,
		ConsensusGossipFrequency:                config.ConsensusGossipFrequency,
		ConsensusAppConcurrency:                 config.ConsensusAppConcurrency,
		BootstrapMaxTimeGetAncestors:            config.BootstrapMaxTimeGetAncestors,
		BootstrapAncestorsMaxContainersSent:     config.BootstrapAncestorsMaxContainersSent,
		BootstrapAncestorsMaxContainersReceived: config.BootstrapAncestorsMaxContainersReceived,
		ApricotPhase4Time:                       version.GetApricotPhase4Time(config.NetworkID),
		ApricotPhase4MinPChainHeight:            version.GetApricotPhase4MinPChainHeight(config.NetworkID),
		ResourceTracker:                         resourceTracker,
		StateSyncBeacons:                        config.StateSyncIDs,
		TracingEnabled:                          config.TraceConfig.Enabled,
		Tracer:                                  tracer,
		ChainDataDir:                            config.ChainDataDir,
	}, chainManagerCancel)

	logger.Info("initializing VMs")

	pChainValidators := validatorManager

	// If staking is disabled, ignore updates to Subnets' validator sets
	// Instead of updating node's validator manager, platform chain makes changes
	// to its own local validator manager (which isn't used for sampling)
	if !config.EnableStaking {
		pChainValidators = validators.NewManager()
		primaryVdrs := validators.NewSet()
		_ = pChainValidators.Add(constants.PrimaryNetworkID, primaryVdrs)
	}

	vmRegisterer := registry.NewVMRegisterer(registry.VMRegistererConfig{
		APIServer:    apiServer,
		Log:          logger,
		VMFactoryLog: vmFactoryLog,
		VMManager:    vmManager,
	})

	// Register the VMs that Avalanche supports
	errs := wrappers.Errs{}
	errs.Add(
		vmRegisterer.Register(context.TODO(), constants.PlatformVMID, &platformvm.Factory{
			Config: platformconfig.Config{
				Chains:                          chainManager,
				Validators:                      pChainValidators,
				UptimeLockedCalculator:          uptimeCalculator,
				StakingEnabled:                  config.EnableStaking,
				TrackedSubnets:                  config.TrackedSubnets,
				TxFee:                           config.TxFee,
				CreateAssetTxFee:                config.CreateAssetTxFee,
				CreateSubnetTxFee:               config.CreateSubnetTxFee,
				TransformSubnetTxFee:            config.TransformSubnetTxFee,
				CreateBlockchainTxFee:           config.CreateBlockchainTxFee,
				AddPrimaryNetworkValidatorFee:   config.AddPrimaryNetworkValidatorFee,
				AddPrimaryNetworkDelegatorFee:   config.AddPrimaryNetworkDelegatorFee,
				AddSubnetValidatorFee:           config.AddSubnetValidatorFee,
				AddSubnetDelegatorFee:           config.AddSubnetDelegatorFee,
				UptimePercentage:                config.UptimeRequirement,
				MinValidatorStake:               config.MinValidatorStake,
				MaxValidatorStake:               config.MaxValidatorStake,
				MinDelegatorStake:               config.MinDelegatorStake,
				MinDelegationFee:                config.MinDelegationFee,
				MinStakeDuration:                config.MinStakeDuration,
				MaxStakeDuration:                config.MaxStakeDuration,
				RewardConfig:                    config.RewardConfig,
				ApricotPhase3Time:               version.GetApricotPhase3Time(config.NetworkID),
				ApricotPhase5Time:               version.GetApricotPhase5Time(config.NetworkID),
				BanffTime:                       version.GetBanffTime(config.NetworkID),
				CortinaTime:                     version.GetCortinaTime(config.NetworkID),
				MinPercentConnectedStakeHealthy: config.MinPercentConnectedStakeHealthy,
				UseCurrentHeight:                config.UseCurrentHeight,
			},
		}),
		vmRegisterer.Register(context.TODO(), constants.AVMID, &avm.Factory{
			Config: avmconfig.Config{
				TxFee:            config.TxFee,
				CreateAssetTxFee: config.CreateAssetTxFee,
			},
		}),
		vmRegisterer.Register(context.TODO(), constants.EVMID, &coreth.Factory{}),
		vmManager.RegisterFactory(context.TODO(), secp256k1fx.ID, &secp256k1fx.Factory{}),
		vmManager.RegisterFactory(context.TODO(), nftfx.ID, &nftfx.Factory{}),
		vmManager.RegisterFactory(context.TODO(), propertyfx.ID, &propertyfx.Factory{}),
	)
	if errs.Errored() {
		return nil, errs.Err
	}

	// initialize vm runtime manager
	runtimeManager := runtime.NewManager()

	// initialize the vm registry
	vmRegistry := registry.NewVMRegistry(registry.VMRegistryConfig{
		VMGetter: registry.NewVMGetter(registry.VMGetterConfig{
			FileReader:      filesystem.NewReader(),
			Manager:         vmManager,
			PluginDirectory: config.PluginDir,
			CPUTracker:      resourceManager,
			RuntimeTracker:  runtimeManager,
		}),
		VMRegisterer: vmRegisterer,
	})

	// register any vms that need to be installed as plugins from disk
	_, failedVMs, err := vmRegistry.Reload(context.TODO())
	if err != nil {
		return nil, err
	}
	for failedVM, err := range failedVMs {
		logger.Error("failed to register VM",
			zap.Stringer("vmID", failedVM),
			zap.Error(err),
		)
	}

	logger.Info("initializing chain aliases")
	_, chainAliases, err := genesis.Aliases(config.GenesisBytes)
	if err != nil {
		return nil, err
	}

	for chainID, aliases := range chainAliases {
		for _, alias := range aliases {
			if err := chainManager.Alias(chainID, alias); err != nil {
				return nil, err
			}
		}
	}

	for chainID, aliases := range config.ChainAliases {
		for _, alias := range aliases {
			if err := chainManager.Alias(chainID, alias); err != nil {
				return nil, err
			}
		}
	}

	logger.Info("initializing API aliases")
	apiAliases, _, err := genesis.Aliases(config.GenesisBytes)
	if err != nil {
		return nil, err
	}

	for url, aliases := range apiAliases {
		if err := apiServer.AddAliases(url, aliases...); err != nil {
			return nil, err
		}
	}

	txIndexerDB := prefixdb.New(indexerDBPrefix, db)

	indexerCtx, indexerCancelF := context.WithCancel(context.Background())
	indexer, err := indexer.NewIndexer(indexer.Config{
		IndexingEnabled:      config.IndexAPIEnabled,
		AllowIncompleteIndex: config.IndexAllowIncomplete,
		DB:                   txIndexerDB,
		Log:                  logger,
		BlockAcceptorGroup:   blockAcceptorGroup,
		TxAcceptorGroup:      txAcceptorGroup,
		VertexAcceptorGroup:  vertexAcceptorGroup,
		APIServer:            apiServer,
	}, indexerCancelF)
	if err != nil {
		return nil, fmt.Errorf("couldn't create index for txs: %w", err)
	}

	var continuousProfiler profiler.ContinuousProfiler
	if !config.ProfilerConfig.Enabled {
		logger.Info("skipping profiler initialization because it has been disabled")
		continuousProfiler = profiler.NoOpContinuousProfiler{
			Close: make(chan struct{}),
		}
	} else {
		logger.Info("initializing continuous profiler")
		continuousProfiler = profiler.NewContinuous(
			filepath.Join(config.ProfilerConfig.Dir, "continuous"),
			config.ProfilerConfig.Freq,
			config.ProfilerConfig.MaxNumFiles,
		)
	}

	return &Node{
		consensusRouter:       consensusRouter,
		Log:                   logger,
		VMFactoryLog:          vmFactoryLog,
		LogFactory:            logFactory,
		ID:                    nodeID,
		DBManager:             dbManager,
		DB:                    db,
		profiler:              continuousProfiler,
		indexer:               indexer,
		keystore:              keystore,
		sharedMemory:          sharedMemory,
		health:                health,
		msgCreator:            msgCreator,
		chainManager:          chainManager,
		uptimeCalculator:      uptimeCalculator,
		BlockAcceptorGroup:    blockAcceptorGroup,
		TxAcceptorGroup:       txAcceptorGroup,
		VertexAcceptorGroup:   vertexAcceptorGroup,
		IPCs:                  chainIPCs,
		networkNamespace:      networkNamespace,
		Net:                   network,
		tlsKeyLogWriterCloser: tlsKeyLogWriterCloser,
		beacons:               beacons,
		vdrs:                  validatorManager,
		APIServer:             apiServer,
		Config:                config,
		tracer:                tracer,
		shutdownOnce:          sync.Once{},
		shuttingDown:          shuttingDown,
		shuttingDownExitCode:  utils.Atomic[int]{},
		MetricsRegisterer:     metricsRegisterer,
		MetricsGatherer:       metricsGatherer,
		VMManager:             vmManager,
		VMRegistry:            vmRegistry,
		runtimeManager:        runtimeManager,
		resourceManager:       resourceManager,
		resourceTracker:       resourceTracker,
		cpuTargeter:           cpuTargeter,
		diskTargeter:          diskTargeter,
		timeoutManager:        timeoutManager,
		criticalChains:        criticalChains,
		benchlistManager:      benchlistManager,
		indexerCtx:            indexerCtx,
		chainManagerCtx:       chainManagerCtx,
	}, nil
}

// Start starts the node's servers.
// Returns when the node exits.
func (n *Node) Start() error {
	go func() {
		for {
			select {
			case <-n.indexerCtx.Done():
				n.Shutdown(1)
				return
			case <-n.chainManagerCtx.Done():
				n.Shutdown(1)
				return
			}
		}
	}()

	go n.Log.RecoverAndPanic(n.timeoutManager.Dispatch)

	go n.Log.RecoverAndPanic(func() {
		err := n.profiler.Dispatch()
		if err != nil {
			n.Log.Fatal("continuous profiler failed",
				zap.Error(err),
			)
		}
		n.Shutdown(1)
	})

	n.health.Start(context.TODO(), n.Config.HealthCheckFreq)

	// Routes incoming messages from peers to the appropriate chain
	if err := n.consensusRouter.Initialize(
		n.ID,
		n.Log,
		n.timeoutManager,
		n.Config.ConsensusShutdownTimeout,
		n.criticalChains,
		n.Config.EnableStaking,
		n.Config.TrackedSubnets,
		n.Shutdown,
		n.Config.RouterHealthConfig,
		"requests",
		n.MetricsRegisterer,
	); err != nil {
		return fmt.Errorf("couldn't initialize chain router: %w", err)
	}

	// Notify subscribers when new chains are created
	n.chainManager.AddRegistrant(n.APIServer)
	n.chainManager.AddRegistrant(n.indexer)

	// Start the Platform chain
	n.Log.Info("initializing chains")

	platformChain := chains.ChainParameters{
		ID:            constants.PlatformChainID,
		SubnetID:      constants.PrimaryNetworkID,
		GenesisData:   n.Config.GenesisBytes, // Specifies other chains to create
		VMID:          constants.PlatformVMID,
		CustomBeacons: n.beacons,
	}

	// Start the chain creator with the Platform Chain
	if err := n.chainManager.StartChainCreator(platformChain); err != nil {
		return err
	}

	// Admin API
	if !n.Config.AdminAPIEnabled {
		n.Log.Info("skipping admin API initialization because it has been disabled")
	} else {
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
		if err := n.APIServer.AddRoute(service, &sync.RWMutex{}, "admin", ""); err != nil {
			return err
		}
	}

	// Info API
	if !n.Config.InfoAPIEnabled {
		n.Log.Info("skipping info API initialization because it has been disabled")
	} else {
		n.Log.Info("initializing info API")

		primaryValidators, _ := n.vdrs.Get(constants.PrimaryNetworkID)
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
			primaryValidators,
			n.benchlistManager,
		)
		if err != nil {
			return err
		}
		if err := n.APIServer.AddRoute(service, &sync.RWMutex{}, "info", ""); err != nil {
			return err
		}
	}

	if !n.Config.IPCAPIEnabled {
		n.Log.Info("skipping ipc API initialization because it has been disabled")
	} else {
		n.Log.Warn("initializing deprecated ipc API")
		service, err := ipcsapi.NewService(n.Log, n.chainManager, n.APIServer, n.IPCs)
		if err != nil {
			return err
		}
		if err := n.APIServer.AddRoute(service, &sync.RWMutex{}, "ipcs", ""); err != nil {
			return err
		}
	}

	if !n.Config.HealthAPIEnabled {
		n.Log.Info("skipping health API initialization because it has been disabled")
	} else {
		n.Log.Info("initializing Health API")
		if err := n.health.RegisterHealthCheck("network", n.Net, health.GlobalTag); err != nil {
			return fmt.Errorf("couldn't register network health check: %w", err)
		}

		if err := n.health.RegisterHealthCheck("router", n.consensusRouter, health.GlobalTag); err != nil {
			return fmt.Errorf("couldn't register router health check: %w", err)
		}

		// TODO: add database health to liveness check
		if err := n.health.RegisterHealthCheck("database", n.DB, health.GlobalTag); err != nil {
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

		if err := n.health.RegisterHealthCheck("diskspace", diskSpaceCheck, health.GlobalTag); err != nil {
			return fmt.Errorf("couldn't register resource health check: %w", err)
		}

		handler, err := health.NewGetAndPostHandler(n.Log, n.health)
		if err != nil {
			return err
		}

		if err := n.APIServer.AddRoute(
			&common.HTTPHandler{
				LockOptions: common.NoLock,
				Handler:     handler,
			},
			&sync.RWMutex{},
			"health",
			"",
		); err != nil {
			return err
		}

		if err := n.APIServer.AddRoute(
			&common.HTTPHandler{
				LockOptions: common.NoLock,
				Handler:     health.NewGetHandler(n.health.Readiness),
			},
			&sync.RWMutex{},
			"health",
			"/readiness",
		); err != nil {
			return err
		}

		if err := n.APIServer.AddRoute(
			&common.HTTPHandler{
				LockOptions: common.NoLock,
				Handler:     health.NewGetHandler(n.health.Health),
			},
			&sync.RWMutex{},
			"health",
			"/health",
		); err != nil {
			return err
		}

		if err := n.APIServer.AddRoute(
			&common.HTTPHandler{
				LockOptions: common.NoLock,
				Handler:     health.NewGetHandler(n.health.Liveness),
			},
			&sync.RWMutex{},
			"health",
			"/liveness",
		); err != nil {
			return err
		}
	}

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
		// This causes [n.APIServer].Start() to return an error.
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
	for i, peerIP := range n.Config.BootstrapIPs {
		n.Net.ManuallyTrack(n.Config.BootstrapIDs[i], peerIP)
	}

	// Start P2P connections
	err := n.Net.Dispatch()

	// If the P2P server isn't running, shut down the node.
	// If node is already shutting down, this does nothing.
	n.Shutdown(1)

	if n.tlsKeyLogWriterCloser != nil {
		if err := n.tlsKeyLogWriterCloser.Close(); err != nil {
			n.Log.Error("closing TLS key log file failed",
				zap.String("filename", n.Config.NetworkConfig.TLSKeyLogFile),
				zap.Error(err),
			)
		}
	}

	return err
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

	// Passes if the node is not shutting down
	shuttingDownCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		return map[string]interface{}{
			"isShuttingDown": true,
		}, errShuttingDown
	})

	err := n.health.RegisterHealthCheck("shuttingDown", shuttingDownCheck, health.GlobalTag)
	if err != nil {
		n.Log.Debug("couldn't register shuttingDown health check",
			zap.Error(err),
		)
	}

	time.Sleep(n.Config.ShutdownWait)

	n.resourceManager.Shutdown()
	if err := n.IPCs.Shutdown(); err != nil {
		n.Log.Debug("error during IPC shutdown",
			zap.Error(err),
		)
	}
	n.chainManager.Shutdown()
	n.profiler.Shutdown()
	n.Net.StartClose()
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

	if err := n.DBManager.Close(); err != nil {
		n.Log.Warn("error during DB shutdown",
			zap.Error(err),
		)
	}

	if n.Config.TraceConfig.Enabled {
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
