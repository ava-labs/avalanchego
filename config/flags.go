// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/osext"

	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/rocksdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	DefaultHTTPPort    = 9650
	DefaultStakingPort = 9651

	AvalancheGoDataDirVar    = "AVALANCHEGO_DATA_DIR"
	defaultUnexpandedDataDir = "$" + AvalancheGoDataDirVar
)

var (
	// [defaultUnexpandedDataDir] will be expanded when reading the flags
	defaultDataDir         = filepath.Join("$HOME", ".avalanchego")
	defaultDBDir           = filepath.Join(defaultUnexpandedDataDir, "db")
	defaultLogDir          = filepath.Join(defaultUnexpandedDataDir, "logs")
	defaultProfileDir      = filepath.Join(defaultUnexpandedDataDir, "profiles")
	defaultStakingPath     = filepath.Join(defaultUnexpandedDataDir, "staking")
	defaultStakingKeyPath  = filepath.Join(defaultStakingPath, "staker.key")
	defaultStakingCertPath = filepath.Join(defaultStakingPath, "staker.crt")
	defaultConfigDir       = filepath.Join(defaultUnexpandedDataDir, "configs")
	defaultChainConfigDir  = filepath.Join(defaultConfigDir, "chains")
	defaultVMConfigDir     = filepath.Join(defaultConfigDir, "vms")
	defaultVMAliasFilePath = filepath.Join(defaultVMConfigDir, "aliases.json")
	defaultSubnetConfigDir = filepath.Join(defaultConfigDir, "subnets")

	// Places to look for the build directory
	defaultBuildDirs = []string{}
)

func init() {
	folderPath, err := osext.ExecutableFolder()
	if err == nil {
		defaultBuildDirs = append(defaultBuildDirs, folderPath)
		defaultBuildDirs = append(defaultBuildDirs, filepath.Dir(folderPath))
	}
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	defaultBuildDirs = append(defaultBuildDirs,
		wd,
		filepath.Join("/", "usr", "local", "lib", constants.AppName),
		defaultUnexpandedDataDir,
	)
}

func addProcessFlags(fs *flag.FlagSet) {
	// If true, print the version and quit.
	fs.Bool(VersionKey, false, "If true, print version and quit")

	// Build directory
	fs.String(BuildDirKey, defaultBuildDirs[0], "Path to the build directory")

	// Plugin
	fs.Bool(PluginModeKey, false, "Whether the app should run as a plugin")
}

