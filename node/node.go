// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

// #include "salticidae/network.h"
// void onTerm(int sig, void *);
// void errorHandler(SalticidaeCError *, bool, void *);
import "C"

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"sync"
	"unsafe"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/api/admin"
	"github.com/ava-labs/gecko/api/ipcs"
	"github.com/ava-labs/gecko/api/keystore"
	"github.com/ava-labs/gecko/api/metrics"
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/networking"
	"github.com/ava-labs/gecko/networking/xputtest"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/evm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/vms/timestampvm"
)

const (
	maxMessageSize = 1 << 25 // maximum size of a message sent with salticidae
)

// MainNode is the reference for node callbacks
var MainNode = Node{}

// Node is an instance of an Ava node.
type Node struct {
	Log        logging.Logger
	LogFactory logging.Factory
	HTTPLog    logging.Logger

	// This node's unique ID used when communicating with other nodes
	// (in consensus, for example)
	ID ids.ShortID

	// Storage for this node
	DB database.Database

	// Handles calls to Keystore API
	keystoreServer keystore.Keystore

	// Manages creation of blockchains and routing messages to them
	chainManager chains.Manager

	// Manages Virtual Machines
	vmManager vms.Manager

	// dispatcher for events as they happen in consensus
	DecisionDispatcher  *triggers.EventDispatcher
	ConsensusDispatcher *triggers.EventDispatcher

	// Event loop manager
	EC salticidae.EventContext
	// Network that manages validator peers
	PeerNet salticidae.PeerNetwork
	// Network that manages clients
	ClientNet salticidae.MsgNetwork // TODO: Remove

	// API that handles new connections
	ValidatorAPI *networking.Handshake
	// API that handles voting messages
	ConsensusAPI *networking.Voting

	// current validators of the network
	vdrs validators.Manager

	// APIs that handle client messages
	// TODO: Remove
	Issuer     *xputtest.Issuer
	CClientAPI *xputtest.CClient

	// Handles HTTP API calls
	APIServer api.Server

	// This node's configuration
	Config *Config
}

/*
 ******************************************************************************
 *************************** P2P Networking Section ***************************
 ******************************************************************************
 */

//export onTerm
func onTerm(C.int, unsafe.Pointer) {
	MainNode.Log.Debug("Terminate signal received")
	MainNode.EC.Stop()
}

//export errorHandler
func errorHandler(_err *C.struct_SalticidaeCError, fatal C.bool, _ unsafe.Pointer) {
	err := (*salticidae.Error)(unsafe.Pointer(_err))
	if fatal {
		MainNode.Log.Fatal("Error during async call: %s", salticidae.StrError(err.GetCode()))
		MainNode.EC.Stop()
		return
	}
	MainNode.Log.Error("Error during async call: %s", salticidae.StrError(err.GetCode()))
}

func (n *Node) initNetlib() error {
	// Create main event context
	n.EC = salticidae.NewEventContext()

	// Set up interrupt signal and terminate signal handlers
	evInt := salticidae.NewSigEvent(n.EC, salticidae.SigEventCallback(C.onTerm), nil)
	evInt.Add(salticidae.SIGINT)
	evTerm := salticidae.NewSigEvent(n.EC, salticidae.SigEventCallback(C.onTerm), nil)
	evTerm.Add(salticidae.SIGTERM)

	// Create peer network config, may have tls enabled
	peerConfig := salticidae.NewPeerNetworkConfig()
	if n.Config.EnableStaking {
		msgConfig := peerConfig.AsMsgNetworkConfig()
		msgConfig.MaxMsgSize(maxMessageSize)
		msgConfig.EnableTLS(true)
		msgConfig.TLSKeyFile(n.Config.StakingKeyFile)
		msgConfig.TLSCertFile(n.Config.StakingCertFile)
	}

	// Create the peer network
	err := salticidae.NewError()
	n.PeerNet = salticidae.NewPeerNetwork(n.EC, peerConfig, &err)
	if code := err.GetCode(); code != 0 {
		return errors.New(salticidae.StrError(code))
	}
	// Add peer network error handling
	net := n.PeerNet.AsMsgNetwork()
	net.RegErrorHandler(salticidae.MsgNetworkErrorCallback(C.errorHandler), nil)

	if n.Config.ThroughputServerEnabled {
		// Create the client network
		msgConfig := salticidae.NewMsgNetworkConfig()
		msgConfig.MaxMsgSize(maxMessageSize)
		n.ClientNet = salticidae.NewMsgNetwork(n.EC, msgConfig, &err)
		if code := err.GetCode(); code != 0 {
			return errors.New(salticidae.StrError(code))
		}
		// Add client network error handling
		n.ClientNet.RegErrorHandler(salticidae.MsgNetworkErrorCallback(C.errorHandler), nil)
	}

	return nil
}

