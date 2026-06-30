// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var (
	errInvalidUptimeRequirement    = errors.New("uptime requirement must be in the range [0, 1]")
	errMinValidatorStakeAboveMax   = errors.New("minimum validator stake can't be greater than maximum validator stake")
	errInvalidDelegationFee        = errors.New("delegation fee must be in the range [0, 1,000,000]")
	errInvalidMinStakeDuration     = errors.New("min stake duration must be > 0")
	errMinStakeDurationAboveMax    = errors.New("max stake duration can't be less than min stake duration")
	errStakeMaxConsumptionTooLarge = fmt.Errorf("max stake consumption must be less than or equal to %d", reward.PercentDenominator)
	errStakeMaxConsumptionBelowMin = errors.New("stake max consumption can't be less than min stake consumption")
	errStakeMintingPeriodBelowMin  = errors.New("stake minting period can't be less than max stake duration")
)

type APIIndexerConfig struct {
	IndexAPIEnabled      bool `json:"indexAPIEnabled"`
	IndexAllowIncomplete bool `json:"indexAllowIncomplete"`
}

type HTTPConfig struct {
	server.HTTPConfig
	APIConfig `json:"apiConfig"`
	HTTPHost  string `json:"httpHost"`
	HTTPPort  uint16 `json:"httpPort"`

	HTTPSEnabled bool   `json:"httpsEnabled"`
	HTTPSKey     []byte `json:"-"`
	HTTPSCert    []byte `json:"-"`

	HTTPAllowedOrigins []string `json:"httpAllowedOrigins"`
	HTTPAllowedHosts   []string `json:"httpAllowedHosts"`

	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	ShutdownWait    time.Duration `json:"shutdownWait"`
}

type APIConfig struct {
	APIIndexerConfig `json:"indexerConfig"`

	// Enable/Disable APIs
	AdminAPIEnabled   bool `json:"adminAPIEnabled"`
	InfoAPIEnabled    bool `json:"infoAPIEnabled"`
	MetricsAPIEnabled bool `json:"metricsAPIEnabled"`
	HealthAPIEnabled  bool `json:"healthAPIEnabled"`
}

type IPConfig struct {
	PublicIP                  string        `json:"publicIP"`
	PublicIPResolutionService string        `json:"publicIPResolutionService"`
	PublicIPResolutionFreq    time.Duration `json:"publicIPResolutionFreq"`
	// The host portion of the address to listen on. The port to
	// listen on will be sourced from IPPort.
	//
	// - If empty, listen on all interfaces (both ipv4 and ipv6).
	// - If populated, listen only on the specified address.
	ListenHost string `json:"listenHost"`
	ListenPort uint16 `json:"listenPort"`
}

type StakingConfig struct {
	genesis.StakingConfig
	SybilProtectionEnabled        bool            `json:"sybilProtectionEnabled"`
	PartialSyncPrimaryNetwork     bool            `json:"partialSyncPrimaryNetwork"`
	StakingTLSCert                tls.Certificate `json:"-"`
	SybilProtectionDisabledWeight uint64          `json:"sybilProtectionDisabledWeight"`
	StakingTLSKeyPath             string          `json:"stakingTLSKeyPath"`
	StakingTLSCertPath            string          `json:"stakingTLSCertPath"`
	StakingSignerConfig           `json:"stakingSingerConfig"`
}

func (c *StakingConfig) Verify() error {
	switch {
	case c.UptimeRequirement < 0 || c.UptimeRequirement > 1:
		return errInvalidUptimeRequirement
	case c.MinValidatorStake > c.MaxValidatorStake:
		return errMinValidatorStakeAboveMax
	case c.MinDelegationFee > 1_000_000:
		return errInvalidDelegationFee
	case c.MinStakeDuration <= 0:
		return errInvalidMinStakeDuration
	case c.MaxStakeDuration < c.MinStakeDuration:
		return errMinStakeDurationAboveMax
	case c.RewardConfig.MaxConsumptionRate > reward.PercentDenominator:
		return errStakeMaxConsumptionTooLarge
	case c.RewardConfig.MaxConsumptionRate < c.RewardConfig.MinConsumptionRate:
		return errStakeMaxConsumptionBelowMin
	case c.RewardConfig.MintingPeriod < c.MaxStakeDuration:
		return errStakeMintingPeriodBelowMin
	}
	return nil
}

type StakingSignerConfig struct {
	EphemeralSignerEnabled bool   `json:"ephemeralSignerEnabled"`
	KeyContent             string `json:"signerKeyContent"`
	KeyPath                string `json:"keyPath"`
	RPCEndpoint            string `json:"RPCEndpoint"`
	KeyPathIsSet           bool   `json:"keyPathIsSet"`
}

type StateSyncConfig struct {
	StateSyncIDs []ids.NodeID     `json:"stateSyncIDs"`
	StateSyncIPs []netip.AddrPort `json:"stateSyncIPs"`
}

