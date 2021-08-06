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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
)

type IPCConfig struct {
	IPCAPIEnabled      bool     `json:"ipcAPIEnabled"`
	IPCPath            string   `json:"ipcPath"`
	IPCDefaultChainIDs []string `json:"ipcDefaultChainIDs"`
}

type APIAuthConfig struct {
	APIRequireAuthToken bool   `json:"apiRequireAuthToken"`
	APIAuthPassword     string `json:"-"`
}

type APIIndexerConfig struct {
	IndexAPIEnabled      bool `json:"indexAPIEnabled"`
	IndexAllowIncomplete bool `json:"indexAllowIncomplete"`
}

type APIConfig struct {
	APIAuthConfig    `json:"apiAuthConfig"`
	APIIndexerConfig `json:"apiIndexerConfig"`

	HTTPHost string `json:"httpHost"`
	HTTPPort uint16 `json:"httpPort"`

	HTTPSEnabled  bool   `json:"httpsEnabled"`
	HTTPSKeyFile  string `json:"httpsKeyFile"`
	HTTPSCertFile string `json:"httpsCertFileAuthConfig"`

	APIAllowedOrigins []string `json:"apiAllowedOrigins"`

	// Enable/Disable APIs
	AdminAPIEnabled    bool `json:"adminAPIEnabled"`
	InfoAPIEnabled     bool `json:"infoAPIEnabled"`
	KeystoreAPIEnabled bool `json:"keystoreAPIEnabled"`
	MetricsAPIEnabled  bool `json:"metricsAPIEnabled"`
	HealthAPIEnabled   bool `json:"healthAPIEnabled"`
}

type PeerListGossipConfig struct {
	PeerListSize       uint32        `json:"peerListSize"`
	PeerListGossipSize uint32        `json:"peerListGossipSize"`
	PeerListGossipFreq time.Duration `json:"peerListGossipFreq"`
}

type ConsensusGossipConfig struct {
	// Gossip a container in the accepted frontier every [ConsensusGossipFrequency]
	ConsensusGossipFrequency time.Duration `json:"consensusGossipFreq"`
	// Number of peers to gossip to when gossiping accepted frontier
	ConsensusGossipAcceptedFrontierSize uint `json:"consensusGossipAcceptedFrontierSize"`
	// Number of peers to gossip each accepted container to
	ConsensusGossipOnAcceptSize uint `json:"consensusGossipOnAcceptSize"`
}

type GossipConfig struct {
	PeerListGossipConfig
	ConsensusGossipConfig
}

// TODO do we need all these fields?
type IPConfig struct {
	IP utils.DynamicIPDesc `json:"ip"`
	// True if we attempted NAT Traversal
	AttemptedNATTraversal bool `json:"attemptedNATTraversal"`
	// Tries to perform network address translation
	Nat nat.Router `json:"-"`
	// Dynamic Update duration for IP or NAT traversal
	DynamicUpdateDuration time.Duration `json:"dynamicUpdateDuration"`
	// Tries to resolve our IP from an external source
	DynamicPublicIPResolver dynamicip.Resolver `json:"-"`
}

type StakingConfig struct {
	genesis.StakingConfig
	EnableStaking         bool            `json:"enableStaking"`
	StakingTLSCert        tls.Certificate `json:"-"`
	DisabledStakingWeight uint64          `json:"disabledStakingWeight"`
}

type BootstrapConfig struct {
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

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration `json:"bootstrapMaxTimeGetAncestors"`

	BootstrapIDs []ids.ShortID  `json:"bootstrapIDs"`
	BootstrapIPs []utils.IPDesc `json:"bootstrapIPs"`
}

type DatabaseConfig struct {
	// Path to database
	Path string `json:"path"`

	// Name of the database type to use
	Name string `json:"name"`
}

type VMAliases map[ids.ID][]string

func (v *VMAliases) MarshalJSON() ([]byte, error) {
	// Sort so we have deterministic ordering
	vmIDs := make([]ids.ID, len(*v))
	i := 0
	for vmID := range *v {
		vmIDs[i] = vmID
		i++
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
				b.WriteString(",")
			}
		}
		b.WriteString("]")
		if i != len(vmIDs)-1 {
			b.WriteString(",")
		}
	}
	b.WriteString("}")
	return []byte(b.String()), nil
}

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	APIConfig           `json:"apiConfig"`
	IPCConfig           `json:"ipcConfig"`
	GossipConfig        `json:"gossipConfig"`
	IPConfig            `json:"ipConfig"`
	StakingConfig       `json:"stakingConfig"`
	genesis.TxFeeConfig `json:"txFeeConfig"`
	genesis.EpochConfig `json:"epochConfig"`
	BootstrapConfig     `json:"bootstrapConfig"`
	DatabaseConfig      `json:"databaseConfig"`

	// Genesis information
	GenesisBytes []byte `json:"-"`
	AvaxAssetID  ids.ID `json:"avaxAssetID"`

	// ID of the network this node should connect to
	NetworkID uint32 `json:"networkID"`

	// Assertions configuration
	EnableAssertions bool `json:"enableAssertions"`

	// Crypto configuration
	EnableCrypto bool `json:"enableCrypto"`

	// Health
	HealthCheckFreq time.Duration `json:"healthCheckFreq"`

	// Network configuration
	NetworkConfig network.Config `json:"networkConfig"`

	// Benchlist Configuration
	BenchlistConfig benchlist.Config `json:"benchlistConfig"`

	// Profiling configurations
	ProfilerConfig profiler.Config `json:"profilerConfig"`

	// Logging configuration
	LoggingConfig logging.Config `json:"loggingConfig"`

	// Plugin directory
	PluginDir string `json:"pluginDir"`

	// Consensus configuration
	ConsensusParams avalanche.Parameters `json:"consensusParams"`

	// Metrics
	MeterVMEnabled bool `json:"meterVMEnabled"`

	// Router that is used to handle incoming consensus messages
	ConsensusRouter          router.Router       `json:"-"`
	RouterHealthConfig       router.HealthConfig `json:"routerHealthConfig"`
	ConsensusShutdownTimeout time.Duration       `json:"consensusShutdownTimeout"`

	// Subnet Whitelist
	WhitelistedSubnets ids.Set `json:"whitelistedSubnets"`

	// ChainConfigs
	ChainConfigs map[string]chains.ChainConfig `json:"-"`

	// VM Aliases
	VMAliases VMAliases `json:"vmAliases"`
}
