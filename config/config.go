// (c) 2021 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/config/versionconfig"
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
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/utils/units"
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
	defaultDbDir           = filepath.Join(defaultDataDir, "db")
	defaultStakingKeyPath  = filepath.Join(defaultDataDir, "staking", "staker.key")
	defaultStakingCertPath = filepath.Join(defaultDataDir, "staking", "staker.crt")
	// Places to look for the build directory
	defaultBuildDirs = []string{}
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

var (
	errBootstrapMismatch    = errors.New("more bootstrap IDs provided than bootstrap IPs")
	errStakingRequiresTLS   = errors.New("if staking is enabled, network TLS must also be enabled")
	errInvalidStakerWeights = errors.New("staking weights must be positive")
)

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
	fs.String(ConfigFileKey, DefaultString, "Specifies a config file")
	// Genesis config File
	fs.String(GenesisConfigFileKey, "", "Specifies a genesis config file (ignored when running standard networks)")
	// Plugins
	fs.String(PluginDirKey, DefaultString, "Plugin directory for Avalanche VMs")
	// Network ID
	fs.String(NetworkNameKey, defaultNetworkName, "Network ID this node will connect to")
	// AVAX fees
	fs.Uint64(TxFeeKey, units.MilliAvax, "Transaction fee, in nAVAX")
	fs.Uint64(CreationTxFeeKey, units.MilliAvax, "Transaction fee, in nAVAX, for transactions that create new state")
	// Database
	fs.Bool(DbEnabledKey, true, "Turn on persistent storage")
	fs.String(DbPathKey, defaultDbDir, "Path to database directory")
	// Coreth config
	fs.String(CorethConfigKey, DefaultString, "Specifies config to pass into coreth")
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
	// Timeouts
	fs.Duration(NetworkInitialTimeoutKey, 5*time.Second, "Initial timeout value of the adaptive timeout manager.")
	fs.Duration(NetworkMinimumTimeoutKey, 2*time.Second, "Minimum timeout value of the adaptive timeout manager.")
	fs.Duration(NetworkMaximumTimeoutKey, 10*time.Second, "Maximum timeout value of the adaptive timeout manager.")
	fs.Duration(NetworkTimeoutHalflifeKey, 5*time.Minute, "Halflife of average network response time. Higher value --> network timeout is less volatile. Can't be 0.")
	fs.Float64(NetworkTimeoutCoefficientKey, 2, "Multiplied by average network response time to get the network timeout. Must be >= 1.")
	fs.Uint(SendQueueSizeKey, 4096, "Max number of messages waiting to be sent to peers.")
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
	// Throughput Server (deprecated)
	fs.Uint(XputServerPortKey, 9652, "Port of the deprecated throughput test server")
	fs.Bool(XputServerEnabledKey, false, "If true, throughput test server is created")
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
	fs.Bool(P2pTLSEnabledKey, true, "Require TLS to authenticate network communication")
	fs.String(StakingKeyPathKey, DefaultString, "Path to the TLS private key for staking")
	fs.String(StakingCertPathKey, DefaultString, "Path to the TLS certificate for staking")
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
	fs.String(BootstrapIPsKey, DefaultString, "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	fs.String(BootstrapIDsKey, DefaultString, "Comma separated list of bootstrap peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	fs.Bool(RetryBootstrapKey, true, "Specifies whether bootstrap should be retried")
	fs.Int(RetryBootstrapMaxAttemptsKey, 50, "Specifies how many times bootstrap should be retried")

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
	fs.String(IpcsPathKey, DefaultString, "The directory (Unix) or named pipe name prefix (Windows) for IPC sockets")

	// Indexer
	fs.Bool(IndexEnabledKey, false, "If true, index all accepted containers and transactions and expose them via an API")
	fs.Bool(IndexAllowIncompleteKey, false, "If true, allow running the node in such a way that could cause an index to miss transactions. Ignored if index is disabled.")
	// Plugin
	fs.Bool(PluginModeKey, true, "Whether the app should run as a plugin. Defaults to true")

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
	if configFile := v.GetString(ConfigFileKey); configFile != DefaultString {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// getConfigFromViper sets attributes on [config] based on the values
// defined in the [viper] environment
func getConfigFromViper(v *viper.Viper) (node.Config, error) {
	config := node.Config{}

	config.FetchOnly = v.GetBool(FetchOnlyKey)

	// Consensus Parameters
	config.ConsensusParams.K = v.GetInt(SnowSampleSizeKey)
	config.ConsensusParams.Alpha = v.GetInt(SnowQuorumSizeKey)
	config.ConsensusParams.BetaVirtuous = v.GetInt(SnowVirtuousCommitThresholdKey)
	config.ConsensusParams.BetaRogue = v.GetInt(SnowRogueCommitThresholdKey)
	config.ConsensusParams.Parents = v.GetInt(SnowAvalancheNumParentsKey)
	config.ConsensusParams.BatchSize = v.GetInt(SnowAvalancheBatchSizeKey)
	config.ConsensusParams.ConcurrentRepolls = v.GetInt(SnowConcurrentRepollsKey)
	config.ConsensusParams.OptimalProcessing = v.GetInt(SnowOptimalProcessingKey)
	config.ConsensusParams.MaxOutstandingItems = v.GetInt(SnowMaxProcessingKey)
	config.ConsensusParams.MaxItemProcessingTime = v.GetDuration(SnowMaxTimeProcessingKey)
	config.ConsensusGossipFrequency = v.GetDuration(ConsensusGossipFrequencyKey)
	config.ConsensusShutdownTimeout = v.GetDuration(ConsensusShutdownTimeoutKey)

	// Logging:
	loggingconfig, err := logging.DefaultConfig()
	if err != nil {
		return node.Config{}, err
	}
	logsDir := v.GetString(LogsDirKey)
	if logsDir != "" {
		loggingconfig.Directory = logsDir
	}
	loggingconfig.LogLevel, err = logging.ToLevel(v.GetString(LogLevelKey))
	if err != nil {
		return node.Config{}, err
	}
	logDisplayLevel := v.GetString(LogDisplayLevelKey)
	if logDisplayLevel == "" {
		logDisplayLevel = v.GetString(LogLevelKey)
	}
	displayLevel, err := logging.ToLevel(logDisplayLevel)
	if err != nil {
		return node.Config{}, err
	}
	loggingconfig.DisplayLevel = displayLevel

	loggingconfig.DisplayHighlight, err = logging.ToHighlight(v.GetString(LogDisplayHighlightKey), os.Stdout.Fd())
	if err != nil {
		return node.Config{}, err
	}

	config.LoggingConfig = loggingconfig

	// NetworkID
	networkID, err := constants.NetworkID(v.GetString(NetworkNameKey))
	if err != nil {
		return node.Config{}, err
	}
	config.NetworkID = networkID

	// DB:
	config.DBEnabled = v.GetBool(DbEnabledKey)
	config.DBPath = os.ExpandEnv(v.GetString(DbPathKey))
	if config.DBPath == DefaultString {
		config.DBPath = defaultDbDir
	}
	config.DBPath = path.Join(config.DBPath, constants.NetworkName(config.NetworkID))

	// IP configuration
	// Resolves our public IP, or does nothing
	config.DynamicPublicIPResolver = dynamicip.NewResolver(v.GetString(DynamicPublicIPResolverKey))

	var ip net.IP
	publicIP := v.GetString(PublicIPKey)
	switch {
	case config.DynamicPublicIPResolver.IsResolver():
		// User specified to use dynamic IP resolution; don't use NAT traversal
		config.Nat = nat.NewNoRouter()
		ip, err = dynamicip.FetchExternalIP(config.DynamicPublicIPResolver)
		if err != nil {
			return node.Config{}, fmt.Errorf("dynamic ip address fetch failed: %s", err)
		}

	case publicIP == "":
		// User didn't specify a public IP to use; try with NAT traversal
		config.AttemptedNATTraversal = true
		config.Nat = nat.GetRouter()
		ip, err = config.Nat.ExternalIP()
		if err != nil {
			ip = net.IPv4zero // Couldn't get my IP...set to 0.0.0.0
		}
	default:
		// User specified a public IP to use; don't use NAT
		config.Nat = nat.NewNoRouter()
		ip = net.ParseIP(publicIP)
	}

	if ip == nil {
		return node.Config{}, fmt.Errorf("invalid IP Address %s", publicIP)
	}

	stakingPort := uint16(v.GetUint(StakingPortKey))

	config.StakingIP = utils.NewDynamicIPDesc(ip, stakingPort)

	config.DynamicUpdateDuration = v.GetDuration(DynamicUpdateDurationKey)
	config.ConnMeterResetDuration = v.GetDuration(ConnMeterResetDurationKey)
	config.ConnMeterMaxConns = v.GetInt(ConnMeterMaxConnsKey)

	// Staking:
	config.EnableStaking = v.GetBool(StakingEnabledKey)
	config.EnableP2PTLS = v.GetBool(P2pTLSEnabledKey)
	config.StakingKeyFile = v.GetString(StakingKeyPathKey)
	config.StakingCertFile = v.GetString(StakingCertPathKey)
	config.DisabledStakingWeight = v.GetUint64(StakingDisabledWeightKey)
	config.MinStakeDuration = v.GetDuration(MinStakeDurationKey)
	config.MaxStakeDuration = v.GetDuration(MaxStakeDurationKey)
	config.StakeMintingPeriod = v.GetDuration(StakeMintingPeriodKey)
	if config.EnableStaking && !config.EnableP2PTLS {
		return node.Config{}, errStakingRequiresTLS
	}
	if !config.EnableStaking && config.DisabledStakingWeight == 0 {
		return node.Config{}, errInvalidStakerWeights
	}

	stakingKeyPath := v.GetString(StakingKeyPathKey)
	if stakingKeyPath == DefaultString {
		config.StakingKeyFile = defaultStakingKeyPath
	} else {
		config.StakingKeyFile = stakingKeyPath
	}
	stakingCertPath := v.GetString(StakingCertPathKey)
	if stakingCertPath == DefaultString {
		config.StakingCertFile = defaultStakingCertPath
	} else {
		config.StakingCertFile = stakingCertPath
	}

	// parse any env variables
	config.StakingCertFile = os.ExpandEnv(config.StakingCertFile)
	config.StakingKeyFile = os.ExpandEnv(config.StakingKeyFile)
	switch {
	// If staking key/cert locations are specified but not found, error
	case config.StakingKeyFile != defaultStakingKeyPath || config.StakingCertFile != defaultStakingCertPath:
		if _, err := os.Stat(config.StakingKeyFile); os.IsNotExist(err) {
			return node.Config{}, fmt.Errorf("couldn't find staking key at %s", config.StakingKeyFile)
		} else if _, err := os.Stat(config.StakingCertFile); os.IsNotExist(err) {
			return node.Config{}, fmt.Errorf("couldn't find staking certificate at %s", config.StakingCertFile)
		}
	default:
		// Only creates staking key/cert if [stakingKeyPath] doesn't exist
		if err := staking.InitNodeStakingKeyPair(config.StakingKeyFile, config.StakingCertFile); err != nil {
			return node.Config{}, fmt.Errorf("couldn't generate staking key/cert: %w", err)
		}
	}

	// Parse staking certificate from file
	certBytes, err := ioutil.ReadFile(config.StakingCertFile)
	if err != nil {
		return node.Config{}, fmt.Errorf("problem reading staking certificate: %w", err)
	}
	config.StakingTLSCert, err = tls.LoadX509KeyPair(config.StakingCertFile, config.StakingKeyFile)
	if err != nil {
		return node.Config{}, err
	}
	block, _ := pem.Decode(certBytes)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return node.Config{}, fmt.Errorf("problem parsing staking certificate: %w", err)
	}
	nodeIDFromFile, err := ids.ToShortID(hashing.PubkeyBytesToAddress(x509Cert.Raw))
	if err != nil {
		return node.Config{}, fmt.Errorf("problem deriving node ID from certificate: %w", err)
	}
	switch {
	case config.FetchOnly:
		// If TLS is enabled and I'm in fetch only mode, my node ID is derived from a new, ephemeral staking key/cert
		keyBytes, certBytes, err := staking.GenerateStakingCert()
		if err != nil {
			return node.Config{}, fmt.Errorf("couldn't generate dummy staking key/cert: %s", err)
		}
		config.StakingTLSCert, err = tls.X509KeyPair(certBytes, keyBytes)
		if err != nil {
			return node.Config{}, fmt.Errorf("couldn't create TLS cert: %s", err)
		}
		block, _ := pem.Decode(certBytes)
		x509Cert, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			return node.Config{}, fmt.Errorf("problem parsing staking certificate: %w", err)
		}
		config.NodeID, err = ids.ToShortID(hashing.PubkeyBytesToAddress(x509Cert.Raw))
		if err != nil {
			return node.Config{}, fmt.Errorf("problem deriving node ID from certificate: %w", err)
		}
	case !config.EnableP2PTLS:
		// If TLS is disabled, my node ID is the hash of my IP
		config.NodeID = ids.ShortID(hashing.ComputeHash160Array([]byte(config.StakingIP.IP().String())))
	case !config.FetchOnly:
		// If TLS is enabled and I'm not in fetch only mode, my node ID is derived from my staking key/cert
		config.NodeID = nodeIDFromFile
	}

	if err := initBootstrapPeers(v, &config); err != nil {
		return node.Config{}, err
	}

	config.WhitelistedSubnets.Add(constants.PrimaryNetworkID)
	for _, subnet := range strings.Split(v.GetString(WhitelistedSubnetsKey), ",") {
		if subnet != "" {
			subnetID, err := ids.FromString(subnet)
			if err != nil {
				return node.Config{}, fmt.Errorf("couldn't parse subnetID %s: %w", subnet, err)
			}
			config.WhitelistedSubnets.Add(subnetID)
		}
	}

	// Build directory
	// The directory should have this structure:
	// build
	// |_avalanchego-latest
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	// |_avalanchego-preupgrade
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	buildDir := v.GetString(BuildDirKey)
	if buildDir == DefaultString {
		config.BuildDir = defaultBuildDirs[0]
	} else {
		config.BuildDir = buildDir
	}
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
	if !validBuildDir(config.BuildDir) {
		foundBuildDir := false
		for _, dir := range defaultBuildDirs {
			if validBuildDir(dir) {
				config.BuildDir = dir
				foundBuildDir = true
				break
			}
		}
		if !foundBuildDir {
			return node.Config{}, fmt.Errorf("couldn't find valid build directory in any of the default locations: %s", defaultBuildDirs)
		}
	}

	// Plugin directory. Defaults to [buildDirectory]/plugins
	pluginDir := v.GetString(PluginDirKey)
	if pluginDir == DefaultString {
		config.PluginDir = filepath.Join(config.BuildDir, avalanchegoLatest, "plugins")
	} else {
		config.PluginDir = pluginDir
	}

	// HTTP:
	config.HTTPHost = v.GetString(HTTPHostKey)
	config.HTTPPort = uint16(v.GetUint(HTTPPortKey))
	config.HTTPSEnabled = v.GetBool(HTTPSEnabledKey)
	config.HTTPSKeyFile = v.GetString(HTTPSKeyFileKey)
	config.HTTPSCertFile = v.GetString(HTTPSCertFileKey)
	config.APIAllowedOrigins = v.GetStringSlice(HTTPAllowedOrigins)

	// API Auth
	config.APIRequireAuthToken = v.GetBool(APIAuthRequiredKey)
	if config.APIRequireAuthToken {
		passwordFile := v.GetString(APIAuthPasswordFileKey)
		pwBytes, err := ioutil.ReadFile(passwordFile)
		if err != nil {
			return node.Config{}, fmt.Errorf("api-auth-password-file %q failed to be read with: %w", passwordFile, err)
		}
		config.APIAuthPassword = strings.TrimSpace(string(pwBytes))
		if !password.SufficientlyStrong(config.APIAuthPassword, password.OK) {
			return node.Config{}, errors.New("api-auth-password is not strong enough")
		}
	}

	// APIs
	config.AdminAPIEnabled = v.GetBool(AdminAPIEnabledKey)
	config.InfoAPIEnabled = v.GetBool(InfoAPIEnabledKey)
	config.KeystoreAPIEnabled = v.GetBool(KeystoreAPIEnabledKey)
	config.MetricsAPIEnabled = v.GetBool(MetricsAPIEnabledKey)
	config.HealthAPIEnabled = v.GetBool(HealthAPIEnabledKey)
	config.IPCAPIEnabled = v.GetBool(IpcAPIEnabledKey)
	config.IndexAPIEnabled = v.GetBool(IndexEnabledKey)

	// Throughput:
	config.ThroughputServerEnabled = v.GetBool(XputServerEnabledKey)
	config.ThroughputPort = uint16(v.GetUint(XputServerPortKey))

	// Halflife of continuous averager used in health checks
	healthCheckAveragerHalflife := v.GetDuration(HealthCheckAveragerHalflifeKey)
	if healthCheckAveragerHalflife <= 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", HealthCheckAveragerHalflifeKey)
	}

	// Router
	config.ConsensusRouter = &router.ChainRouter{}
	config.RouterHealthConfig.MaxDropRate = v.GetFloat64(RouterHealthMaxDropRateKey)
	config.RouterHealthConfig.MaxOutstandingRequests = int(v.GetUint(RouterHealthMaxOutstandingRequestsKey))
	config.RouterHealthConfig.MaxOutstandingDuration = v.GetDuration(NetworkHealthMaxOutstandingDurationKey)
	config.RouterHealthConfig.MaxRunTimeRequests = v.GetDuration(NetworkMaximumTimeoutKey)
	config.RouterHealthConfig.MaxDropRateHalflife = healthCheckAveragerHalflife
	switch {
	case config.RouterHealthConfig.MaxDropRate < 0 || config.RouterHealthConfig.MaxDropRate > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", RouterHealthMaxDropRateKey)
	case config.RouterHealthConfig.MaxOutstandingDuration <= 0:
		return node.Config{}, fmt.Errorf("%s must be positive", NetworkHealthMaxOutstandingDurationKey)
	}

	// IPCs
	ipcsChainIDs := v.GetString(IpcsChainIDsKey)
	if ipcsChainIDs != "" {
		config.IPCDefaultChainIDs = strings.Split(ipcsChainIDs, ",")
	}

	ipcsPath := v.GetString(IpcsPathKey)
	if ipcsPath == DefaultString {
		config.IPCPath = ipcs.DefaultBaseURL
	} else {
		config.IPCPath = ipcsPath
	}

	// Throttling
	config.MaxNonStakerPendingMsgs = v.GetUint32(MaxNonStakerPendingMsgsKey)
	config.StakerMSGPortion = v.GetFloat64(StakerMsgReservedKey)
	config.StakerCPUPortion = v.GetFloat64(StakerCPUReservedKey)
	config.SendQueueSize = v.GetUint32(SendQueueSizeKey)
	config.MaxPendingMsgs = v.GetUint32(MaxPendingMsgsKey)
	if config.MaxPendingMsgs < config.MaxNonStakerPendingMsgs {
		return node.Config{}, errors.New("maximum pending messages must be >= maximum non-staker pending messages")
	}

	// Health
	config.HealthCheckFreq = v.GetDuration(HealthCheckFreqKey)
	// Network Health Check
	config.NetworkHealthConfig.MaxTimeSinceMsgSent = v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey)
	config.NetworkHealthConfig.MaxTimeSinceMsgReceived = v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey)
	config.NetworkHealthConfig.MaxPortionSendQueueBytesFull = v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey)
	config.NetworkHealthConfig.MinConnectedPeers = v.GetUint(NetworkHealthMinPeersKey)
	config.NetworkHealthConfig.MaxSendFailRate = v.GetFloat64(NetworkHealthMaxSendFailRateKey)
	config.NetworkHealthConfig.MaxSendFailRateHalflife = healthCheckAveragerHalflife
	switch {
	case config.NetworkHealthConfig.MaxTimeSinceMsgSent < 0:
		return node.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case config.NetworkHealthConfig.MaxTimeSinceMsgReceived < 0:
		return node.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case config.NetworkHealthConfig.MaxSendFailRate < 0 || config.NetworkHealthConfig.MaxSendFailRate > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case config.NetworkHealthConfig.MaxPortionSendQueueBytesFull < 0 || config.NetworkHealthConfig.MaxPortionSendQueueBytesFull > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	}

	// Network Timeout
	config.NetworkConfig.InitialTimeout = v.GetDuration(NetworkInitialTimeoutKey)
	config.NetworkConfig.MinimumTimeout = v.GetDuration(NetworkMinimumTimeoutKey)
	config.NetworkConfig.MaximumTimeout = v.GetDuration(NetworkMaximumTimeoutKey)
	config.NetworkConfig.TimeoutHalflife = v.GetDuration(NetworkTimeoutHalflifeKey)
	config.NetworkConfig.TimeoutCoefficient = v.GetFloat64(NetworkTimeoutCoefficientKey)

	switch {
	case config.NetworkConfig.MinimumTimeout < 1:
		return node.Config{}, errors.New("minimum timeout must be positive")
	case config.NetworkConfig.MinimumTimeout > config.NetworkConfig.MaximumTimeout:
		return node.Config{}, errors.New("maximum timeout can't be less than minimum timeout")
	case config.NetworkConfig.InitialTimeout < config.NetworkConfig.MinimumTimeout ||
		config.NetworkConfig.InitialTimeout > config.NetworkConfig.MaximumTimeout:
		return node.Config{}, errors.New("initial timeout should be in the range [minimumTimeout, maximumTimeout]")
	case config.NetworkConfig.TimeoutHalflife <= 0:
		return node.Config{}, errors.New("network timeout halflife must be positive")
	case config.NetworkConfig.TimeoutCoefficient < 1:
		return node.Config{}, errors.New("network timeout coefficient must be >= 1")
	}

	// Benchlist
	config.BenchlistConfig.Threshold = v.GetInt(BenchlistFailThresholdKey)
	config.BenchlistConfig.PeerSummaryEnabled = v.GetBool(BenchlistPeerSummaryEnabledKey)
	config.BenchlistConfig.Duration = v.GetDuration(BenchlistDurationKey)
	config.BenchlistConfig.MinimumFailingDuration = v.GetDuration(BenchlistMinFailingDurationKey)
	config.BenchlistConfig.MaxPortion = (1.0 - (float64(config.ConsensusParams.Alpha) / float64(config.ConsensusParams.K))) / 3.0

	if config.ConsensusGossipFrequency < 0 {
		return node.Config{}, errors.New("gossip frequency can't be negative")
	}
	if config.ConsensusShutdownTimeout < 0 {
		return node.Config{}, errors.New("gossip frequency can't be negative")
	}

	// File Descriptor Limit
	fdLimit := v.GetUint64(FdLimitKey)
	if err := ulimit.Set(fdLimit); err != nil {
		return node.Config{}, fmt.Errorf("failed to set fd limit correctly due to: %w", err)
	}

	// Network Parameters
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		txFee := v.GetUint64(TxFeeKey)
		creationTxFee := v.GetUint64(CreationTxFeeKey)
		uptimeRequirement := v.GetFloat64(UptimeRequirementKey)
		config.TxFee = txFee
		config.CreationTxFee = creationTxFee
		config.UptimeRequirement = uptimeRequirement

		minValidatorStake := v.GetUint64(MinValidatorStakeKey)
		maxValidatorStake := v.GetUint64(MaxValidatorStakeKey)
		minDelegatorStake := v.GetUint64(MinDelegatorStakeKey)
		minDelegationFee := v.GetUint64(MinDelegatorFeeKey)
		if minValidatorStake > maxValidatorStake {
			return node.Config{}, errors.New("minimum validator stake can't be greater than maximum validator stake")
		}

		config.MinValidatorStake = minValidatorStake
		config.MaxValidatorStake = maxValidatorStake
		config.MinDelegatorStake = minDelegatorStake

		if minDelegationFee > 1000000 {
			return node.Config{}, errors.New("delegation fee must be in the range [0, 1000000]")
		}
		config.MinDelegationFee = uint32(minDelegationFee)

		if config.MinStakeDuration == 0 {
			return node.Config{}, errors.New("min stake duration can't be zero")
		}
		if config.MaxStakeDuration < config.MinStakeDuration {
			return node.Config{}, errors.New("max stake duration can't be less than min stake duration")
		}
		if config.StakeMintingPeriod < config.MaxStakeDuration {
			return node.Config{}, errors.New("stake minting period can't be less than max stake duration")
		}

		config.EpochFirstTransition = time.Unix(v.GetInt64(SnowEpochFirstTransition), 0)
		config.EpochDuration = v.GetDuration(SnowEpochDuration)
	} else {
		config.Params = *genesis.GetParams(networkID)
	}

	// Load genesis data
	config.GenesisBytes, config.AvaxAssetID, err = genesis.Genesis(networkID, v.GetString(GenesisConfigFileKey))
	if err != nil {
		return node.Config{}, fmt.Errorf("unable to load genesis file: %w", err)
	}

	// Assertions
	config.EnableAssertions = v.GetBool(AssertionsEnabledKey)

	// Crypto
	config.EnableCrypto = v.GetBool(SignatureVerificationEnabledKey)

	// Coreth Plugin
	corethConfigString := v.GetString(CorethConfigKey)
	if corethConfigString != DefaultString {
		corethConfigValue := v.Get(CorethConfigKey)
		switch value := corethConfigValue.(type) {
		case string:
			corethConfigString = value
		default:
			corethConfigBytes, err := json.Marshal(value)
			if err != nil {
				return node.Config{}, fmt.Errorf("couldn't parse coreth config: %w", err)
			}
			corethConfigString = string(corethConfigBytes)
		}
	}
	config.CorethConfig = corethConfigString

	// Indexer
	config.IndexAllowIncomplete = v.GetBool(IndexAllowIncompleteKey)

	// Bootstrap Configs
	config.RetryBootstrap = v.GetBool(RetryBootstrapKey)
	config.RetryBootstrapMaxAttempts = v.GetInt(RetryBootstrapMaxAttemptsKey)

	// Peer alias
	config.PeerAliasTimeout = v.GetDuration(PeerAliasTimeoutKey)

	// Plugin config
	config.PluginMode = v.GetBool(PluginModeKey)

	return config, nil
}

