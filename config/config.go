// (c) 2021 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/network"

	"github.com/ava-labs/avalanchego/app/process"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/kardianos/osext"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	avalanchegoLatest     = "avalanchego-latest"
	avalanchegoPreupgrade = "avalanchego-preupgrade"
)

// Results of parsing the CLI
var (
	defaultNetworkName = constants.MainnetName

	homeDir                = os.ExpandEnv("$HOME")
	prefixedAppName        = fmt.Sprintf(".%s", constants.AppName)
	defaultDataDir         = filepath.Join(homeDir, prefixedAppName)
	defaultDBDir           = filepath.Join(defaultDataDir, "db")
	defaultProfileDir      = filepath.Join(defaultDataDir, "profiles")
	defaultStakingKeyPath  = filepath.Join(defaultDataDir, "staking", "staker.key")
	defaultStakingCertPath = filepath.Join(defaultDataDir, "staking", "staker.crt")
	// Places to look for the build directory
	defaultBuildDirs = []string{}

	errInvalidStakerWeights = errors.New("staking weights must be positive")
)

func init() {
	folderPath, err := osext.ExecutableFolder()
	if err == nil {
		defaultBuildDirs = append(defaultBuildDirs, folderPath)
		defaultBuildDirs = append(defaultBuildDirs, filepath.Dir(folderPath))
	}
	defaultBuildDirs = append(defaultBuildDirs,
		".",
		filepath.Join("/", "usr", "local", "lib", constants.AppName),
		defaultDataDir,
	)
}

