// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto/tls"
	"fmt"
	"strings"
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
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
)

type VMAliases map[ids.ID][]string

func (v *VMAliases) MarshalJSON() ([]byte, error) {
	// Sort so we have deterministic ordering
	vmIDs := []ids.ID{}
	for vmID := range *v {
		vmIDs = append(vmIDs, vmID)
	}
	ids.SortIDs(vmIDs)

	b := strings.Builder{}
	b.WriteString("{")
	for i, vmID := range vmIDs {
		b.WriteString(fmt.Sprintf("\"%s\": [", vmID))
		aliases := (*v)[vmID]
		for i, alias := range aliases {
			b.WriteString(fmt.Sprintf("\"%s\"", alias))
			if i != len(aliases)-1 {
				b.WriteString(", ")
			}
		}
		b.WriteString("]")
		if i != len(vmIDs)-1 {
			b.WriteString(", ")
		}
	}
	b.WriteString("}")
	return []byte(b.String()), nil
}

// Alias []byte so we can specify a MarshalJSON, which will be called
// when MarshalJSON is called on Config. (The JSON representation of the
// node's Config can be retrieved via the Admin API.)
type GenesisBytes []byte

func (g *GenesisBytes) MarshalJSON() ([]byte, error) {
	encoded, err := formatting.EncodeWithChecksum(formatting.Hex, *g)
	if err != nil {
		return nil, fmt.Errorf("couldn't encode genesis bytes to hex+checksum: %w", err)
	}
	return []byte(fmt.Sprintf("\"%s\"", encoded)), nil
}

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	genesis.Params

	// Genesis information
	GenesisBytes GenesisBytes `json:"genesisBytes"`
	AvaxAssetID  ids.ID       `json:"avaxAssetID"`

	// protocol to use for opening the network interface
	Nat nat.Router `json:"-"`

	// Attempted NAT Traversal did we attempt
	AttemptedNATTraversal bool `json:"attemptedNATTraversal"`

	// ID of the network this node should connect to
	NetworkID uint32 `json:"networkID"`

	// Assertions configuration
	EnableAssertions bool `json:"enableAssertions"`

	// Crypto configuration
	EnableCrypto bool `json:"enableCrypto"`

	// Path to database
	DBPath string `json:"dbPath"`

	// Name of the database type to use
	DBName string `json:"dbName"`

	// Staking configuration
	StakingIP             utils.DynamicIPDesc `json:"stakingIP"`
	EnableStaking         bool                `json:"enableStaking"`
	StakingTLSCert        tls.Certificate     `json:"-"`
	DisabledStakingWeight uint64              `json:"disabledStakingWeight"`

	// Health
	HealthCheckFreq time.Duration `json:"healthCheckFreq"`

	// Network configuration
	NetworkConfig      network.Config `json:"networkConfig"`
	PeerListSize       uint32         `json:"peerListSize"`
	PeerListGossipSize uint32         `json:"peerListGossipSize"`
	PeerListGossipFreq time.Duration  `json:"peerListGossipFreq"`
	CompressionEnabled bool           `json:"compressionEnabled"`

	// Benchlist Configuration
	BenchlistConfig benchlist.Config `json:"benchlistConfig"`

	// Bootstrapping configuration
	BootstrapIDs []ids.ShortID  `json:"bootstrapIDs"`
	BootstrapIPs []utils.IPDesc `json:"bootstrapIPs"`

	// HTTP configuration
	HTTPHost string `json:"httpHost"`
	HTTPPort uint16 `json:"httpPort"`

	HTTPSEnabled        bool     `json:"httpsEnabled"`
	HTTPSKeyFile        string   `json:"httpsKeyFile"`
	HTTPSCertFile       string   `json:"httpsCertFile"`
	APIRequireAuthToken bool     `json:"apiRequireAuthToken"`
	APIAuthPassword     string   `json:"apiAuthPassword"`
	APIAllowedOrigins   []string `json:"apiAllowedOrigins"`

	// Enable/Disable APIs
	AdminAPIEnabled    bool `json:"adminAPIEnabled"`
	InfoAPIEnabled     bool `json:"infoAPIEnabled"`
	KeystoreAPIEnabled bool `json:"keystoreAPIEnabled"`
	MetricsAPIEnabled  bool `json:"metricsAPIEnabled"`
	HealthAPIEnabled   bool `json:"healthAPIEnabled"`
	IndexAPIEnabled    bool `json:"indexAPIEnabled"`

	// Profiling configurations
	ProfilerConfig profiler.Config `json:"profilerConfig"`

	// Logging configuration
	LoggingConfig logging.Config `json:"loggingConfig"`

	// Plugin directory
	PluginDir string `json:"pluginDir"`

	// Consensus configuration
	ConsensusParams avalanche.Parameters `json:"consensusParams"`

	// IPC configuration
	IPCAPIEnabled      bool     `json:"ipcAPIEnabled"`
	IPCPath            string   `json:"ipcPath"`
	IPCDefaultChainIDs []string `json:"ipcDefaultChainIDs"`

	// Metrics
	MeterVMEnabled bool `json:"meterVMEnabled"`

	// Router that is used to handle incoming consensus messages
	ConsensusRouter          router.Router       `json:"-"`
	RouterHealthConfig       router.HealthConfig `json:"routerHealthConfig"`
	ConsensusShutdownTimeout time.Duration       `json:"consensusShutdownTimeout"`
	ConsensusGossipFrequency time.Duration       `json:"consensusGossipFrequency"`
	// Number of peers to gossip to when gossiping accepted frontier
	ConsensusGossipAcceptedFrontierSize uint `json:"consensusGossipAcceptedFrontierSize"`
	// Number of peers to gossip each accepted container to
	ConsensusGossipOnAcceptSize uint `json:"consensusGossipOnAcceptSize"`

	// Dynamic Update duration for IP or NAT traversal
	DynamicUpdateDuration time.Duration `json:"dynamicUpdateDuration"`

	DynamicPublicIPResolver dynamicip.Resolver `json:"-"`

	// Subnet Whitelist
	WhitelistedSubnets ids.Set `json:"whitelistedSubnets"`

	IndexAllowIncomplete bool `json:"indexAllowIncomplete"`

	// Should Bootstrap be retried
	RetryBootstrap bool `json:"retryBootstrap"`

	// Max number of times to retry bootstrap
	RetryBootstrapMaxAttempts int `json:"retryBootstrapMaxAttempts"`

	// Timeout when connecting to bootstrapping beacons
	BootstrapBeaconConnectionTimeout time.Duration `json:"bootstrapBeaconConnectionTimeout"`

	// Max number of containers in a multiput message sent by this node.
	BootstrapMultiputMaxContainersSent int `json:"bootstrapMultiputMaxContainersSent"`

	// This node will only consider the first [MultiputMaxContainersReceived]
	// containers in a multiput it receives.
	BootstrapMultiputMaxContainersReceived int `json:"bootstrapMultiputMaxContainersReceived"`

	// Peer alias configuration
	PeerAliasTimeout time.Duration `json:"peerAliasTimeout"`

	// ChainConfigs
	ChainConfigs map[string]chains.ChainConfig `json:"chainConfigs"`

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration `json:"bootstrapMaxTimeGetAncestors"`

	// VM Aliases
	VMAliases VMAliases `json:"vmAliases"`
}
