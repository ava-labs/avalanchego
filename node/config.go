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

type IPCConfig struct {
	IPCAPIEnabled      bool
	IPCPath            string
	IPCDefaultChainIDs []string
}

type APIAuthConfig struct {
	APIRequireAuthToken bool
	APIAuthPassword     string
}

type APIIndexerConfig struct {
	IndexAPIEnabled      bool
	IndexAllowIncomplete bool
}

type APIConfig struct {
	APIAuthConfig
	APIIndexerConfig

	HTTPHost string
	HTTPPort uint16

	HTTPSEnabled  bool
	HTTPSKeyFile  string
	HTTPSCertFile string

	APIAllowedOrigins []string

	// Enable/Disable APIs
	AdminAPIEnabled    bool
	InfoAPIEnabled     bool
	KeystoreAPIEnabled bool
	MetricsAPIEnabled  bool
	HealthAPIEnabled   bool
}

type PeerListGossipConfig struct {
	PeerListSize       uint32
	PeerListGossipSize uint32
	PeerListGossipFreq time.Duration
}

type ConsensusGossipConfig struct {
	// Gossip a container in the accepted frontier every [ConsensusGossipFrequency]
	ConsensusGossipFrequency time.Duration
	// Number of peers to gossip to when gossiping accepted frontier
	ConsensusGossipAcceptedFrontierSize uint
	// Number of peers to gossip each accepted container to
	ConsensusGossipOnAcceptSize uint
}

type GossipConfig struct {
	PeerListGossipConfig
	ConsensusGossipConfig
}

// TODO do we need all these fields?
type IPConfig struct {
	IP utils.DynamicIPDesc
	// True if we attempted NAT Traversal
	AttemptedNATTraversal bool
	// Tries to perform network address translation
	Nat nat.Router
	// Dynamic Update duration for IP or NAT traversal
	DynamicUpdateDuration time.Duration
	// Tries to resolve our IP from an external source
	DynamicPublicIPResolver dynamicip.Resolver
}

type StakingConfig struct {
	genesis.StakingConfig
	EnableStaking         bool
	StakingTLSCert        tls.Certificate
	DisabledStakingWeight uint64
}

type BootstrapConfig struct {
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

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration

	BootstrapIDs []ids.ShortID
	BootstrapIPs []utils.IPDesc
}

type DatabaseConfig struct {
	// Path to database
	Path string

	// Name of the database type to use
	Name string
}

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	APIConfig
	IPCConfig
	GossipConfig
	IPConfig
	StakingConfig
	genesis.TxFeeConfig
	genesis.EpochConfig
	BootstrapConfig
	DatabaseConfig

	// Genesis information
	GenesisBytes []byte
	AvaxAssetID  ids.ID

	// ID of the network this node should connect to
	NetworkID uint32

	// Assertions configuration
	EnableAssertions bool

	// Crypto configuration
	EnableCrypto bool

	// Health
	HealthCheckFreq time.Duration

	// Network configuration
	NetworkConfig network.Config

	// Benchlist Configuration
	BenchlistConfig benchlist.Config

	// Profiling configurations
	ProfilerConfig profiler.Config

	// Logging configuration
	LoggingConfig logging.Config

	// Plugin directory
	PluginDir string

	// Consensus configuration
	ConsensusParams avalanche.Parameters

	// Metrics
	MeterVMEnabled bool

	// Router that is used to handle incoming consensus messages
	ConsensusRouter          router.Router
	RouterHealthConfig       router.HealthConfig
	ConsensusShutdownTimeout time.Duration

	// Subnet Whitelist
	WhitelistedSubnets ids.Set

	// ChainConfigs
	ChainConfigs map[string]chains.ChainConfig

	// VM Aliases
	VMAliases map[ids.ID][]string
}
