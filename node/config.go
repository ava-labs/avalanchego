// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto/tls"
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
)

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	genesis.Params

	// If true, bootstrap the current database version and then end the node.
	FetchOnly bool

	// Genesis information
	GenesisBytes []byte
	AvaxAssetID  ids.ID

	// protocol to use for opening the network interface
	Nat nat.Router

	// Attempted NAT Traversal did we attempt
	AttemptedNATTraversal bool

	// ID of the network this node should connect to
	NetworkID uint32

	// Assertions configuration
	EnableAssertions bool

	// Crypto configuration
	EnableCrypto bool

	// Path to database
	DBPath string

	// Name of the database type to use
	DBName string

	// Staking configuration
	StakingIP             utils.DynamicIPDesc
	EnableStaking         bool
	StakingTLSCert        tls.Certificate
	DisabledStakingWeight uint64

	// Throttling
	SendQueueSize uint32

	// Health
	HealthCheckFreq time.Duration

	// Network configuration
	NetworkConfig      network.Config
	PeerListSize       uint32
	PeerListGossipSize uint32
	PeerListGossipFreq time.Duration
	CompressionEnabled bool

	// Benchlist Configuration
	BenchlistConfig benchlist.Config

	// Bootstrapping configuration
	BootstrapIDs []ids.ShortID
	BootstrapIPs []utils.IPDesc

	// HTTP configuration
	HTTPHost string
	HTTPPort uint16

	HTTPSEnabled        bool
	HTTPSKeyFile        string
	HTTPSCertFile       string
	APIRequireAuthToken bool
	APIAuthPassword     string
	APIAllowedOrigins   []string

	// Enable/Disable APIs
	AdminAPIEnabled    bool
	InfoAPIEnabled     bool
	KeystoreAPIEnabled bool
	MetricsAPIEnabled  bool
	HealthAPIEnabled   bool
	IndexAPIEnabled    bool

	// Profiling configurations
	ProfilerConfig profiler.Config

	// Logging configuration
	LoggingConfig logging.Config

	// Plugin directory
	PluginDir string

	// Consensus configuration
	ConsensusParams avalanche.Parameters

	// IPC configuration
	IPCAPIEnabled      bool
	IPCPath            string
	IPCDefaultChainIDs []string

	// Metrics
	MeterVMEnabled bool

	// Router that is used to handle incoming consensus messages
	ConsensusRouter          router.Router
	RouterHealthConfig       router.HealthConfig
	ConsensusShutdownTimeout time.Duration
	ConsensusGossipFrequency time.Duration
	// Number of peers to gossip to when gossiping accepted frontier
	ConsensusGossipAcceptedFrontierSize uint
	// Number of peers to gossip each accepted container to
	ConsensusGossipOnAcceptSize uint

	// Dynamic Update duration for IP or NAT traversal
	DynamicUpdateDuration time.Duration

	DynamicPublicIPResolver dynamicip.Resolver

	// Throttling incoming connections
	ConnMeterResetDuration time.Duration
	ConnMeterMaxConns      int

	// Subnet Whitelist
	WhitelistedSubnets ids.Set

	IndexAllowIncomplete bool

	// Should Bootstrap be retried
	RetryBootstrap bool

	// Max number of times to retry bootstrap
	RetryBootstrapMaxAttempts int

	// Timeout when connecting to bootstrapping beacons
	BootstrapBeaconConnectionTimeout time.Duration

	// Max number of containers in a multiput message sent by this node.
	BootstrapMultiputMaxContainersSent int

	// This node will only consider the first [MultiputMaxContainersReceived]
	// containers in a multiput it receives.
	BootstrapMultiputMaxContainersReceived int

	// Peer alias configuration
	PeerAliasTimeout time.Duration

	// ChainConfigs
	ChainConfigs map[string]chains.ChainConfig

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration

	// VM Aliases
	VMAliases map[ids.ID][]string
}