// Initialize config.BootstrapPeers.
func initBootstrapPeers(v *viper.Viper, config *node.Config) error {
	bootstrapIPs := v.GetString(BootstrapIPsKey)
	bootstrapIDs := v.GetString(BootstrapIDsKey)

	defaultBootstrapIPs, defaultBootstrapIDs := genesis.SampleBeacons(config.NetworkID, 5)
	if bootstrapIPs == DefaultString { // If no IPs specified, use default bootstrap node IPs
		bootstrapIPs = strings.Join(defaultBootstrapIPs, ",")
	}
	if bootstrapIDs == DefaultString { // If no node IDs given, use node IDs of nodes in [bootstrapIPs]
		if bootstrapIPs == "" { // If user specified no bootstrap IPs, default to no bootstrap IDs either
			bootstrapIDs = ""
		} else {
			bootstrapIDs = strings.Join(defaultBootstrapIDs, ",")
		}
	}

	for _, ip := range strings.Split(bootstrapIPs, ",") {
		if ip == "" {
			continue
		}
		addr, err := utils.ToIPDesc(ip)
		if err != nil {
			return fmt.Errorf("couldn't parse bootstrap ip %s: %w", ip, err)
		}
		config.BootstrapPeers = append(config.BootstrapPeers, &node.Peer{
			IP: addr,
		})
	}

	if config.EnableP2PTLS {
		i := 0
		for _, id := range strings.Split(bootstrapIDs, ",") {
			if id == "" {
				continue
			}
			nodeID, err := ids.ShortFromPrefixedString(id, constants.NodeIDPrefix)
			if err != nil {
				return fmt.Errorf("couldn't parse bootstrap peer id: %w", err)
			}
			if len(config.BootstrapPeers) <= i {
				return errBootstrapMismatch
			}
			config.BootstrapPeers[i].ID = nodeID
			i++
		}
		if len(config.BootstrapPeers) != i {
			return fmt.Errorf("got %d bootstrap IPs but %d bootstrap IDs", len(config.BootstrapPeers), i)
		}
	} else {
		for _, peer := range config.BootstrapPeers {
			peer.ID = ids.ShortID(hashing.ComputeHash160Array([]byte(peer.IP.String())))
		}
	}
	return nil
}

func GetConfig(commit string) (node.Config, string, bool, error) {
	v, err := getViper()
	if err != nil {
		return node.Config{}, "", false, err
	}

	if v.GetBool(VersionKey) {
		format := "%s ["
		args := []interface{}{
			versionconfig.NodeVersion,
		}

		networkID, err := constants.NetworkID(v.GetString(NetworkNameKey))
		if err != nil {
			return node.Config{}, "", false, err
		}
		networkGeneration := constants.NetworkName(networkID)
		if networkID == constants.MainnetID {
			format += "network=%s"
		} else {
			format += "network=testnet/%s"
		}
		args = append(args, networkGeneration)

		format += ", database=%s"
		args = append(args, versionconfig.CurrentDBVersion)

		if commit != "" {
			format += ", commit=%s"
			args = append(args, commit)
		}

		format += "]\n"

		return node.Config{}, fmt.Sprintf(format, args...), true, nil
	}
	config, err := getConfigFromViper(v)
	return config, "", false, err
}
