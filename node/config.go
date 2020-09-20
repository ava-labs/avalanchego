// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	// protocol to use for opening the network interface
	Nat nat.Router

	// ID of the network this node should connect to
	NetworkID uint32

	// Transaction fee for transactions that create new state
	CreationTxFee uint64
	// Transaction fee
	TxFee uint64

	// Staking uptime requirements
	UptimeRequirement float64

	// Minimum stake, in nAVAX, required to validate the primary network
	MinValidatorStake uint64

	// Maximum stake, in nAVAX, allowed to be placed on a single validator in
	// the primary network
	MaxValidatorStake uint64

	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake uint64

	// Minimum delegation fee, in the range [0, 1000000], that can be charged
	// for delegation on the primary network.
	MinDelegationFee uint32

	// MinStakeDuration is the minimum amount of time a validator can validate
	// for in a single period.
	MinStakeDuration time.Duration

	// MaxStakeDuration is the maximum amount of time a validator can validate
	// for in a single period.
	MaxStakeDuration time.Duration

	// StakeMintingPeriod is the amount of time for a consumption period.
	StakeMintingPeriod time.Duration

	// Assertions configuration
	EnableAssertions bool

	// Crypto configuration
	EnableCrypto bool

	// Database to use for the node
	DB database.Database

	// Staking configuration
	StakingIP               utils.IPDesc
	StakingLocalPort        uint16
	EnableP2PTLS            bool
	EnableStaking           bool
	StakingKeyFile          string
	StakingCertFile         string
	DisabledStakingWeight   uint64
	MaxNonStakerPendingMsgs uint
	StakerMSGPortion        float64
	StakerCPUPortion        float64

	// Network configuration
	NetworkConfig timer.AdaptiveTimeoutConfig

	// Bootstrapping configuration
	BootstrapPeers []*Peer

	// HTTP configuration
	HTTPHost            string
	HTTPPort            uint16
	HTTPSEnabled        bool
	HTTPSKeyFile        string
	HTTPSCertFile       string
	APIRequireAuthToken bool
	APIAuthPassword     string

	// Enable/Disable APIs
	AdminAPIEnabled    bool
	InfoAPIEnabled     bool
	KeystoreAPIEnabled bool
	MetricsAPIEnabled  bool
	HealthAPIEnabled   bool

	// Logging configuration
	LoggingConfig logging.Config

	// Plugin directory
	PluginDir string

	// Consensus configuration
	ConsensusParams avalanche.Parameters

	// Throughput configuration
	ThroughputPort          uint16
	ThroughputServerEnabled bool

	// IPC configuration
	IPCAPIEnabled      bool
	IPCPath            string
	IPCDefaultChainIDs []string

	// Router that is used to handle incoming consensus messages
	ConsensusRouter          router.Router
	ConsensusGossipFrequency time.Duration
	ConsensusShutdownTimeout time.Duration
}