func (n *Node) initValidatorNet() error {
	// Initialize validator manager and default subnet's validator set
	defaultSubnetValidators := validators.NewSet()
	if !n.Config.EnableStaking {
		defaultSubnetValidators.Add(validators.NewValidator(n.ID, 1))
	}
	n.vdrs = validators.NewManager()
	n.vdrs.PutValidatorSet(platformvm.DefaultSubnetID, defaultSubnetValidators)

	cErr := salticidae.NewError()
	serverIP := salticidae.NewNetAddrFromIPPortString(n.Config.StakingIP.String(), true, &cErr)
	if code := cErr.GetCode(); code != 0 {
		return errors.New(salticidae.StrError(code))
	}

	n.ValidatorAPI = &networking.HandshakeNet
	n.ValidatorAPI.Initialize(
		/*log=*/ n.Log,
		/*validators=*/ defaultSubnetValidators,
		/*myIP=*/ serverIP,
		/*myID=*/ n.ID,
		/*network=*/ n.PeerNet,
		/*metrics=*/ n.Config.ConsensusParams.Metrics,
		/*enableStaking=*/ n.Config.EnableStaking,
		/*networkID=*/ n.Config.NetworkID,
	)

	return nil
}

func (n *Node) initConsensusNet() {
	vdrs, ok := n.vdrs.GetValidatorSet(platformvm.DefaultSubnetID)
	n.Log.AssertTrue(ok, "should have initialize the validator set already")

	n.ConsensusAPI = &networking.VotingNet
	n.ConsensusAPI.Initialize(n.Log, vdrs, n.PeerNet, n.ValidatorAPI.Connections(), n.chainManager.Router(), n.Config.ConsensusParams.Metrics)

	n.Log.AssertNoError(n.ConsensusDispatcher.Register("gossip", n.ConsensusAPI))
}

func (n *Node) initClients() {
	n.Issuer = &xputtest.Issuer{}
	n.Issuer.Initialize(n.Log)

	n.CClientAPI = &xputtest.CClientHandler
	n.CClientAPI.Initialize(n.ClientNet, n.Issuer)

	n.chainManager.AddRegistrant(n.Issuer)
}