// avalancheFlagSet returns the complete set of flags for avalanchego
func avalancheFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(constants.AppName, flag.ContinueOnError)

	// If true, print the version and quit.
	fs.Bool(VersionKey, false, "If true, print version and quit")

	// Fetch only mode
	fs.Bool(FetchOnlyKey, false, "If true, bootstrap the current database version then stop")

	// System
	fs.Uint64(FdLimitKey, ulimit.DefaultFDLimit, "Attempts to raise the process file descriptor limit to at least this value.")

	// config
	fs.String(ConfigFileKey, "", "Specifies a config file")
	// Genesis config File
	fs.String(GenesisConfigFileKey, "", "Specifies a genesis config file (ignored when running standard networks)")
	// Network ID
	fs.String(NetworkNameKey, defaultNetworkName, "Network ID this node will connect to")
	// AVAX fees
	fs.Uint64(TxFeeKey, units.MilliAvax, "Transaction fee, in nAVAX")
	fs.Uint64(CreationTxFeeKey, units.MilliAvax, "Transaction fee, in nAVAX, for transactions that create new state")
	// Database
	fs.Bool(DBEnabledKey, true, "Turn on persistent storage")
	fs.String(DBPathKey, defaultDBDir, "Path to database directory")
	// Coreth config
	fs.String(CorethConfigKey, "", "Specifies config to pass into coreth")
	// Logging
	fs.String(LogsDirKey, "", "Logging directory for Avalanche")
	fs.String(LogLevelKey, "info", "The log level. Should be one of {verbo, debug, info, warn, error, fatal, off}")
	fs.String(LogDisplayLevelKey, "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, info, warn, error, fatal, off}")
	fs.String(LogDisplayHighlightKey, "auto", "Whether to color/highlight display logs. Default highlights when the output is a terminal. Otherwise, should be one of {auto, plain, colors}")
	// Assertions
	fs.Bool(AssertionsEnabledKey, true, "Turn on assertion execution")
	// Signature Verification
	fs.Bool(SignatureVerificationEnabledKey, true, "Turn on signature verification")

	// Networking
	// Peer List Gossip
	gossipHelpMsg := fmt.Sprintf(
		"Gossip [%s] peers to [%s] peers every [%s]",
		NetworkPeerListSizeKey,
		NetworkPeerListGossipSizeKey,
		NetworkPeerListGossipFreqKey,
	)
	fs.Uint(NetworkPeerListSizeKey, 20, gossipHelpMsg)
	fs.Uint(NetworkPeerListGossipSizeKey, 50, gossipHelpMsg)
	fs.Duration(NetworkPeerListGossipFreqKey, time.Minute, gossipHelpMsg)
	// Public IP Resolution
	fs.String(PublicIPKey, "", "Public IP of this node for P2P communication. If empty, try to discover with NAT. Ignored if dynamic-public-ip is non-empty.")
	fs.Duration(DynamicUpdateDurationKey, 5*time.Minute, "Dynamic IP and NAT Traversal update duration")
	fs.String(DynamicPublicIPResolverKey, "", "'ifconfigco' (alias 'ifconfig') or 'opendns' or 'ifconfigme'. By default does not do dynamic public IP updates. If non-empty, ignores public-ip argument.")
	// Incoming Connection Throttling
	// After we receive [conn-meter-max-conns] incoming connections from a given IP
	// in the last [conn-meter-reset-duration], we close all subsequent incoming connections
	// from the IP before upgrade.
	fs.Duration(ConnMeterResetDurationKey, 0*time.Second,
		"Upgrade at most [conn-meter-max-conns] connections from a given IP per [conn-meter-reset-duration]. "+
			"If [conn-meter-reset-duration] is 0, incoming connections are not rate-limited.")
	fs.Int(ConnMeterMaxConnsKey, 5,
		"Upgrade at most [conn-meter-max-conns] connections from a given IP per [conn-meter-reset-duration]. "+
			"If [conn-meter-reset-duration] is 0, incoming connections are not rate-limited.")
	// Outgoing Connection Throttling
	fs.Uint(OutboundConnectionThrottlingRps, 50, "Make at most this number of outgoing peer connection attempts per second.")
	fs.Duration(OutboundConnectionTimeout, 30*time.Second, "Timeout when dialing a peer.")
	// Timeouts
	fs.Duration(NetworkInitialTimeoutKey, 5*time.Second, "Initial timeout value of the adaptive timeout manager.")
	fs.Duration(NetworkMinimumTimeoutKey, 2*time.Second, "Minimum timeout value of the adaptive timeout manager.")
	fs.Duration(NetworkMaximumTimeoutKey, 10*time.Second, "Maximum timeout value of the adaptive timeout manager.")
	fs.Duration(NetworkTimeoutHalflifeKey, 5*time.Minute, "Halflife of average network response time. Higher value --> network timeout is less volatile. Can't be 0.")
	fs.Float64(NetworkTimeoutCoefficientKey, 2, "Multiplied by average network response time to get the network timeout. Must be >= 1.")
	fs.Uint(SendQueueSizeKey, 512, "Max number of messages waiting to be sent to a given peer.")
	// Peer alias configuration
	fs.Duration(PeerAliasTimeoutKey, 10*time.Minute, "How often the node will attempt to connect "+
		"to an IP address previously associated with a peer (i.e. a peer alias).")
	// Benchlist
	fs.Int(BenchlistFailThresholdKey, 10, "Number of consecutive failed queries before benchlisting a node.")
	fs.Bool(BenchlistPeerSummaryEnabledKey, false, "Enables peer specific query latency metrics.")
	fs.Duration(BenchlistDurationKey, 30*time.Minute, "Max amount of time a peer is benchlisted after surpassing the threshold.")
	fs.Duration(BenchlistMinFailingDurationKey, 5*time.Minute, "Minimum amount of time messages to a peer must be failing before the peer is benched.")
	// Router
	fs.Uint(MaxNonStakerPendingMsgsKey, uint(router.DefaultMaxNonStakerPendingMsgs), "Maximum number of messages a non-staker is allowed to have pending.")
	fs.Float64(StakerMsgReservedKey, router.DefaultStakerPortion, "Reserve a portion of the chain message queue's space for stakers.")
	fs.Float64(StakerCPUReservedKey, router.DefaultStakerPortion, "Reserve a portion of the chain's CPU time for stakers.")
	fs.Uint(MaxPendingMsgsKey, 4096, "Maximum number of pending messages. Messages after this will be dropped.")
	fs.Duration(ConsensusGossipFrequencyKey, 10*time.Second, "Frequency of gossiping accepted frontiers.")
	fs.Duration(ConsensusShutdownTimeoutKey, 5*time.Second, "Timeout before killing an unresponsive chain.")

	// HTTP API
	fs.String(HTTPHostKey, "127.0.0.1", "Address of the HTTP server")
	fs.Uint(HTTPPortKey, 9650, "Port of the HTTP server")
	fs.Bool(HTTPSEnabledKey, false, "Upgrade the HTTP server to HTTPs")
	fs.String(HTTPSKeyFileKey, "", "TLS private key file for the HTTPs server")
	fs.String(HTTPSCertFileKey, "", "TLS certificate file for the HTTPs server")
	fs.String(HTTPAllowedOrigins, "*", "Origins to allow on the HTTP port. Defaults to * which allows all origins. Example: https://*.avax.network https://*.avax-test.network")
	fs.Bool(APIAuthRequiredKey, false, "Require authorization token to call HTTP APIs")
	fs.String(APIAuthPasswordFileKey, "", "Password file used to initially create/validate API authorization tokens. Leading and trailing whitespace is removed from the password. Can be changed via API call.")
	// Enable/Disable APIs
	fs.Bool(AdminAPIEnabledKey, false, "If true, this node exposes the Admin API")
	fs.Bool(InfoAPIEnabledKey, true, "If true, this node exposes the Info API")
	fs.Bool(KeystoreAPIEnabledKey, true, "If true, this node exposes the Keystore API")
	fs.Bool(MetricsAPIEnabledKey, true, "If true, this node exposes the Metrics API")
	fs.Bool(HealthAPIEnabledKey, true, "If true, this node exposes the Health API")
	fs.Bool(IpcAPIEnabledKey, false, "If true, IPCs can be opened")
	// Health
	fs.Duration(HealthCheckFreqKey, 30*time.Second, "Time between health checks")
	fs.Duration(HealthCheckAveragerHalflifeKey, 10*time.Second, "Halflife of averager when calculating a running average in a health check")
	// Network Layer Health
	fs.Duration(NetworkHealthMaxTimeSinceMsgSentKey, time.Minute, "Network layer returns unhealthy if haven't sent a message for at least this much time")
	fs.Duration(NetworkHealthMaxTimeSinceMsgReceivedKey, time.Minute, "Network layer returns unhealthy if haven't received a message for at least this much time")
	fs.Float64(NetworkHealthMaxPortionSendQueueFillKey, 0.9, "Network layer returns unhealthy if more than this portion of the pending send queue is full")
	fs.Uint(NetworkHealthMinPeersKey, 1, "Network layer returns unhealthy if connected to less than this many peers")
	fs.Float64(NetworkHealthMaxSendFailRateKey, .9, "Network layer reports unhealthy if more than this portion of attempted message sends fail")
	// Router Health
	fs.Float64(RouterHealthMaxDropRateKey, 1, "Node reports unhealthy if the router drops more than this portion of messages.")
	fs.Uint(RouterHealthMaxOutstandingRequestsKey, 1024, "Node reports unhealthy if there are more than this many outstanding consensus requests (Get, PullQuery, etc.) over all chains")
	fs.Duration(NetworkHealthMaxOutstandingDurationKey, 5*time.Minute, "Node reports unhealthy if there has been a request outstanding for this duration")

	// Staking
	fs.Uint(StakingPortKey, 9651, "Port of the consensus server")
	fs.Bool(StakingEnabledKey, true, "Enable staking. If enabled, Network TLS is required.")
	fs.String(StakingKeyPathKey, defaultStakingKeyPath, "Path to the TLS private key for staking")
	fs.String(StakingCertPathKey, defaultStakingCertPath, "Path to the TLS certificate for staking")
	fs.Uint64(StakingDisabledWeightKey, 1, "Weight to provide to each peer when staking is disabled")
	// Uptime Requirement
	fs.Float64(UptimeRequirementKey, .6, "Fraction of time a validator must be online to receive rewards")
	// Minimum Stake required to validate the Primary Network
	fs.Uint64(MinValidatorStakeKey, 2*units.KiloAvax, "Minimum stake, in nAVAX, required to validate the primary network")
	// Maximum Stake that can be staked and delegated to a validator on the Primary Network
	fs.Uint64(MaxValidatorStakeKey, 3*units.MegaAvax, "Maximum stake, in nAVAX, that can be placed on a validator on the primary network")
	// Minimum Stake that can be delegated on the Primary Network
	fs.Uint64(MinDelegatorStakeKey, 25*units.Avax, "Minimum stake, in nAVAX, that can be delegated on the primary network")
	fs.Uint64(MinDelegatorFeeKey, 20000, "Minimum delegation fee, in the range [0, 1000000], that can be charged for delegation on the primary network")
	// Minimum Stake Duration
	fs.Duration(MinStakeDurationKey, 24*time.Hour, "Minimum staking duration")
	// Maximum Stake Duration
	fs.Duration(MaxStakeDurationKey, 365*24*time.Hour, "Maximum staking duration")
	// Stake Minting Period
	fs.Duration(StakeMintingPeriodKey, 365*24*time.Hour, "Consumption period of the staking function")
	// Subnets
	fs.String(WhitelistedSubnetsKey, "", "Whitelist of subnets to validate.")
	// Bootstrapping
	fs.String(BootstrapIPsKey, "", "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	fs.String(BootstrapIDsKey, "", "Comma separated list of bootstrap peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	fs.Bool(RetryBootstrapKey, true, "Specifies whether bootstrap should be retried")
	fs.Int(RetryBootstrapMaxAttemptsKey, 50, "Specifies how many times bootstrap should be retried")
	fs.Duration(BootstrapBeaconConnectionTimeoutKey, time.Minute, "Timeout when attempting to connect to bootstrapping beacons.")

	// Consensus
	fs.Int(SnowSampleSizeKey, 20, "Number of nodes to query for each network poll")
	fs.Int(SnowQuorumSizeKey, 14, "Alpha value to use for required number positive results")
	fs.Int(SnowVirtuousCommitThresholdKey, 15, "Beta value to use for virtuous transactions")
	fs.Int(SnowRogueCommitThresholdKey, 20, "Beta value to use for rogue transactions")
	fs.Int(SnowAvalancheNumParentsKey, 5, "Number of vertexes for reference from each new vertex")
	fs.Int(SnowAvalancheBatchSizeKey, 30, "Number of operations to batch in each new vertex")
	fs.Int(SnowConcurrentRepollsKey, 4, "Minimum number of concurrent polls for finalizing consensus")
	fs.Int(SnowOptimalProcessingKey, 50, "Optimal number of processing vertices in consensus")
	fs.Int(SnowMaxProcessingKey, 1024, "Maximum number of processing items to be considered healthy")
	fs.Duration(SnowMaxTimeProcessingKey, 2*time.Minute, "Maximum amount of time an item should be processing and still be healthy")
	fs.Int64(SnowEpochFirstTransition, 1607626800, "Unix timestamp of the first epoch transaction, in seconds. Defaults to 12/10/2020 @ 7:00pm (UTC)")
	fs.Duration(SnowEpochDuration, 6*time.Hour, "Duration of each epoch")

	// IPC
	fs.String(IpcsChainIDsKey, "", "Comma separated list of chain ids to add to the IPC engine. Example: 11111111111111111111111111111111LpoYY,4R5p2RXDGLqaifZE4hHWH9owe34pfoBULn1DrQTWivjg8o4aH")
	fs.String(IpcsPathKey, "", "The directory (Unix) or named pipe name prefix (Windows) for IPC sockets")

	// Indexer
	fs.Bool(IndexEnabledKey, false, "If true, index all accepted containers and transactions and expose them via an API")
	fs.Bool(IndexAllowIncompleteKey, false, "If true, allow running the node in such a way that could cause an index to miss transactions. Ignored if index is disabled.")
	// Plugin
	fs.Bool(PluginModeKey, true, "Whether the app should run as a plugin. Defaults to true")
	// Build directory
	fs.String(BuildDirKey, defaultBuildDirs[0], "Path to the build directory")

	// Profiles
	fs.String(ProfileDirKey, defaultProfileDir, "Path to the profile directory")
	fs.Bool(ProfileContinuousEnabledKey, false, "Whether the app should continuously produce performance profiles")
	fs.Duration(ProfileContinuousFreqKey, 15*time.Minute, "How frequently to rotate performance profiles")
	fs.Int(ProfileContinuousMaxFilesKey, 5, "Maximum number of historical profiles to keep")
	return fs
}