type BootstrapConfig struct {
	// Timeout before emitting a warn log when connecting to bootstrapping beacons
	BootstrapBeaconConnectionTimeout time.Duration `json:"bootstrapBeaconConnectionTimeout"`

	// Max number of containers in an ancestors message sent by this node.
	BootstrapAncestorsMaxContainersSent int `json:"bootstrapAncestorsMaxContainersSent"`

	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	BootstrapAncestorsMaxContainersReceived int `json:"bootstrapAncestorsMaxContainersReceived"`

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration `json:"bootstrapMaxTimeGetAncestors"`

	Bootstrappers []genesis.Bootstrapper `json:"bootstrappers"`
}

type DatabaseConfig struct {
	// If true, all writes are to memory and are discarded at node shutdown.
	ReadOnly bool `json:"readOnly"`

	// Path to database
	Path string `json:"path"`

	// Name of the database type to use
	Name string `json:"name"`

	// Path to config file
	Config []byte `json:"-"`
}

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	HTTPConfig          `json:"httpConfig"`
	IPConfig            `json:"ipConfig"`
	StakingConfig       `json:"stakingConfig"`
	genesis.TxFeeConfig `json:"txFeeConfig"`
	StateSyncConfig     `json:"stateSyncConfig"`
	BootstrapConfig     `json:"bootstrapConfig"`
	DatabaseConfig      `json:"databaseConfig"`

	UpgradeConfig upgrade.Config `json:"upgradeConfig"`

	// Genesis information
	GenesisBytes []byte `json:"-"`
	AvaxAssetID  ids.ID `json:"avaxAssetID"`

	// ID of the network this node should connect to
	NetworkID uint32 `json:"networkID"`

	// Health
	HealthCheckFreq time.Duration `json:"healthCheckFreq"`

	// Network configuration
	NetworkConfig network.Config `json:"networkConfig"`

	AdaptiveTimeoutConfig timer.AdaptiveTimeoutConfig `json:"adaptiveTimeoutConfig"`

	BenchlistConfig benchlist.Config `json:"benchlistConfig"`

	ProfilerConfig profiler.Config `json:"profilerConfig"`

	LoggingConfig logging.Config `json:"loggingConfig"`

	PluginDir string `json:"pluginDir"`

	// File Descriptor Limit
	FdLimit uint64 `json:"fdLimit"`

	// Metrics
	MeterVMEnabled bool `json:"meterVMEnabled"`

	RouterHealthConfig       router.HealthConfig `json:"routerHealthConfig"`
	ConsensusShutdownTimeout time.Duration       `json:"consensusShutdownTimeout"`
	// Poll for new frontiers every [FrontierPollFrequency]
	FrontierPollFrequency time.Duration `json:"consensusGossipFreq"`
	// ConsensusAppConcurrency defines the maximum number of goroutines to
	// handle App messages per chain.
	ConsensusAppConcurrency int `json:"consensusAppConcurrency"`

	TrackedSubnets set.Set[ids.ID] `json:"trackedSubnets"`

	// ProposerMinBlockDelay is the minimum delay this node will enforce when
	// building a snowman++ block on the P-chain and the X-chain. All other
	// chains are expected to perform their own block production throttling.
	//
	// TODO: Remove this flag once the P-chain and X-chain throttle their own
	// block production.
	ProposerMinBlockDelay time.Duration             `json:"proposerMinBlockDelay"`
	SubnetConfigs         map[ids.ID]subnets.Config `json:"subnetConfigs"`

	ChainConfigs map[string]chains.ChainConfig `json:"-"`
	ChainAliases map[ids.ID][]string           `json:"chainAliases"`

	VMAliases map[ids.ID][]string `json:"vmAliases"`

	// Halflife to use for the processing requests tracker.
	// Larger halflife --> usage metrics change more slowly.
	SystemTrackerProcessingHalflife time.Duration `json:"systemTrackerProcessingHalflife"`

	// Frequency to check the real resource usage of tracked processes.
	// More frequent checks --> usage metrics are more accurate, but more
	// expensive to track
	SystemTrackerFrequency time.Duration `json:"systemTrackerFrequency"`

	// Halflife to use for the cpu tracker.
	// Larger halflife --> cpu usage metrics change more slowly.
	SystemTrackerCPUHalflife time.Duration `json:"systemTrackerCPUHalflife"`

	// Halflife to use for the disk tracker.
	// Larger halflife --> disk usage metrics change more slowly.
	SystemTrackerDiskHalflife time.Duration `json:"systemTrackerDiskHalflife"`

	CPUTargeterConfig tracker.TargeterConfig `json:"cpuTargeterConfig"`

	DiskTargeterConfig tracker.TargeterConfig `json:"diskTargeterConfig"`

	RequiredAvailableDiskSpacePercentage uint64 `json:"requiredAvailableDiskSpacePercentage"`
	WarningAvailableDiskSpacePercentage  uint64 `json:"warningAvailableDiskSpacePercentage"`

	TraceConfig trace.Config `json:"traceConfig"`

	// See comment on [UseCurrentHeight] in platformvm.Config
	UseCurrentHeight bool `json:"useCurrentHeight"`

	// ProvidedFlags contains all the flags set by the user
	ProvidedFlags map[string]interface{} `json:"-"`

	// ChainDataDir is the root path for per-chain directories where VMs can
	// write arbitrary data.
	ChainDataDir string `json:"chainDataDir"`

	// Path to write process context to (including PID, API URI, and
	// staking address).
	ProcessContextFilePath string `json:"processContextFilePath"`
}