// StartConsensusServer starts the P2P server this node uses to communicate
// with other nodes
func (n *Node) StartConsensusServer() error {
	n.Log.Verbo("starting the consensus server")

	n.PeerNet.AsMsgNetwork().Start()

	err := salticidae.NewError()

	// The IP this node listens on for P2P messaging
	serverIP := salticidae.NewNetAddrFromIPPortString(n.Config.StakingIP.String(), true, &err)
	if code := err.GetCode(); code != 0 {
		return fmt.Errorf("failed to create ip addr: %s", salticidae.StrError(code))
	}

	// Listen for P2P messages
	n.PeerNet.Listen(serverIP, &err)
	if code := err.GetCode(); code != 0 {
		return fmt.Errorf("failed to start consensus server: %s", salticidae.StrError(code))
	}

	// Start a server to handle throughput tests if configuration says to. Disabled by default.
	if n.Config.ThroughputServerEnabled {
		n.ClientNet.Start()

		clientIP := salticidae.NewNetAddrFromIPPortString(fmt.Sprintf("127.0.0.1:%d", n.Config.ThroughputPort), true, &err)
		if code := err.GetCode(); code != 0 {
			return fmt.Errorf("failed to start xput server: %s", salticidae.StrError(code))
		}

		n.ClientNet.Listen(clientIP, &err)
		if code := err.GetCode(); code != 0 {
			return fmt.Errorf("failed to listen on xput server: %s", salticidae.StrError(code))
		}
	}

	// Add bootstrap nodes to the peer network
	for _, peer := range n.Config.BootstrapPeers {
		if !peer.IP.Equal(n.Config.StakingIP) {
			bootstrapIP := salticidae.NewNetAddrFromIPPortString(peer.IP.String(), true, &err)
			if code := err.GetCode(); code != 0 {
				return fmt.Errorf("failed to create bootstrap ip addr: %s", salticidae.StrError(code))
			}
			n.PeerNet.AddPeer(bootstrapIP)
		} else {
			n.Log.Error("can't add self as a bootstrapper")
		}
	}

	return nil
}

// Dispatch starts the node's servers.
// Returns when the node exits.
func (n *Node) Dispatch() { n.EC.Dispatch() }

/*
 ******************************************************************************
 *********************** End P2P Networking Section ***************************
 ******************************************************************************
 */

func (n *Node) initDatabase() { n.DB = n.Config.DB }

// Initialize this node's ID
// If staking is disabled, a node's ID is a hash of its IP
// Otherwise, it is a hash of the TLS certificate that this node
// uses for P2P communication
func (n *Node) initNodeID() error {
	if !n.Config.EnableStaking {
		n.ID = ids.NewShortID(hashing.ComputeHash160Array([]byte(n.Config.StakingIP.String())))
		n.Log.Info("Set the node's ID to %s", n.ID)
		return nil
	}

	stakeCert, err := ioutil.ReadFile(n.Config.StakingCertFile)
	if err != nil {
		return fmt.Errorf("problem reading staking certificate: %w", err)
	}

	block, _ := pem.Decode(stakeCert)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("problem parsing staking certificate: %w", err)
	}
	n.ID, err = ids.ToShortID(hashing.PubkeyBytesToAddress(cert.Raw))
	if err != nil {
		return fmt.Errorf("problem deriving staker ID from certificate: %w", err)
	}
	n.Log.Info("Set node's ID to %s", n.ID)
	return nil
}

// Create the vmManager and register the following vms:
// AVM, EVM, Simple Payments DAG, Simple Payments Chain
// The Platform VM is registered in initStaking because
// its factory needs to reference n.chainManager, which is nil right now
func (n *Node) initVMManager() {
	n.vmManager = vms.NewManager(&n.APIServer, n.HTTPLog)
	n.vmManager.RegisterVMFactory(avm.ID, &avm.Factory{})
	n.vmManager.RegisterVMFactory(evm.ID, &evm.Factory{})
	n.vmManager.RegisterVMFactory(spdagvm.ID, &spdagvm.Factory{TxFee: n.Config.AvaTxFee})
	n.vmManager.RegisterVMFactory(spchainvm.ID, &spchainvm.Factory{})
	n.vmManager.RegisterVMFactory(secp256k1fx.ID, &secp256k1fx.Factory{})
	n.vmManager.RegisterVMFactory(timestampvm.ID, &timestampvm.Factory{})
}

// Create the EventDispatcher used for hooking events
// into the general process flow.
func (n *Node) initEventDispatcher() {
	n.DecisionDispatcher = &triggers.EventDispatcher{}
	n.DecisionDispatcher.Initialize(n.Log)

	n.ConsensusDispatcher = &triggers.EventDispatcher{}
	n.ConsensusDispatcher.Initialize(n.Log)
}