// getViper returns the viper environment from parsing config file from default search paths
// and any parsed command line flags
func getViper() (*viper.Viper, error) {
	v := viper.New()
	fs := avalancheFlagSet()
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		return nil, err
	}
	if v.IsSet(ConfigFileKey) {
		v.SetConfigFile(os.ExpandEnv(v.GetString(ConfigFileKey)))
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// getConfigFromViper sets attributes on [config] based on the values
// defined in the [viper] environment
func getConfigsFromViper(v *viper.Viper) (node.Config, process.Config, error) {
	// First, get the process config
	processConfig := process.Config{}
	processConfig.DisplayVersionAndExit = v.GetBool(VersionKey)
	processConfig.PluginMode = v.GetBool(PluginModeKey)

	// Build directory should have this structure:
	// build
	// |_avalanchego-latest
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	// |_avalanchego-preupgrade
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	processConfig.BuildDir = os.ExpandEnv(v.GetString(BuildDirKey))
	validBuildDir := func(dir string) bool {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return false
		}
		// make sure both expected subdirectories exist
		if _, err := os.Stat(filepath.Join(dir, avalanchegoLatest)); err != nil {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, avalanchegoPreupgrade)); err != nil {
			return false
		}
		return true
	}
	if !validBuildDir(processConfig.BuildDir) {
		foundBuildDir := false
		for _, dir := range defaultBuildDirs {
			if validBuildDir(dir) {
				processConfig.BuildDir = dir
				foundBuildDir = true
				break
			}
		}
		if !foundBuildDir {
			return node.Config{}, process.Config{}, fmt.Errorf("couldn't find valid build directory in any of the default locations: %s", defaultBuildDirs)
		}
	}

	// Then, get the node config
	nodeConfig := node.Config{}

	// Plugin directory defaults to [buildDirectory]/avalanchego-latest/plugins
	nodeConfig.PluginDir = filepath.Join(processConfig.BuildDir, avalanchegoLatest, "plugins")

	nodeConfig.FetchOnly = v.GetBool(FetchOnlyKey)

	// Consensus Parameters
	nodeConfig.ConsensusParams.K = v.GetInt(SnowSampleSizeKey)
	nodeConfig.ConsensusParams.Alpha = v.GetInt(SnowQuorumSizeKey)
	nodeConfig.ConsensusParams.BetaVirtuous = v.GetInt(SnowVirtuousCommitThresholdKey)
	nodeConfig.ConsensusParams.BetaRogue = v.GetInt(SnowRogueCommitThresholdKey)
	nodeConfig.ConsensusParams.Parents = v.GetInt(SnowAvalancheNumParentsKey)
	nodeConfig.ConsensusParams.BatchSize = v.GetInt(SnowAvalancheBatchSizeKey)
	nodeConfig.ConsensusParams.ConcurrentRepolls = v.GetInt(SnowConcurrentRepollsKey)
	nodeConfig.ConsensusParams.OptimalProcessing = v.GetInt(SnowOptimalProcessingKey)
	nodeConfig.ConsensusParams.MaxOutstandingItems = v.GetInt(SnowMaxProcessingKey)
	nodeConfig.ConsensusParams.MaxItemProcessingTime = v.GetDuration(SnowMaxTimeProcessingKey)
	nodeConfig.ConsensusGossipFrequency = v.GetDuration(ConsensusGossipFrequencyKey)
	nodeConfig.ConsensusShutdownTimeout = v.GetDuration(ConsensusShutdownTimeoutKey)

	// Logging:
	loggingConfig, err := logging.DefaultConfig()
	if err != nil {
		return node.Config{}, process.Config{}, err
	}
	if v.IsSet(LogsDirKey) {
		loggingConfig.Directory = os.ExpandEnv(v.GetString(LogsDirKey))
	}
	loggingConfig.LogLevel, err = logging.ToLevel(v.GetString(LogLevelKey))
	if err != nil {
		return node.Config{}, process.Config{}, err
	}
	logDisplayLevel := v.GetString(LogLevelKey)
	if v.IsSet(LogDisplayLevelKey) {
		logDisplayLevel = v.GetString(LogDisplayLevelKey)
	}
	displayLevel, err := logging.ToLevel(logDisplayLevel)
	if err != nil {
		return node.Config{}, process.Config{}, err
	}
	loggingConfig.DisplayLevel = displayLevel

	loggingConfig.DisplayHighlight, err = logging.ToHighlight(v.GetString(LogDisplayHighlightKey), os.Stdout.Fd())
	if err != nil {
		return node.Config{}, process.Config{}, err
	}

	nodeConfig.LoggingConfig = loggingConfig

	// NetworkID
	networkID, err := constants.NetworkID(v.GetString(NetworkNameKey))
	if err != nil {
		return node.Config{}, process.Config{}, err
	}
	nodeConfig.NetworkID = networkID

	// DB:
	nodeConfig.DBEnabled = v.GetBool(DBEnabledKey)
	nodeConfig.DBPath = filepath.Join(
		os.ExpandEnv(v.GetString(DBPathKey)),
		constants.NetworkName(nodeConfig.NetworkID),
	)

	// IP configuration
	// Resolves our public IP, or does nothing
	nodeConfig.DynamicPublicIPResolver = dynamicip.NewResolver(v.GetString(DynamicPublicIPResolverKey))

	var ip net.IP
	publicIP := v.GetString(PublicIPKey)
	switch {
	case nodeConfig.DynamicPublicIPResolver.IsResolver():
		// User specified to use dynamic IP resolution; don't use NAT traversal
		nodeConfig.Nat = nat.NewNoRouter()
		ip, err = dynamicip.FetchExternalIP(nodeConfig.DynamicPublicIPResolver)
		if err != nil {
			return node.Config{}, process.Config{}, fmt.Errorf("dynamic ip address fetch failed: %s", err)
		}

	case publicIP == "":
		// User didn't specify a public IP to use; try with NAT traversal
		nodeConfig.AttemptedNATTraversal = true
		nodeConfig.Nat = nat.GetRouter()
		ip, err = nodeConfig.Nat.ExternalIP()
		if err != nil {
			ip = net.IPv4zero // Couldn't get my IP...set to 0.0.0.0
		}
	default:
		// User specified a public IP to use; don't use NAT
		nodeConfig.Nat = nat.NewNoRouter()
		ip = net.ParseIP(publicIP)
	}

	if ip == nil {
		return node.Config{}, process.Config{}, fmt.Errorf("invalid IP Address %s", publicIP)
	}

	stakingPort := uint16(v.GetUint(StakingPortKey))

	nodeConfig.StakingIP = utils.NewDynamicIPDesc(ip, stakingPort)

	nodeConfig.DynamicUpdateDuration = v.GetDuration(DynamicUpdateDurationKey)
	nodeConfig.ConnMeterResetDuration = v.GetDuration(ConnMeterResetDurationKey)
	nodeConfig.ConnMeterMaxConns = v.GetInt(ConnMeterMaxConnsKey)

	// Staking:
	nodeConfig.EnableStaking = v.GetBool(StakingEnabledKey)
	nodeConfig.DisabledStakingWeight = v.GetUint64(StakingDisabledWeightKey)
	nodeConfig.MinStakeDuration = v.GetDuration(MinStakeDurationKey)
	nodeConfig.MaxStakeDuration = v.GetDuration(MaxStakeDurationKey)
	nodeConfig.StakeMintingPeriod = v.GetDuration(StakeMintingPeriodKey)
	if !nodeConfig.EnableStaking && nodeConfig.DisabledStakingWeight == 0 {
		return node.Config{}, process.Config{}, errInvalidStakerWeights
	}

	if nodeConfig.FetchOnly {
		// In fetch only mode, use an ephemeral staking key/cert
		cert, err := staking.NewTLSCert()
		if err != nil {
			return node.Config{}, process.Config{}, fmt.Errorf("couldn't generate dummy staking key/cert: %w", err)
		}
		nodeConfig.StakingTLSCert = *cert
	} else {
		// Parse the staking key/cert paths
		stakingKeyPath := os.ExpandEnv(v.GetString(StakingKeyPathKey))
		stakingCertPath := os.ExpandEnv(v.GetString(StakingCertPathKey))

		switch {
		// If staking key/cert locations are specified but not found, error
		case v.IsSet(StakingKeyPathKey) || v.IsSet(StakingCertPathKey):
			if _, err := os.Stat(stakingKeyPath); os.IsNotExist(err) {
				return node.Config{}, process.Config{}, fmt.Errorf("couldn't find staking key at %s", stakingKeyPath)
			} else if _, err := os.Stat(stakingCertPath); os.IsNotExist(err) {
				return node.Config{}, process.Config{}, fmt.Errorf("couldn't find staking certificate at %s", stakingCertPath)
			}
		default:
			// Create the staking key/cert if [stakingKeyPath] doesn't exist
			if err := staking.InitNodeStakingKeyPair(stakingKeyPath, stakingCertPath); err != nil {
				return node.Config{}, process.Config{}, fmt.Errorf("couldn't generate staking key/cert: %w", err)
			}
		}

		// Load and parse the staking key/cert
		cert, err := staking.LoadTLSCert(stakingKeyPath, stakingCertPath)
		if err != nil {
			return node.Config{}, process.Config{}, fmt.Errorf("problem reading staking certificate: %w", err)
		}
		nodeConfig.StakingTLSCert = *cert
	}

	if err := initBootstrapPeers(v, &nodeConfig); err != nil {
		return node.Config{}, process.Config{}, err
	}

	nodeConfig.WhitelistedSubnets.Add(constants.PrimaryNetworkID)
	for _, subnet := range strings.Split(v.GetString(WhitelistedSubnetsKey), ",") {
		if subnet != "" {
			subnetID, err := ids.FromString(subnet)
			if err != nil {
				return node.Config{}, process.Config{}, fmt.Errorf("couldn't parse subnetID %s: %w", subnet, err)
			}
			nodeConfig.WhitelistedSubnets.Add(subnetID)
		}
	}

	// HTTP:
	nodeConfig.HTTPHost = v.GetString(HTTPHostKey)
	nodeConfig.HTTPPort = uint16(v.GetUint(HTTPPortKey))
	nodeConfig.HTTPSEnabled = v.GetBool(HTTPSEnabledKey)
	nodeConfig.HTTPSKeyFile = os.ExpandEnv(v.GetString(HTTPSKeyFileKey))
	nodeConfig.HTTPSCertFile = os.ExpandEnv(v.GetString(HTTPSCertFileKey))
	nodeConfig.APIAllowedOrigins = v.GetStringSlice(HTTPAllowedOrigins)

	// API Auth
	nodeConfig.APIRequireAuthToken = v.GetBool(APIAuthRequiredKey)
	if nodeConfig.APIRequireAuthToken {
		passwordFile := v.GetString(APIAuthPasswordFileKey)
		pwBytes, err := ioutil.ReadFile(passwordFile)
		if err != nil {
			return node.Config{}, process.Config{}, fmt.Errorf("api-auth-password-file %q failed to be read with: %w", passwordFile, err)
		}
		nodeConfig.APIAuthPassword = strings.TrimSpace(string(pwBytes))
		if !password.SufficientlyStrong(nodeConfig.APIAuthPassword, password.OK) {
			return node.Config{}, process.Config{}, errors.New("api-auth-password is not strong enough")
		}
	}

	// APIs
	nodeConfig.AdminAPIEnabled = v.GetBool(AdminAPIEnabledKey)
	nodeConfig.InfoAPIEnabled = v.GetBool(InfoAPIEnabledKey)
	nodeConfig.KeystoreAPIEnabled = v.GetBool(KeystoreAPIEnabledKey)
	nodeConfig.MetricsAPIEnabled = v.GetBool(MetricsAPIEnabledKey)
	nodeConfig.HealthAPIEnabled = v.GetBool(HealthAPIEnabledKey)
	nodeConfig.IPCAPIEnabled = v.GetBool(IpcAPIEnabledKey)
	nodeConfig.IndexAPIEnabled = v.GetBool(IndexEnabledKey)

	// Halflife of continuous averager used in health checks
	healthCheckAveragerHalflife := v.GetDuration(HealthCheckAveragerHalflifeKey)
	if healthCheckAveragerHalflife <= 0 {
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be positive", HealthCheckAveragerHalflifeKey)
	}

	// Router
	nodeConfig.ConsensusRouter = &router.ChainRouter{}
	nodeConfig.RouterHealthConfig.MaxDropRate = v.GetFloat64(RouterHealthMaxDropRateKey)
	nodeConfig.RouterHealthConfig.MaxOutstandingRequests = int(v.GetUint(RouterHealthMaxOutstandingRequestsKey))
	nodeConfig.RouterHealthConfig.MaxOutstandingDuration = v.GetDuration(NetworkHealthMaxOutstandingDurationKey)
	nodeConfig.RouterHealthConfig.MaxRunTimeRequests = v.GetDuration(NetworkMaximumTimeoutKey)
	nodeConfig.RouterHealthConfig.MaxDropRateHalflife = healthCheckAveragerHalflife
	switch {
	case nodeConfig.RouterHealthConfig.MaxDropRate < 0 || nodeConfig.RouterHealthConfig.MaxDropRate > 1:
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be in [0,1]", RouterHealthMaxDropRateKey)
	case nodeConfig.RouterHealthConfig.MaxOutstandingDuration <= 0:
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be positive", NetworkHealthMaxOutstandingDurationKey)
	}

	// IPCs
	if v.IsSet(IpcsChainIDsKey) {
		nodeConfig.IPCDefaultChainIDs = strings.Split(v.GetString(IpcsChainIDsKey), ",")
	}

	if v.IsSet(IpcsPathKey) {
		nodeConfig.IPCPath = os.ExpandEnv(v.GetString(IpcsPathKey))
	} else {
		nodeConfig.IPCPath = ipcs.DefaultBaseURL
	}

	// Throttling
	nodeConfig.MaxNonStakerPendingMsgs = v.GetUint32(MaxNonStakerPendingMsgsKey)
	nodeConfig.StakerMSGPortion = v.GetFloat64(StakerMsgReservedKey)
	nodeConfig.StakerCPUPortion = v.GetFloat64(StakerCPUReservedKey)
	nodeConfig.SendQueueSize = v.GetUint32(SendQueueSizeKey)
	nodeConfig.MaxPendingMsgs = v.GetUint32(MaxPendingMsgsKey)
	if nodeConfig.MaxPendingMsgs < nodeConfig.MaxNonStakerPendingMsgs {
		return node.Config{}, process.Config{}, errors.New("maximum pending messages must be >= maximum non-staker pending messages")
	}

	// Health
	nodeConfig.HealthCheckFreq = v.GetDuration(HealthCheckFreqKey)
	// Network Health Check
	nodeConfig.NetworkHealthConfig.MaxTimeSinceMsgSent = v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey)
	nodeConfig.NetworkHealthConfig.MaxTimeSinceMsgReceived = v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey)
	nodeConfig.NetworkHealthConfig.MaxPortionSendQueueBytesFull = v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey)
	nodeConfig.NetworkHealthConfig.MinConnectedPeers = v.GetUint(NetworkHealthMinPeersKey)
	nodeConfig.NetworkHealthConfig.MaxSendFailRate = v.GetFloat64(NetworkHealthMaxSendFailRateKey)
	nodeConfig.NetworkHealthConfig.MaxSendFailRateHalflife = healthCheckAveragerHalflife
	switch {
	case nodeConfig.NetworkHealthConfig.MaxTimeSinceMsgSent < 0:
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case nodeConfig.NetworkHealthConfig.MaxTimeSinceMsgReceived < 0:
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case nodeConfig.NetworkHealthConfig.MaxSendFailRate < 0 || nodeConfig.NetworkHealthConfig.MaxSendFailRate > 1:
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case nodeConfig.NetworkHealthConfig.MaxPortionSendQueueBytesFull < 0 || nodeConfig.NetworkHealthConfig.MaxPortionSendQueueBytesFull > 1:
		return node.Config{}, process.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	}

	// Network Timeout
	nodeConfig.NetworkConfig.InitialTimeout = v.GetDuration(NetworkInitialTimeoutKey)
	nodeConfig.NetworkConfig.MinimumTimeout = v.GetDuration(NetworkMinimumTimeoutKey)
	nodeConfig.NetworkConfig.MaximumTimeout = v.GetDuration(NetworkMaximumTimeoutKey)
	nodeConfig.NetworkConfig.TimeoutHalflife = v.GetDuration(NetworkTimeoutHalflifeKey)
	nodeConfig.NetworkConfig.TimeoutCoefficient = v.GetFloat64(NetworkTimeoutCoefficientKey)

	switch {
	case nodeConfig.NetworkConfig.MinimumTimeout < 1:
		return node.Config{}, process.Config{}, errors.New("minimum timeout must be positive")
	case nodeConfig.NetworkConfig.MinimumTimeout > nodeConfig.NetworkConfig.MaximumTimeout:
		return node.Config{}, process.Config{}, errors.New("maximum timeout can't be less than minimum timeout")
	case nodeConfig.NetworkConfig.InitialTimeout < nodeConfig.NetworkConfig.MinimumTimeout ||
		nodeConfig.NetworkConfig.InitialTimeout > nodeConfig.NetworkConfig.MaximumTimeout:
		return node.Config{}, process.Config{}, errors.New("initial timeout should be in the range [minimumTimeout, maximumTimeout]")
	case nodeConfig.NetworkConfig.TimeoutHalflife <= 0:
		return node.Config{}, process.Config{}, errors.New("network timeout halflife must be positive")
	case nodeConfig.NetworkConfig.TimeoutCoefficient < 1:
		return node.Config{}, process.Config{}, errors.New("network timeout coefficient must be >= 1")
	}

	// Node will gossip [PeerListSize] peers to [PeerListGossipSize] every
	// [PeerListGossipFreq]
	nodeConfig.PeerListSize = v.GetUint32(NetworkPeerListSizeKey)
	nodeConfig.PeerListGossipFreq = v.GetDuration(NetworkPeerListGossipFreqKey)
	nodeConfig.PeerListGossipSize = v.GetUint32(NetworkPeerListGossipSizeKey)

	// Outbound connection throttling
	nodeConfig.DialerConfig = network.NewDialerConfig(
		v.GetUint32(OutboundConnectionThrottlingRps),
		v.GetDuration(OutboundConnectionTimeout),
	)

	// Benchlist
	nodeConfig.BenchlistConfig.Threshold = v.GetInt(BenchlistFailThresholdKey)
	nodeConfig.BenchlistConfig.PeerSummaryEnabled = v.GetBool(BenchlistPeerSummaryEnabledKey)
	nodeConfig.BenchlistConfig.Duration = v.GetDuration(BenchlistDurationKey)
	nodeConfig.BenchlistConfig.MinimumFailingDuration = v.GetDuration(BenchlistMinFailingDurationKey)
	nodeConfig.BenchlistConfig.MaxPortion = (1.0 - (float64(nodeConfig.ConsensusParams.Alpha) / float64(nodeConfig.ConsensusParams.K))) / 3.0

	if nodeConfig.ConsensusGossipFrequency < 0 {
		return node.Config{}, process.Config{}, errors.New("gossip frequency can't be negative")
	}
	if nodeConfig.ConsensusShutdownTimeout < 0 {
		return node.Config{}, process.Config{}, errors.New("gossip frequency can't be negative")
	}

	// File Descriptor Limit
	fdLimit := v.GetUint64(FdLimitKey)
	if err := ulimit.Set(fdLimit); err != nil {
		return node.Config{}, process.Config{}, fmt.Errorf("failed to set fd limit correctly due to: %w", err)
	}

	// Network Parameters
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		txFee := v.GetUint64(TxFeeKey)
		creationTxFee := v.GetUint64(CreationTxFeeKey)
		uptimeRequirement := v.GetFloat64(UptimeRequirementKey)
		nodeConfig.TxFee = txFee
		nodeConfig.CreationTxFee = creationTxFee
		nodeConfig.UptimeRequirement = uptimeRequirement

		minValidatorStake := v.GetUint64(MinValidatorStakeKey)
		maxValidatorStake := v.GetUint64(MaxValidatorStakeKey)
		minDelegatorStake := v.GetUint64(MinDelegatorStakeKey)
		minDelegationFee := v.GetUint64(MinDelegatorFeeKey)
		if minValidatorStake > maxValidatorStake {
			return node.Config{}, process.Config{}, errors.New("minimum validator stake can't be greater than maximum validator stake")
		}

		nodeConfig.MinValidatorStake = minValidatorStake
		nodeConfig.MaxValidatorStake = maxValidatorStake
		nodeConfig.MinDelegatorStake = minDelegatorStake

		if minDelegationFee > 1000000 {
			return node.Config{}, process.Config{}, errors.New("delegation fee must be in the range [0, 1000000]")
		}
		nodeConfig.MinDelegationFee = uint32(minDelegationFee)

		if nodeConfig.MinStakeDuration == 0 {
			return node.Config{}, process.Config{}, errors.New("min stake duration can't be zero")
		}
		if nodeConfig.MaxStakeDuration < nodeConfig.MinStakeDuration {
			return node.Config{}, process.Config{}, errors.New("max stake duration can't be less than min stake duration")
		}
		if nodeConfig.StakeMintingPeriod < nodeConfig.MaxStakeDuration {
			return node.Config{}, process.Config{}, errors.New("stake minting period can't be less than max stake duration")
		}

		nodeConfig.EpochFirstTransition = time.Unix(v.GetInt64(SnowEpochFirstTransition), 0)
		nodeConfig.EpochDuration = v.GetDuration(SnowEpochDuration)
	} else {
		nodeConfig.Params = *genesis.GetParams(networkID)
	}

	// Load genesis data
	nodeConfig.GenesisBytes, nodeConfig.AvaxAssetID, err = genesis.Genesis(
		networkID,
		os.ExpandEnv(v.GetString(GenesisConfigFileKey)),
	)
	if err != nil {
		return node.Config{}, process.Config{}, fmt.Errorf("unable to load genesis file: %w", err)
	}

	// Assertions
	nodeConfig.EnableAssertions = v.GetBool(AssertionsEnabledKey)

	// Crypto
	nodeConfig.EnableCrypto = v.GetBool(SignatureVerificationEnabledKey)

	// Coreth Plugin
	if v.IsSet(CorethConfigKey) {
		corethConfigValue := v.Get(CorethConfigKey)
		switch value := corethConfigValue.(type) {
		case string:
			nodeConfig.CorethConfig = value
		default:
			corethConfigBytes, err := json.Marshal(value)
			if err != nil {
				return node.Config{}, process.Config{}, fmt.Errorf("couldn't parse coreth config: %w", err)
			}
			nodeConfig.CorethConfig = string(corethConfigBytes)
		}
	}

	// Indexer
	nodeConfig.IndexAllowIncomplete = v.GetBool(IndexAllowIncompleteKey)

	// Bootstrap Configs
	nodeConfig.RetryBootstrap = v.GetBool(RetryBootstrapKey)
	nodeConfig.RetryBootstrapMaxAttempts = v.GetInt(RetryBootstrapMaxAttemptsKey)
	nodeConfig.BootstrapBeaconConnectionTimeout = v.GetDuration(BootstrapBeaconConnectionTimeoutKey)

	// Peer alias
	nodeConfig.PeerAliasTimeout = v.GetDuration(PeerAliasTimeoutKey)

	// Profile config
	nodeConfig.ProfilerConfig.Dir = os.ExpandEnv(v.GetString(ProfileDirKey))
	nodeConfig.ProfilerConfig.Enabled = v.GetBool(ProfileContinuousEnabledKey)
	nodeConfig.ProfilerConfig.Freq = v.GetDuration(ProfileContinuousFreqKey)
	nodeConfig.ProfilerConfig.MaxNumFiles = v.GetInt(ProfileContinuousMaxFilesKey)

	return nodeConfig, processConfig, nil
}

