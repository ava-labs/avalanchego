// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/nat"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/logging"
)

// Config contains all of the configurations of an Ava node.
type Config struct {
	// protocol to use for opening the network interface
	Nat nat.Router

	// ID of the network this node should connect to
	NetworkID uint32

	// Transaction fee configuration
	AvaTxFee uint64

	// Assertions configuration
	EnableAssertions bool

	// Crypto configuration
	EnableCrypto bool

	// Database to use for the node
	DB database.Database

	// Staking configuration
	StakingIP       utils.IPDesc
	EnableStaking   bool
	StakingKeyFile  string
	StakingCertFile string

	// Bootstrapping configuration
	BootstrapPeers []*Peer

	// HTTP configuration
	HTTPHost      string
	HTTPPort      uint16
	EnableHTTPS   bool
	HTTPSKeyFile  string
	HTTPSCertFile string

	// Enable/Disable APIs
	AdminAPIEnabled    bool
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

	// IPCEnabled configuration
	IPCEnabled bool

	// Router that is used to handle incoming consensus messages
	ConsensusRouter router.Router
}