// Initializes the Platform chain.
// Its genesis data specifies the other chains that should
// be created.
func (n *Node) initChains() {
	n.Log.Info("initializing chains")

	vdrs := n.vdrs

	// If staking is disabled, ignore updates to Subnets' validator sets
	// Instead of updating node's validator manager, platform chain makes changes
	// to its own local validator manager (which isn't used for sampling)
	if !n.Config.EnableStaking {
		defaultSubnetValidators := validators.NewSet()
		defaultSubnetValidators.Add(validators.NewValidator(n.ID, 1))
		vdrs = validators.NewManager()
		vdrs.PutValidatorSet(platformvm.DefaultSubnetID, defaultSubnetValidators)
	}

	n.vmManager.RegisterVMFactory(
		/*vmID=*/ platformvm.ID,
		/*vmFactory=*/ &platformvm.Factory{
			ChainManager: n.chainManager,
			Validators:   vdrs,
		},
	)

	beacons := validators.NewSet()
	for _, peer := range n.Config.BootstrapPeers {
		beacons.Add(validators.NewValidator(peer.ID, 1))
	}

	genesisBytes, _ := genesis.Genesis(n.Config.NetworkID)

	// Create the Platform Chain
	n.chainManager.ForceCreateChain(chains.ChainParameters{
		ID:            ids.Empty,
		SubnetID:      platformvm.DefaultSubnetID,
		GenesisData:   genesisBytes, // Specifies other chains to create
		VMAlias:       platformvm.ID.String(),
		CustomBeacons: beacons,
	})
}

// initAPIServer initializes the server that handles HTTP calls
func (n *Node) initAPIServer() {
	n.Log.Info("Initializing API server")

	n.APIServer.Initialize(n.Log, n.LogFactory, n.Config.HTTPPort)

	if n.Config.EnableHTTPS {
		n.Log.Debug("Initializing API server with TLS Enabled")
		go n.Log.RecoverAndPanic(func() {
			if err := n.APIServer.DispatchTLS(n.Config.HTTPSCertFile, n.Config.HTTPSKeyFile); err != nil {
				n.Log.Warn("API server initialization failed with %s, attempting to create insecure API server", err)
				n.APIServer.Dispatch()
			}
		})
	} else {
		n.Log.Debug("Initializing API server with TLS Disabled")
		go n.Log.RecoverAndPanic(func() { n.APIServer.Dispatch() })
	}
}

// Assumes n.DB, n.vdrs all initialized (non-nil)
func (n *Node) initChainManager() {
	n.chainManager = chains.New(
		n.Config.EnableStaking,
		n.Log,
		n.LogFactory,
		n.vmManager,
		n.DecisionDispatcher,
		n.ConsensusDispatcher,
		n.DB,
		n.Config.ConsensusRouter,
		&networking.VotingNet,
		n.Config.ConsensusParams,
		n.vdrs,
		n.ID,
		n.Config.NetworkID,
		n.ValidatorAPI,
		&n.APIServer,
		&n.keystoreServer,
	)

	n.chainManager.AddRegistrant(&n.APIServer)
}

// initWallet initializes the Wallet service
// Assumes n.APIServer is already set
func (n *Node) initKeystoreAPI() {
	n.Log.Info("initializing Keystore API")
	keystoreDB := prefixdb.New([]byte("keystore"), n.DB)
	n.keystoreServer.Initialize(n.Log, keystoreDB)
	keystoreHandler := n.keystoreServer.CreateHandler()
	if n.Config.KeystoreAPIEnabled {
		n.APIServer.AddRoute(keystoreHandler, &sync.RWMutex{}, "keystore", "", n.HTTPLog)
	}
}