// Initialize config.BootstrapPeers.
func initBootstrapPeers(v *viper.Viper, config *node.Config) error {
	bootstrapIPs, bootstrapIDs := genesis.SampleBeacons(config.NetworkID, 5)
	if v.IsSet(BootstrapIPsKey) {
		bootstrapIPs = strings.Split(v.GetString(BootstrapIPsKey), ",")
	}
	for _, ip := range bootstrapIPs {
		if ip == "" {
			continue
		}
		addr, err := utils.ToIPDesc(ip)
		if err != nil {
			return fmt.Errorf("couldn't parse bootstrap ip %s: %w", ip, err)
		}
		config.BootstrapIPs = append(config.BootstrapIPs, addr)
	}

	if v.IsSet(BootstrapIDsKey) {
		bootstrapIDs = strings.Split(v.GetString(BootstrapIDsKey), ",")
	}
	for _, id := range bootstrapIDs {
		if id == "" {
			continue
		}
		nodeID, err := ids.ShortFromPrefixedString(id, constants.NodeIDPrefix)
		if err != nil {
			return fmt.Errorf("couldn't parse bootstrap peer id: %w", err)
		}
		config.BootstrapIDs = append(config.BootstrapIDs, nodeID)
	}
	return nil
}

// Returns:
// 1) The node config
// 2) The process config
func GetConfigs(commit string) (node.Config, process.Config, error) {
	v, err := getViper()
	if err != nil {
		return node.Config{}, process.Config{}, err
	}

	nodeConfig, processConfig, err := getConfigsFromViper(v)
	if processConfig.DisplayVersionAndExit {
		format := "%s ["
		args := []interface{}{
			version.Current,
		}

		networkID, err := constants.NetworkID(v.GetString(NetworkNameKey))
		if err != nil {
			return node.Config{}, process.Config{}, err
		}
		networkGeneration := constants.NetworkName(networkID)
		if networkID == constants.MainnetID {
			format += "network=%s"
		} else {
			format += "network=testnet/%s"
		}
		args = append(args, networkGeneration)

		format += ", database=%s"
		args = append(args, version.CurrentDatabase)

		if commit != "" {
			format += ", commit=%s"
			args = append(args, commit)
		}
		format += "]\n"

		processConfig.VersionStr = fmt.Sprintf(format, args...)
	}

	return nodeConfig, processConfig, err
}