func addNodeFlags(fs *flag.FlagSet) {
	// Home directory
	fs.String(DataDirKey, defaultDataDir, "Sets the base data directory where default sub-directories will be placed unless otherwise specified.")
	// System
	fs.Uint64(FdLimitKey, ulimit.DefaultFDLimit, "Attempts to raise the process file descriptor limit to at least this value and error if the value is above the system max")

	// Config File
	fs.String(ConfigFileKey, "", fmt.Sprintf("Specifies a config file. Ignored if %s is specified", ConfigContentKey))
	fs.String(ConfigContentKey, "", "Specifies base64 encoded config content")
	fs.String(ConfigContentTypeKey, "json", "Specifies the format of the base64 encoded config content. Available values: 'json', 'yaml', 'toml'")

	// Genesis
	fs.String(GenesisConfigFileKey, "", fmt.Sprintf("Specifies a genesis config file (ignored when running standard networks or if %s is specified)",
		GenesisConfigContentKey))
	fs.String(GenesisConfigContentKey, "", "Specifies base64 encoded genesis content")

	// Network ID
	fs.String(NetworkNameKey, constants.MainnetName, "Network ID this node will connect to")

	// AVAX fees
	fs.Uint64(TxFeeKey, genesis.LocalParams.TxFee, "Transaction fee, in nAVAX")
	fs.Uint64(CreateAssetTxFeeKey, genesis.LocalParams.CreateAssetTxFee, "Transaction fee, in nAVAX, for transactions that create new assets")
	fs.Uint64(CreateSubnetTxFeeKey, genesis.LocalParams.CreateSubnetTxFee, "Transaction fee, in nAVAX, for transactions that create new subnets")
	fs.Uint64(CreateBlockchainTxFeeKey, genesis.LocalParams.CreateBlockchainTxFee, "Transaction fee, in nAVAX, for transactions that create new blockchains")

	// Database
	fs.String(DBTypeKey, leveldb.Name, fmt.Sprintf("Database type to use. Should be one of {%s, %s, %s}", leveldb.Name, rocksdb.Name, memdb.Name))
	fs.String(DBPathKey, defaultDBDir, "Path to database directory")
	fs.String(DBConfigFileKey, "", fmt.Sprintf("Path to database config file. Ignored if %s is specified", DBConfigContentKey))
	fs.String(DBConfigContentKey, "", "Specifies base64 encoded database config content")

	// Logging
	fs.String(LogsDirKey, defaultLogDir, "Logging directory for Avalanche")
	fs.String(LogLevelKey, "info", "The log level. Should be one of {verbo, debug, trace, info, warn, error, fatal, off}")
	fs.String(LogDisplayLevelKey, "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, trace, info, warn, error, fatal, off}")
	fs.String(LogFormatKey, "auto", "The structure of log format. Defaults to 'auto' which formats terminal-like logs, when the output is a terminal. Otherwise, should be one of {auto, plain, colors, json}")
	fs.Uint(LogRotaterMaxSizeKey, 8, "The maximum file size in megabytes of the log file before it gets rotated.")
	fs.Uint(LogRotaterMaxFilesKey, 7, "The maximum number of old log files to retain. 0 means retain all old log files.")
	fs.Uint(LogRotaterMaxAgeKey, 0, "The maximum number of days to retain old log files based on the timestamp encoded in their filename. 0 means retain all old log files.")
	fs.Bool(LogRotaterCompressEnabledKey, false, "Enables the compression of rotated log files through gzip.")
	fs.Bool(LogDisableDisplayPluginLogsKey, false, "Disables displaying plugin logs in stdout.")

	// Assertions
	fs.Bool(AssertionsEnabledKey, true, "Turn on assertion execution")

	// Signature Verification
	fs.Bool(SignatureVerificationEnabledKey, true, "Turn on signature verification")

	// Peer List Gossip
	gossipHelpMsg := fmt.Sprintf(
		"Gossip [%s] validator IPs to [%s] validators, [%s] non-validators, and [%s] validating or non-validating peers every [%s]",
		NetworkPeerListNumValidatorIPsKey,
		NetworkPeerListValidatorGossipSizeKey,
		NetworkPeerListNonValidatorGossipSizeKey,
		NetworkPeerListPeersGossipSizeKey,
		NetworkPeerListGossipFreqKey,
	)
	fs.Uint(NetworkPeerListNumValidatorIPsKey, 15, gossipHelpMsg)
	fs.Uint(NetworkPeerListValidatorGossipSizeKey, 20, gossipHelpMsg)
	fs.Uint(NetworkPeerListNonValidatorGossipSizeKey, 10, gossipHelpMsg)
	fs.Uint(NetworkPeerListPeersGossipSizeKey, 0, gossipHelpMsg)
	fs.Duration(NetworkPeerListGossipFreqKey, time.Minute, gossipHelpMsg)

	// Public IP Resolution
	fs.String(PublicIPKey, "", "Public IP of this node for P2P communication. If empty, try to discover with NAT. Ignored if dynamic-public-ip is non-empty")
	fs.Duration(DynamicUpdateDurationKey, 5*time.Minute, "Dynamic IP and NAT Traversal update duration")                                                        // Deprecated
	fs.String(DynamicPublicIPResolverKey, "", "'ifconfigco' (alias 'ifconfig') or 'opendns' or 'ifconfigme'. By default does not do dynamic public IP updates") // Deprecated
	fs.Duration(PublicIPResolutionFreqKey, 5*time.Minute, "Frequency at which this node resolves/updates its public IP and renew NAT mappings, if applicable")
	fs.String(PublicIPResolutionServiceKey, "", "Only acceptable values are 'ifconfigco', 'opendns' or 'ifconfigme'. When provided, the node will use that service to periodically resolve/update its public IP")

	// Inbound Connection Throttling
	fs.Duration(InboundConnUpgradeThrottlerCooldownKey, 10*time.Second, "Upgrade an inbound connection from a given IP at most once per this duration. If 0, don't rate-limit inbound connection upgrades")
	fs.Float64(InboundThrottlerMaxConnsPerSecKey, 256, "Max number of inbound connections to accept (from all peers) per second")
	// Outbound Connection Throttling
	fs.Uint(OutboundConnectionThrottlingRps, 50, "Make at most this number of outgoing peer connection attempts per second")
	fs.Duration(OutboundConnectionTimeout, 30*time.Second, "Timeout when dialing a peer")
	// Timeouts
	fs.Duration(NetworkInitialTimeoutKey, 5*time.Second, "Initial timeout value of the adaptive timeout manager")
	fs.Duration(NetworkMinimumTimeoutKey, 2*time.Second, "Minimum timeout value of the adaptive timeout manager")
	fs.Duration(NetworkMaximumTimeoutKey, 10*time.Second, "Maximum timeout value of the adaptive timeout manager")
	fs.Duration(NetworkMaximumInboundTimeoutKey, 10*time.Second, "Maximum timeout value of an inbound message. Defines duration within which an incoming message must be fulfilled. Incoming messages containing deadline higher than this value will be overridden with this value.")
	fs.Duration(NetworkTimeoutHalflifeKey, 5*time.Minute, "Halflife of average network response time. Higher value --> network timeout is less volatile. Can't be 0")
	fs.Float64(NetworkTimeoutCoefficientKey, 2, "Multiplied by average network response time to get the network timeout. Must be >= 1")
	fs.Duration(NetworkReadHandshakeTimeoutKey, 15*time.Second, "Timeout value for reading handshake messages")
	fs.Duration(NetworkPingTimeoutKey, constants.DefaultPingPongTimeout, "Timeout value for Ping-Pong with a peer")
	fs.Duration(NetworkPingFrequencyKey, constants.DefaultPingFrequency, "Frequency of pinging other peers")

	fs.Bool(NetworkCompressionEnabledKey, true, "If true, compress certain outbound messages. This node will be able to parse compressed inbound messages regardless of this flag's value")
	fs.Duration(NetworkMaxClockDifferenceKey, time.Minute, "Max allowed clock difference value between this node and peers")
	fs.Bool(NetworkAllowPrivateIPsKey, true, "Allows the node to initiate outbound connection attempts to peers with private IPs")
	fs.Bool(NetworkRequireValidatorToConnectKey, false, "If true, this node will only maintain a connection with another node if this node is a validator, the other node is a validator, or the other node is a beacon")
	fs.Uint(NetworkPeerReadBufferSizeKey, 8*units.KiB, "Size, in bytes, of the buffer that we read peer messages into (there is one buffer per peer)")
	fs.Uint(NetworkPeerWriteBufferSizeKey, 8*units.KiB, "Size, in bytes, of the buffer that we write peer messages into (there is one buffer per peer)")

	// Benchlist
	fs.Int(BenchlistFailThresholdKey, 10, "Number of consecutive failed queries before benchlisting a node")
	fs.Duration(BenchlistDurationKey, 15*time.Minute, "Max amount of time a peer is benchlisted after surpassing the threshold")
	fs.Duration(BenchlistMinFailingDurationKey, 2*time.Minute+30*time.Second, "Minimum amount of time messages to a peer must be failing before the peer is benched")

	// Router
	fs.Duration(ConsensusGossipFrequencyKey, 10*time.Second, "Frequency of gossiping accepted frontiers")
	fs.Duration(ConsensusShutdownTimeoutKey, 30*time.Second, "Timeout before killing an unresponsive chain")
	fs.Uint(ConsensusGossipAcceptedFrontierValidatorSizeKey, 0, "Number of validators to gossip to when gossiping accepted frontier")
	fs.Uint(ConsensusGossipAcceptedFrontierNonValidatorSizeKey, 0, "Number of non-validators to gossip to when gossiping accepted frontier")
	fs.Uint(ConsensusGossipAcceptedFrontierPeerSizeKey, 15, "Number of peers to gossip to when gossiping accepted frontier")
	fs.Uint(ConsensusGossipOnAcceptValidatorSizeKey, 0, "Number of validators to gossip to each accepted container to")
	fs.Uint(ConsensusGossipOnAcceptNonValidatorSizeKey, 0, "Number of non-validators to gossip to each accepted container to")
	fs.Uint(ConsensusGossipOnAcceptPeerSizeKey, 10, "Number of peers to gossip to each accepted container to")
	fs.Uint(AppGossipValidatorSizeKey, 10, "Number of validators to gossip an AppGossip message to")
	fs.Uint(AppGossipNonValidatorSizeKey, 0, "Number of non-validators to gossip an AppGossip message to")
	fs.Uint(AppGossipPeerSizeKey, 0, "Number of peers (which may be validators or non-validators) to gossip an AppGossip message to")

	// Inbound Throttling
	fs.Uint64(InboundThrottlerAtLargeAllocSizeKey, 6*units.MiB, "Size, in bytes, of at-large byte allocation in inbound message throttler")
	fs.Uint64(InboundThrottlerVdrAllocSizeKey, 32*units.MiB, "Size, in bytes, of validator byte allocation in inbound message throttler")
	fs.Uint64(InboundThrottlerNodeMaxAtLargeBytesKey, constants.DefaultMaxMessageSize, "Max number of bytes a node can take from the inbound message throttler's at-large allocation.  Must be at least the max message size")
	fs.Uint64(InboundThrottlerMaxProcessingMsgsPerNodeKey, 1024, "Max number of messages currently processing from a given node")
	fs.Uint64(InboundThrottlerBandwidthRefillRateKey, 512*units.KiB, "Max average inbound bandwidth usage of a peer, in bytes per second. See BandwidthThrottler")
	fs.Uint64(InboundThrottlerBandwidthMaxBurstSizeKey, constants.DefaultMaxMessageSize, "Max inbound bandwidth a node can use at once. Must be at least the max message size. See BandwidthThrottler")
	fs.Duration(InboundThrottlerCPUMaxRecheckDelayKey, 5*time.Second, "In the CPU-based network throttler, check at least this often whether the node's CPU usage has fallen to an acceptable level")
	fs.Duration(InboundThrottlerDiskMaxRecheckDelayKey, 5*time.Second, "In the disk-based network throttler, check at least this often whether the node's disk usage has fallen to an acceptable level")

	// Outbound Throttling
	fs.Uint64(OutboundThrottlerAtLargeAllocSizeKey, 32*units.MiB, "Size, in bytes, of at-large byte allocation in outbound message throttler")
	fs.Uint64(OutboundThrottlerVdrAllocSizeKey, 32*units.MiB, "Size, in bytes, of validator byte allocation in outbound message throttler")
	fs.Uint64(OutboundThrottlerNodeMaxAtLargeBytesKey, constants.DefaultMaxMessageSize, "Max number of bytes a node can take from the outbound message throttler's at-large allocation.  Must be at least the max message size")

	// HTTP APIs
	fs.String(HTTPHostKey, "127.0.0.1", "Address of the HTTP server")
	fs.Uint(HTTPPortKey, DefaultHTTPPort, "Port of the HTTP server")
	fs.Bool(HTTPSEnabledKey, false, "Upgrade the HTTP server to HTTPs")
	fs.String(HTTPSKeyFileKey, "", fmt.Sprintf("TLS private key file for the HTTPs server. Ignored if %s is specified", HTTPSKeyContentKey))
	fs.String(HTTPSKeyContentKey, "", "Specifies base64 encoded TLS private key for the HTTPs server")
	fs.String(HTTPSCertFileKey, "", fmt.Sprintf("TLS certificate file for the HTTPs server. Ignored if %s is specified", HTTPSCertContentKey))
	fs.String(HTTPSCertContentKey, "", "Specifies base64 encoded TLS certificate for the HTTPs server")
	fs.String(HTTPAllowedOrigins, "*", "Origins to allow on the HTTP port. Defaults to * which allows all origins. Example: https://*.avax.network https://*.avax-test.network")
	fs.Duration(HTTPShutdownWaitKey, 0, "Duration to wait after receiving SIGTERM or SIGINT before initiating shutdown. The /health endpoint will return unhealthy during this duration")
	fs.Duration(HTTPShutdownTimeoutKey, 10*time.Second, "Maximum duration to wait for existing connections to complete during node shutdown")
	fs.Bool(APIAuthRequiredKey, false, "Require authorization token to call HTTP APIs")
	fs.String(APIAuthPasswordFileKey, "",
		fmt.Sprintf("Password file used to initially create/validate API authorization tokens. Ignored if %s is specified. Leading and trailing whitespace is removed from the password. Can be changed via API call",
			APIAuthPasswordKey))
	fs.String(APIAuthPasswordKey, "", "Specifies password for API authorization tokens")

	// Enable/Disable APIs
	fs.Bool(AdminAPIEnabledKey, false, "If true, this node exposes the Admin API")
	fs.Bool(InfoAPIEnabledKey, true, "If true, this node exposes the Info API")
	fs.Bool(KeystoreAPIEnabledKey, true, "If true, this node exposes the Keystore API")
	fs.Bool(MetricsAPIEnabledKey, true, "If true, this node exposes the Metrics API")
	fs.Bool(HealthAPIEnabledKey, true, "If true, this node exposes the Health API")
	fs.Bool(IpcAPIEnabledKey, false, "If true, IPCs can be opened")

	// Health Checks
	fs.Duration(HealthCheckFreqKey, 30*time.Second, "Time between health checks")
	fs.Duration(HealthCheckAveragerHalflifeKey, 10*time.Second, "Halflife of averager when calculating a running average in a health check")
	// Network Layer Health
	fs.Duration(NetworkHealthMaxTimeSinceMsgSentKey, time.Minute, "Network layer returns unhealthy if haven't sent a message for at least this much time")
	fs.Duration(NetworkHealthMaxTimeSinceMsgReceivedKey, time.Minute, "Network layer returns unhealthy if haven't received a message for at least this much time")
	fs.Float64(NetworkHealthMaxPortionSendQueueFillKey, 0.9, "Network layer returns unhealthy if more than this portion of the pending send queue is full")
	fs.Uint(NetworkHealthMinPeersKey, 1, "Network layer returns unhealthy if connected to less than this many peers")
	fs.Float64(NetworkHealthMaxSendFailRateKey, .9, "Network layer reports unhealthy if more than this portion of attempted message sends fail")
	// Router Health
	fs.Float64(RouterHealthMaxDropRateKey, 1, "Node reports unhealthy if the router drops more than this portion of messages")
	fs.Uint(RouterHealthMaxOutstandingRequestsKey, 1024, "Node reports unhealthy if there are more than this many outstanding consensus requests (Get, PullQuery, etc.) over all chains")
	fs.Duration(NetworkHealthMaxOutstandingDurationKey, 5*time.Minute, "Node reports unhealthy if there has been a request outstanding for this duration")

	// Staking
	fs.Uint(StakingPortKey, DefaultStakingPort, "Port of the consensus server")
	fs.Bool(StakingEnabledKey, true, "Enable staking. If enabled, Network TLS is required")
	fs.Bool(StakingEphemeralCertEnabledKey, false, "If true, the node uses an ephemeral staking key and certificate, and has an ephemeral node ID")
	fs.String(StakingKeyPathKey, defaultStakingKeyPath, fmt.Sprintf("Path to the TLS private key for staking. Ignored if %s is specified", StakingKeyContentKey))
	fs.String(StakingKeyContentKey, "", "Specifies base64 encoded TLS private key for staking")
	fs.String(StakingCertPathKey, defaultStakingCertPath, fmt.Sprintf("Path to the TLS certificate for staking. Ignored if %s is specified", StakingCertContentKey))
	fs.String(StakingCertContentKey, "", "Specifies base64 encoded TLS certificate for staking")
	fs.Uint64(StakingDisabledWeightKey, 100, "Weight to provide to each peer when staking is disabled")
	// Uptime Requirement
	fs.Float64(UptimeRequirementKey, genesis.LocalParams.UptimeRequirement, "Fraction of time a validator must be online to receive rewards")
	// Minimum Stake required to validate the Primary Network
	fs.Uint64(MinValidatorStakeKey, genesis.LocalParams.MinValidatorStake, "Minimum stake, in nAVAX, required to validate the primary network")
	// Maximum Stake that can be staked and delegated to a validator on the Primary Network
	fs.Uint64(MaxValidatorStakeKey, genesis.LocalParams.MaxValidatorStake, "Maximum stake, in nAVAX, that can be placed on a validator on the primary network")
	// Minimum Stake that can be delegated on the Primary Network
	fs.Uint64(MinDelegatorStakeKey, genesis.LocalParams.MinDelegatorStake, "Minimum stake, in nAVAX, that can be delegated on the primary network")
	fs.Uint64(MinDelegatorFeeKey, uint64(genesis.LocalParams.MinDelegationFee), "Minimum delegation fee, in the range [0, 1000000], that can be charged for delegation on the primary network")
	// Minimum Stake Duration
	fs.Duration(MinStakeDurationKey, genesis.LocalParams.MinStakeDuration, "Minimum staking duration")
	// Maximum Stake Duration
	fs.Duration(MaxStakeDurationKey, genesis.LocalParams.MaxStakeDuration, "Maximum staking duration")
	// Stake Reward Configs
	fs.Uint64(StakeMaxConsumptionRateKey, genesis.LocalParams.RewardConfig.MaxConsumptionRate, "Maximum consumption rate of the remaining tokens to mint in the staking function")
	fs.Uint64(StakeMinConsumptionRateKey, genesis.LocalParams.RewardConfig.MinConsumptionRate, "Minimum consumption rate of the remaining tokens to mint in the staking function")
	fs.Duration(StakeMintingPeriodKey, genesis.LocalParams.RewardConfig.MintingPeriod, "Consumption period of the staking function")
	fs.Uint64(StakeSupplyCapKey, genesis.LocalParams.RewardConfig.SupplyCap, "Supply cap of the staking function")
	// Subnets
	fs.String(WhitelistedSubnetsKey, "", "Whitelist of subnets to validate")

	// State syncing
	fs.String(StateSyncIPsKey, "", "Comma separated list of state sync peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	fs.String(StateSyncIDsKey, "", "Comma separated list of state sync peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	// Bootstrapping
	fs.String(BootstrapIPsKey, "", "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	fs.String(BootstrapIDsKey, "", "Comma separated list of bootstrap peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	fs.Bool(RetryBootstrapKey, true, "Specifies whether bootstrap should be retried")
	fs.Int(RetryBootstrapWarnFrequencyKey, 50, "Specifies how many times bootstrap should be retried before warning the operator")
	fs.Duration(BootstrapBeaconConnectionTimeoutKey, time.Minute, "Timeout when attempting to connect to bootstrapping beacons")
	fs.Duration(BootstrapMaxTimeGetAncestorsKey, 50*time.Millisecond, "Max Time to spend fetching a container and its ancestors when responding to a GetAncestors")
	fs.Uint(BootstrapAncestorsMaxContainersSentKey, 2000, "Max number of containers in an Ancestors message sent by this node")
	fs.Uint(BootstrapAncestorsMaxContainersReceivedKey, 2000, "This node reads at most this many containers from an incoming Ancestors message")

	// Consensus
	fs.Int(SnowSampleSizeKey, 20, "Number of nodes to query for each network poll")
	fs.Int(SnowQuorumSizeKey, 15, "Alpha value to use for required number positive results")
	fs.Int(SnowVirtuousCommitThresholdKey, 15, "Beta value to use for virtuous transactions")
	fs.Int(SnowRogueCommitThresholdKey, 20, "Beta value to use for rogue transactions")
	fs.Int(SnowAvalancheNumParentsKey, 5, "Number of vertexes for reference from each new vertex")
	fs.Int(SnowAvalancheBatchSizeKey, 30, "Number of operations to batch in each new vertex")
	fs.Int(SnowConcurrentRepollsKey, 4, "Minimum number of concurrent polls for finalizing consensus")
	fs.Int(SnowOptimalProcessingKey, 50, "Optimal number of processing containers in consensus")
	fs.Int(SnowMaxProcessingKey, 1024, "Maximum number of processing items to be considered healthy")
	fs.Duration(SnowMaxTimeProcessingKey, 2*time.Minute, "Maximum amount of time an item should be processing and still be healthy")
	fs.Uint(SnowMixedQueryNumPushVdrKey, 10, fmt.Sprintf("If this node is a validator, when a container is inserted into consensus, send a Push Query to %s validators and a Pull Query to the others. Must be <= k.", SnowMixedQueryNumPushVdrKey))
	fs.Uint(SnowMixedQueryNumPushNonVdrKey, 0, fmt.Sprintf("If this node is not a validator, when a container is inserted into consensus, send a Push Query to %s validators and a Pull Query to the others. Must be <= k.", SnowMixedQueryNumPushNonVdrKey))

	// Metrics
	fs.Bool(MeterVMsEnabledKey, true, "Enable Meter VMs to track VM performance with more granularity")
	fs.Duration(UptimeMetricFreqKey, 30*time.Second, "Frequency of renewing this node's average uptime metric")

	// IPC
	fs.String(IpcsChainIDsKey, "", "Comma separated list of chain ids to add to the IPC engine. Example: 11111111111111111111111111111111LpoYY,4R5p2RXDGLqaifZE4hHWH9owe34pfoBULn1DrQTWivjg8o4aH")
	fs.String(IpcsPathKey, "", "The directory (Unix) or named pipe name prefix (Windows) for IPC sockets")

	// Indexer
	fs.Bool(IndexEnabledKey, false, "If true, index all accepted containers and transactions and expose them via an API")
	fs.Bool(IndexAllowIncompleteKey, false, "If true, allow running the node in such a way that could cause an index to miss transactions. Ignored if index is disabled")

	// Config Directories
	fs.String(ChainConfigDirKey, defaultChainConfigDir, fmt.Sprintf("Chain specific configurations parent directory. Ignored if %s is specified", ChainConfigContentKey))
	fs.String(ChainConfigContentKey, "", "Specifies base64 encoded chains configurations")
	fs.String(SubnetConfigDirKey, defaultSubnetConfigDir, fmt.Sprintf("Subnet specific configurations parent directory. Ignored if %s is specified", SubnetConfigContentKey))
	fs.String(SubnetConfigContentKey, "", "Specifies base64 encoded subnets configurations")

	// Profiles
	fs.String(ProfileDirKey, defaultProfileDir, "Path to the profile directory")
	fs.Bool(ProfileContinuousEnabledKey, false, "Whether the app should continuously produce performance profiles")
	fs.Duration(ProfileContinuousFreqKey, 15*time.Minute, "How frequently to rotate performance profiles")
	fs.Int(ProfileContinuousMaxFilesKey, 5, "Maximum number of historical profiles to keep")
	fs.String(VMAliasesFileKey, defaultVMAliasFilePath, fmt.Sprintf("Specifies a JSON file that maps vmIDs with custom aliases. Ignored if %s is specified", VMAliasesContentKey))
	fs.String(VMAliasesContentKey, "", "Specifies base64 encoded maps vmIDs with custom aliases")

	// Delays
	fs.Duration(NetworkInitialReconnectDelayKey, time.Second, "Initial delay duration must be waited before attempting to reconnect a peer")
	fs.Duration(NetworkMaxReconnectDelayKey, time.Hour, "Maximum delay duration must be waited before attempting to reconnect a peer")

	// System resource trackers
	fs.Duration(SystemTrackerFrequencyKey, 500*time.Millisecond, "Frequency to check the real system usage of tracked processes. More frequent checks --> usage metrics are more accurate, but more expensive to track")
	fs.Duration(SystemTrackerProcessingHalflifeKey, 15*time.Second, "Halflife to use for the processing requests tracker. Larger halflife --> usage metrics change more slowly")
	fs.Duration(SystemTrackerCPUHalflifeKey, 15*time.Second, "Halflife to use for the cpu tracker. Larger halflife --> cpu usage metrics change more slowly")
	fs.Duration(SystemTrackerDiskHalflifeKey, time.Minute, "Halflife to use for the disk tracker. Larger halflife --> disk usage metrics change more slowly")
	fs.Uint64(SystemTrackerRequiredAvailableDiskSpaceKey, units.GiB/2, "Minimum number of available bytes on disk, under which the node will shutdown.")
	fs.Uint64(SystemTrackerWarningThresholdAvailableDiskSpaceKey, units.GiB, fmt.Sprintf("Warning threshold for the number of available bytes on disk, under which the node will be considered unhealthy.  Must be >= [%s]", SystemTrackerRequiredAvailableDiskSpaceKey))

	// CPU management
	fs.Float64(CPUVdrAllocKey, float64(runtime.NumCPU()), "Maximum number of CPUs to allocate for use by validators. Value should be in range [0, total core count]")
	fs.Float64(CPUMaxNonVdrUsageKey, .8*float64(runtime.NumCPU()), "Number of CPUs that if fully utilized, will rate limit all non-validators. Value should be in range [0, total core count]")
	fs.Float64(CPUMaxNonVdrNodeUsageKey, float64(runtime.NumCPU())/8, "Maximum number of CPUs that a non-validator can utilize. Value should be in range [0, total core count]")

	// Disk management
	fs.Float64(DiskVdrAllocKey, 1000*units.GiB, "Maximum number of disk reads/writes per second to allocate for use by validators. Must be > 0")
	fs.Float64(DiskMaxNonVdrUsageKey, 1000*units.GiB, "Number of disk reads/writes per second that, if fully utilized, will rate limit all non-validators. Must be >= 0")
	fs.Float64(DiskMaxNonVdrNodeUsageKey, 1000*units.GiB, "Maximum number of disk reads/writes per second that a non-validator can utilize. Must be >= 0")
}

// BuildFlagSet returns a complete set of flags for avalanchego
func BuildFlagSet() *flag.FlagSet {
	// TODO parse directly into a *pflag.FlagSet instead of into a *flag.FlagSet
	// and then putting those into a *plag.FlagSet
	fs := flag.NewFlagSet(constants.AppName, flag.ContinueOnError)
	addProcessFlags(fs)
	addNodeFlags(fs)
	return fs
}

// GetExpandedArg gets the string in viper corresponding to [key] and expands
// any variables using the OS env. If the [AvalancheGoDataDirVar] var is used,
// we expand the value of the variable with the string in viper corresponding to
// [DataDirKey].
func GetExpandedArg(v *viper.Viper, key string) string {
	return GetExpandedString(v, v.GetString(key))
}

// GetExpandedString expands [s] with any variables using the OS env. If the
// [AvalancheGoDataDirVar] var is used, we expand the value of the variable with
// the string in viper corresponding to [DataDirKey].
func GetExpandedString(v *viper.Viper, s string) string {
	return os.Expand(
		s,
		func(strVar string) string {
			if strVar == AvalancheGoDataDirVar {
				return os.ExpandEnv(v.GetString(DataDirKey))
			}
			return os.Getenv(strVar)
		},
	)
}