// initMetricsAPI initializes the Metrics API
// Assumes n.APIServer is already set
func (n *Node) initMetricsAPI() {
	n.Log.Info("initializing Metrics API")
	registry, handler := metrics.NewService()
	if n.Config.MetricsAPIEnabled {
		n.APIServer.AddRoute(handler, &sync.RWMutex{}, "metrics", "", n.HTTPLog)
	}
	n.Config.ConsensusParams.Metrics = registry
}

// initAdminAPI initializes the Admin API service
// Assumes n.log, n.chainManager, and n.ValidatorAPI already initialized
func (n *Node) initAdminAPI() {
	if n.Config.AdminAPIEnabled {
		n.Log.Info("initializing Admin API")
		service := admin.NewService(n.ID, n.Config.NetworkID, n.Log, n.chainManager, n.ValidatorAPI.Connections(), &n.APIServer)
		n.APIServer.AddRoute(service, &sync.RWMutex{}, "admin", "", n.HTTPLog)
	}
}

// initIPCAPI initializes the IPC API service
// Assumes n.log and n.chainManager already initialized
func (n *Node) initIPCAPI() {
	if n.Config.IPCEnabled {
		n.Log.Info("initializing IPC API")
		service := ipcs.NewService(n.Log, n.chainManager, n.DecisionDispatcher, &n.APIServer)
		n.APIServer.AddRoute(service, &sync.RWMutex{}, "ipcs", "", n.HTTPLog)
	}
}

// Give chains and VMs aliases as specified by the genesis information
func (n *Node) initAliases() {
	n.Log.Info("initializing aliases")
	defaultAliases, chainAliases, vmAliases := genesis.Aliases(n.Config.NetworkID)
	for chainIDKey, aliases := range chainAliases {
		chainID := ids.NewID(chainIDKey)
		for _, alias := range aliases {
			n.Log.AssertNoError(n.chainManager.Alias(chainID, alias))
		}
	}
	for vmIDKey, aliases := range vmAliases {
		vmID := ids.NewID(vmIDKey)
		for _, alias := range aliases {
			n.Log.AssertNoError(n.vmManager.Alias(vmID, alias))
		}
	}
	for url, aliases := range defaultAliases {
		n.APIServer.AddAliases(url, aliases...)
	}
}

// Initialize this node
func (n *Node) Initialize(Config *Config, logger logging.Logger, logFactory logging.Factory) error {
	n.Log = logger
	n.LogFactory = logFactory
	n.Config = Config

	httpLog, err := logFactory.MakeSubdir("http")
	if err != nil {
		return fmt.Errorf("problem initializing HTTP logger: %w", err)
	}
	n.HTTPLog = httpLog

	n.initDatabase() // Set up the node's database

	if err = n.initNodeID(); err != nil { // Derive this node's ID
		return fmt.Errorf("problem initializing staker ID: %w", err)
	}

	// Start HTTP APIs
	n.initAPIServer()   // Start the API Server
	n.initKeystoreAPI() // Start the Keystore API
	n.initMetricsAPI()  // Start the Metrics API

	// Start node-to-node consensus server
	if err = n.initNetlib(); err != nil { // Set up all networking
		return fmt.Errorf("problem initializing networking: %w", err)
	}
	n.initValidatorNet()    // Set up the validator handshake + authentication
	n.initVMManager()       // Set up the vm manager
	n.initEventDispatcher() // Set up the event dipatcher
	n.initChainManager()    // Set up the chain manager
	n.initConsensusNet()    // Set up the main consensus network

	// TODO: Remove once API is fully featured for throughput tests
	if n.Config.ThroughputServerEnabled {
		n.initClients() // Set up the client servers
	}

	n.initAdminAPI() // Start the Admin API
	n.initIPCAPI()   // Start the IPC API
	n.initAliases()  // Set up aliases
	n.initChains()   // Start the Platform chain

	return nil
}

// Shutdown this node
func (n *Node) Shutdown() {
	n.Log.Info("shutting down the node")
	n.ValidatorAPI.Shutdown()
	n.ConsensusAPI.Shutdown()
	n.chainManager.Shutdown()
}
