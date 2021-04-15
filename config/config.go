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
	"github.com/ava-labs/avalanchego/version"
	"github.com/kardianos/osext"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	PrevDBVersion = version.NewDefaultVersion(1, 0, 0)
	DBVersion     = version.NewDefaultVersion(1, 1, 0)
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
	defaultBuildDirs       = []string{
		filepath.Join(".", "build"),
		filepath.Join("/", "usr", "local", "lib", constants.AppName, "build"),
		filepath.Join(defaultDataDir, "build"),
	}
	// GitCommit should be optionally set at compile time.
	GitCommit string
)

func init() {
	folderPath, err := osext.ExecutableFolder()
	if err == nil {
		defaultBuildDirs = append(defaultBuildDirs, filepath.Join(folderPath, "build"))
	}
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
	fs.Bool(versionKey, false, "If true, print version and quit")

	// Fetch only mode
	fs.Bool(FetchOnlyKey, false, "If true, bootstrap the current database version then stop")

	// System
	fs.Uint64(fdLimitKey, ulimit.DefaultFDLimit, "Attempts to raise the process file descriptor limit to at least this value.")

	// config
	fs.String(configFileKey, defaultString, "Specifies a config file")
	// Genesis config File
	fs.String(genesisConfigFileKey, "", "Specifies a genesis config file (ignored when running standard networks)")
	// Plugins
	fs.String(pluginDirKey, defaultString, "Plugin directory for Avalanche VMs")
	// Network ID
	fs.String(networkNameKey, defaultNetworkName, "Network ID this node will connect to")
	// AVAX fees
	fs.Uint64(txFeeKey, units.MilliAvax, "Transaction fee, in nAVAX")
	fs.Uint64(creationTxFeeKey, units.MilliAvax, "Transaction fee, in nAVAX, for transactions that create new state")
	// Database
	fs.Bool(dbEnabledKey, true, "Turn on persistent storage")
	fs.String(dbPathKey, defaultDbDir, "Path to database directory")
	// Coreth config
	fs.String(corethConfigKey, defaultString, "Specifies config to pass into coreth")
	// Logging
	fs.String(logsDirKey, "", "Logging directory for Avalanche")
	fs.String(logLevelKey, "info", "The log level. Should be one of {verbo, debug, info, warn, error, fatal, off}")
	fs.String(logDisplayLevelKey, "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, info, warn, error, fatal, off}")
	fs.String(logDisplayHighlightKey, "auto", "Whether to color/highlight display logs. Default highlights when the output is a terminal. Otherwise, should be one of {auto, plain, colors}")
	// Assertions
	fs.Bool(assertionsEnabledKey, true, "Turn on assertion execution")
	// Signature Verification
	fs.Bool(signatureVerificationEnabledKey, true, "Turn on signature verification")

	// Networking
	// Public IP Resolution
	fs.String(publicIPKey, "", "Public IP of this node for P2P communication. If empty, try to discover with NAT. Ignored if dynamic-public-ip is non-empty.")
	fs.Duration(dynamicUpdateDurationKey, 5*time.Minute, "Dynamic IP and NAT Traversal update duration")
	fs.String(dynamicPublicIPResolverKey, "", "'ifconfigco' (alias 'ifconfig') or 'opendns' or 'ifconfigme'. By default does not do dynamic public IP updates. If non-empty, ignores public-ip argument.")
	// Incoming Connection Throttling
	// After we receive [conn-meter-max-conns] incoming connections from a given IP
	// in the last [conn-meter-reset-duration], we close all subsequent incoming connections
	// from the IP before upgrade.
	fs.Duration(connMeterResetDurationKey, 0*time.Second,
		"Upgrade at most [conn-meter-max-conns] connections from a given IP per [conn-meter-reset-duration]. "+
			"If [conn-meter-reset-duration] is 0, incoming connections are not rate-limited.")
	fs.Int(connMeterMaxConnsKey, 5,
		"Upgrade at most [conn-meter-max-conns] connections from a given IP per [conn-meter-reset-duration]. "+
			"If [conn-meter-reset-duration] is 0, incoming connections are not rate-limited.")
	// Timeouts
	fs.Duration(networkInitialTimeoutKey, 5*time.Second, "Initial timeout value of the adaptive timeout manager.")
	fs.Duration(networkMinimumTimeoutKey, 2*time.Second, "Minimum timeout value of the adaptive timeout manager.")
	fs.Duration(networkMaximumTimeoutKey, 10*time.Second, "Maximum timeout value of the adaptive timeout manager.")
	fs.Duration(networkTimeoutHalflifeKey, 5*time.Minute, "Halflife of average network response time. Higher value --> network timeout is less volatile. Can't be 0.")
	fs.Float64(networkTimeoutCoefficientKey, 2, "Multiplied by average network response time to get the network timeout. Must be >= 1.")
	fs.Uint(sendQueueSizeKey, 4096, "Max number of messages waiting to be sent to peers.")
	// Peer alias configuration
	fs.Duration(peerAliasTimeoutKey, 10*time.Minute, "How often the node will attempt to connect "+
		"to an IP address previously associated with a peer (i.e. a peer alias).")
	// Benchlist
	fs.Int(benchlistFailThresholdKey, 10, "Number of consecutive failed queries before benchlisting a node.")
	fs.Bool(benchlistPeerSummaryEnabledKey, false, "Enables peer specific query latency metrics.")
	fs.Duration(benchlistDurationKey, 30*time.Minute, "Max amount of time a peer is benchlisted after surpassing the threshold.")
	fs.Duration(benchlistMinFailingDurationKey, 5*time.Minute, "Minimum amount of time messages to a peer must be failing before the peer is benched.")
	// Router
	fs.Uint(maxNonStakerPendingMsgsKey, uint(router.DefaultMaxNonStakerPendingMsgs), "Maximum number of messages a non-staker is allowed to have pending.")
	fs.Float64(stakerMsgReservedKey, router.DefaultStakerPortion, "Reserve a portion of the chain message queue's space for stakers.")
	fs.Float64(stakerCPUReservedKey, router.DefaultStakerPortion, "Reserve a portion of the chain's CPU time for stakers.")
	fs.Uint(maxPendingMsgsKey, 4096, "Maximum number of pending messages. Messages after this will be dropped.")
	fs.Duration(consensusGossipFrequencyKey, 10*time.Second, "Frequency of gossiping accepted frontiers.")
	fs.Duration(consensusShutdownTimeoutKey, 5*time.Second, "Timeout before killing an unresponsive chain.")

	// HTTP API
	fs.String(httpHostKey, "127.0.0.1", "Address of the HTTP server")
	fs.Uint(HTTPPortKey, 9650, "Port of the HTTP server")
	fs.Bool(httpsEnabledKey, false, "Upgrade the HTTP server to HTTPs")
	fs.String(httpsKeyFileKey, "", "TLS private key file for the HTTPs server")
	fs.String(httpsCertFileKey, "", "TLS certificate file for the HTTPs server")
	fs.String(httpAllowedOrigins, "*", "Origins to allow on the HTTP port. Defaults to * which allows all origins. Example: https://*.avax.network https://*.avax-test.network")
	fs.Bool(apiAuthRequiredKey, false, "Require authorization token to call HTTP APIs")
	fs.String(apiAuthPasswordFileKey, "", "Password file used to initially create/validate API authorization tokens. Leading and trailing whitespace is removed from the password. Can be changed via API call.")
	// Enable/Disable APIs
	fs.Bool(adminAPIEnabledKey, false, "If true, this node exposes the Admin API")
	fs.Bool(infoAPIEnabledKey, true, "If true, this node exposes the Info API")
	fs.Bool(keystoreAPIEnabledKey, true, "If true, this node exposes the Keystore API")
	fs.Bool(metricsAPIEnabledKey, true, "If true, this node exposes the Metrics API")
	fs.Bool(healthAPIEnabledKey, true, "If true, this node exposes the Health API")
	fs.Bool(ipcAPIEnabledKey, false, "If true, IPCs can be opened")
	// Throughput Server (deprecated)
	fs.Uint(xputServerPortKey, 9652, "Port of the deprecated throughput test server")
	fs.Bool(xputServerEnabledKey, false, "If true, throughput test server is created")
	// Health
	fs.Duration(healthCheckFreqKey, 30*time.Second, "Time between health checks")
	fs.Duration(healthCheckAveragerHalflifeKey, 10*time.Second, "Halflife of averager when calculating a running average in a health check")
	// Network Layer Health
	fs.Duration(networkHealthMaxTimeSinceMsgSentKey, time.Minute, "Network layer returns unhealthy if haven't sent a message for at least this much time")
	fs.Duration(networkHealthMaxTimeSinceMsgReceivedKey, time.Minute, "Network layer returns unhealthy if haven't received a message for at least this much time")
	fs.Float64(networkHealthMaxPortionSendQueueFillKey, 0.9, "Network layer returns unhealthy if more than this portion of the pending send queue is full")
	fs.Uint(networkHealthMinPeersKey, 1, "Network layer returns unhealthy if connected to less than this many peers")
	fs.Float64(networkHealthMaxSendFailRateKey, .9, "Network layer reports unhealthy if more than this portion of attempted message sends fail")
	// Router Health
	fs.Float64(routerHealthMaxDropRateKey, 1, "Node reports unhealthy if the router drops more than this portion of messages.")
	fs.Uint(routerHealthMaxOutstandingRequestsKey, 1024, "Node reports unhealthy if there are more than this many outstanding consensus requests (Get, PullQuery, etc.) over all chains")
	fs.Duration(networkHealthMaxOutstandingDurationKey, 5*time.Minute, "Node reports unhealthy if there has been a request outstanding for this duration")

	// Staking
	fs.Uint(StakingPortKey, 9651, "Port of the consensus server")
	fs.Bool(stakingEnabledKey, true, "Enable staking. If enabled, Network TLS is required.")
	fs.Bool(p2pTLSEnabledKey, true, "Require TLS to authenticate network communication")
	fs.String(stakingKeyPathKey, defaultString, "Path to the TLS private key for staking")
	fs.String(stakingCertPathKey, defaultString, "Path to the TLS certificate for staking")
	fs.Uint64(stakingDisabledWeightKey, 1, "Weight to provide to each peer when staking is disabled")
	// Uptime Requirement
	fs.Float64(uptimeRequirementKey, .6, "Fraction of time a validator must be online to receive rewards")
	// Minimum Stake required to validate the Primary Network
	fs.Uint64(minValidatorStakeKey, 2*units.KiloAvax, "Minimum stake, in nAVAX, required to validate the primary network")
	// Maximum Stake that can be staked and delegated to a validator on the Primary Network
	fs.Uint64(maxValidatorStakeKey, 3*units.MegaAvax, "Maximum stake, in nAVAX, that can be placed on a validator on the primary network")
	// Minimum Stake that can be delegated on the Primary Network
	fs.Uint64(minDelegatorStakeKey, 25*units.Avax, "Minimum stake, in nAVAX, that can be delegated on the primary network")
	fs.Uint64(minDelegatorFeeKey, 20000, "Minimum delegation fee, in the range [0, 1000000], that can be charged for delegation on the primary network")
	// Minimum Stake Duration
	fs.Duration(minStakeDurationKey, 24*time.Hour, "Minimum staking duration")
	// Maximum Stake Duration
	fs.Duration(maxStakeDurationKey, 365*24*time.Hour, "Maximum staking duration")
	// Stake Minting Period
	fs.Duration(stakeMintingPeriodKey, 365*24*time.Hour, "Consumption period of the staking function")
	// Subnets
	fs.String(whitelistedSubnetsKey, "", "Whitelist of subnets to validate.")
	// Bootstrapping
	fs.String(BootstrapIPsKey, defaultString, "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	fs.String(BootstrapIDsKey, defaultString, "Comma separated list of bootstrap peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	fs.Bool(retryBootstrap, true, "Specifies whether bootstrap should be retried")
	fs.Int(retryBootstrapMaxAttempts, 50, "Specifies how many times bootstrap should be retried")

	// Consensus
	fs.Int(snowSampleSizeKey, 20, "Number of nodes to query for each network poll")
	fs.Int(snowQuorumSizeKey, 14, "Alpha value to use for required number positive results")
	fs.Int(snowVirtuousCommitThresholdKey, 15, "Beta value to use for virtuous transactions")
	fs.Int(snowRogueCommitThresholdKey, 20, "Beta value to use for rogue transactions")
	fs.Int(snowAvalancheNumParentsKey, 5, "Number of vertexes for reference from each new vertex")
	fs.Int(snowAvalancheBatchSizeKey, 30, "Number of operations to batch in each new vertex")
	fs.Int(snowConcurrentRepollsKey, 4, "Minimum number of concurrent polls for finalizing consensus")
	fs.Int(snowOptimalProcessingKey, 50, "Optimal number of processing vertices in consensus")
	fs.Int(snowMaxProcessingKey, 1024, "Maximum number of processing items to be considered healthy")
	fs.Duration(snowMaxTimeProcessingKey, 2*time.Minute, "Maximum amount of time an item should be processing and still be healthy")
	fs.Int64(snowEpochFirstTransition, 1607626800, "Unix timestamp of the first epoch transaction, in seconds. Defaults to 12/10/2020 @ 7:00pm (UTC)")
	fs.Duration(snowEpochDuration, 6*time.Hour, "Duration of each epoch")

	// IPC
	fs.String(ipcsChainIDsKey, "", "Comma separated list of chain ids to add to the IPC engine. Example: 11111111111111111111111111111111LpoYY,4R5p2RXDGLqaifZE4hHWH9owe34pfoBULn1DrQTWivjg8o4aH")
	fs.String(ipcsPathKey, defaultString, "The directory (Unix) or named pipe name prefix (Windows) for IPC sockets")

	// Plugin
	fs.Bool(PluginModeKey, true, "Whether the app should run as a plugin. Defaults to true")

	return fs
}

// getViper returns the viper environment from parsing config file from default search paths
// and any parsed command line flags
func GetViper() (*viper.Viper, error) {
	v := viper.New()

	fs := avalancheFlagSet()
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		return nil, err
	}

	if configFile := v.GetString(configFileKey); configFile != defaultString {
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
	config.ConsensusParams.K = v.GetInt(snowSampleSizeKey)
	config.ConsensusParams.Alpha = v.GetInt(snowQuorumSizeKey)
	config.ConsensusParams.BetaVirtuous = v.GetInt(snowVirtuousCommitThresholdKey)
	config.ConsensusParams.BetaRogue = v.GetInt(snowRogueCommitThresholdKey)
	config.ConsensusParams.Parents = v.GetInt(snowAvalancheNumParentsKey)
	config.ConsensusParams.BatchSize = v.GetInt(snowAvalancheBatchSizeKey)
	config.ConsensusParams.ConcurrentRepolls = v.GetInt(snowConcurrentRepollsKey)
	config.ConsensusParams.OptimalProcessing = v.GetInt(snowOptimalProcessingKey)
	config.ConsensusParams.MaxOutstandingItems = v.GetInt(snowMaxProcessingKey)
	config.ConsensusParams.MaxItemProcessingTime = v.GetDuration(snowMaxTimeProcessingKey)
	config.ConsensusGossipFrequency = v.GetDuration(consensusGossipFrequencyKey)
	config.ConsensusShutdownTimeout = v.GetDuration(consensusShutdownTimeoutKey)

	// Logging:
	loggingconfig, err := logging.DefaultConfig()
	if err != nil {
		return node.Config{}, err
	}
	logsDir := v.GetString(logsDirKey)
	if logsDir != "" {
		loggingconfig.Directory = logsDir
	}
	loggingconfig.LogLevel, err = logging.ToLevel(v.GetString(logLevelKey))
	if err != nil {
		return node.Config{}, err
	}
	logDisplayLevel := v.GetString(logDisplayLevelKey)
	if logDisplayLevel == "" {
		logDisplayLevel = v.GetString(logLevelKey)
	}
	displayLevel, err := logging.ToLevel(logDisplayLevel)
	if err != nil {
		return node.Config{}, err
	}
	loggingconfig.DisplayLevel = displayLevel

	loggingconfig.DisplayHighlight, err = logging.ToHighlight(v.GetString(logDisplayHighlightKey), os.Stdout.Fd())
	if err != nil {
		return node.Config{}, err
	}

	config.LoggingConfig = loggingconfig

	// NetworkID
	networkID, err := constants.NetworkID(v.GetString(networkNameKey))
	if err != nil {
		return node.Config{}, err
	}
	config.NetworkID = networkID

	// DB:
	config.DBEnabled = v.GetBool(dbEnabledKey)
	config.DBPath = os.ExpandEnv(v.GetString(dbPathKey))
	if config.DBPath == defaultString {
		config.DBPath = defaultDbDir
	}
	config.DBPath = path.Join(config.DBPath, constants.NetworkName(config.NetworkID))

	// IP configuration
	// Resolves our public IP, or does nothing
	config.DynamicPublicIPResolver = dynamicip.NewResolver(v.GetString(dynamicPublicIPResolverKey))

	var ip net.IP
	publicIP := v.GetString(publicIPKey)
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

	config.StakingIP = utils.NewDynamicIPDesc(ip, uint16(v.GetUint(StakingPortKey)))

	config.DynamicUpdateDuration = v.GetDuration(dynamicUpdateDurationKey)
	config.ConnMeterResetDuration = v.GetDuration(connMeterResetDurationKey)
	config.ConnMeterMaxConns = v.GetInt(connMeterMaxConnsKey)

	// Staking:
	config.EnableStaking = v.GetBool(stakingEnabledKey)
	config.EnableP2PTLS = v.GetBool(p2pTLSEnabledKey)
	config.StakingKeyFile = v.GetString(stakingKeyPathKey)
	config.StakingCertFile = v.GetString(stakingCertPathKey)
	config.DisabledStakingWeight = v.GetUint64(stakingDisabledWeightKey)
	config.MinStakeDuration = v.GetDuration(minStakeDurationKey)
	config.MaxStakeDuration = v.GetDuration(maxStakeDurationKey)
	config.StakeMintingPeriod = v.GetDuration(stakeMintingPeriodKey)
	if config.EnableStaking && !config.EnableP2PTLS {
		return node.Config{}, errStakingRequiresTLS
	}
	if !config.EnableStaking && config.DisabledStakingWeight == 0 {
		return node.Config{}, errInvalidStakerWeights
	}

	stakingKeyPath := v.GetString(stakingKeyPathKey)
	if stakingKeyPath == defaultString {
		config.StakingKeyFile = defaultStakingKeyPath
	} else {
		config.StakingKeyFile = stakingKeyPath
	}
	stakingCertPath := v.GetString(stakingCertPathKey)
	if stakingCertPath == defaultString {
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
	for _, subnet := range strings.Split(v.GetString(whitelistedSubnetsKey), ",") {
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
	// |_avalanchego-[currentVersion]
	//   |_avalanchego-inner
	//   |_plugins
	//     |_evm
	// |_avalanchego-[previousVersion]
	//   |_avalanchego-inner
	//   |_plugins
	//     |_evm
	// where [currentVersion] and [previousVersion] are, for example, v1.3.2 and v.1.3.1, resepectively
	buildDir := v.GetString(buildDirKey)
	if buildDir == defaultString {
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
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s%s%s", constants.AppName, "-v", node.Version.AsVersion()))); err != nil {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s%s%s", constants.AppName, "-v", node.PreviousVersion.AsVersion()))); err != nil {
			return false
		}
		return true
	}
	if !validBuildDir(config.BuildDir) {
		for _, dir := range defaultBuildDirs {
			if validBuildDir(dir) {
				config.BuildDir = dir
				break
			}
		}
	}

	// Plugin directory
	pluginDir := v.GetString(pluginDirKey)
	if pluginDir == defaultString {
		config.PluginDir = filepath.Join(config.BuildDir, fmt.Sprintf("%s%s%s", constants.AppName, "-v", node.Version.AsVersion()), "plugins") // TODO fix this. It always uses the current node version right now.
	} else {
		config.PluginDir = pluginDir
	}

	// HTTP:
	config.HTTPHost = v.GetString(httpHostKey)
	config.HTTPPort = uint16(v.GetUint(HTTPPortKey))

	config.HTTPSEnabled = v.GetBool(httpsEnabledKey)
	config.HTTPSKeyFile = v.GetString(httpsKeyFileKey)
	config.HTTPSCertFile = v.GetString(httpsCertFileKey)
	config.APIAllowedOrigins = v.GetStringSlice(httpAllowedOrigins)

	// API Auth
	config.APIRequireAuthToken = v.GetBool(apiAuthRequiredKey)
	if config.APIRequireAuthToken {
		passwordFile := v.GetString(apiAuthPasswordFileKey)
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
	config.AdminAPIEnabled = v.GetBool(adminAPIEnabledKey)
	config.InfoAPIEnabled = v.GetBool(infoAPIEnabledKey)
	config.KeystoreAPIEnabled = v.GetBool(keystoreAPIEnabledKey)
	config.MetricsAPIEnabled = v.GetBool(metricsAPIEnabledKey)
	config.HealthAPIEnabled = v.GetBool(healthAPIEnabledKey)
	config.IPCAPIEnabled = v.GetBool(ipcAPIEnabledKey)

	// Throughput:
	config.ThroughputServerEnabled = v.GetBool(xputServerEnabledKey)
	config.ThroughputPort = uint16(v.GetUint(xputServerPortKey))

	// Halflife of continuous averager used in health checks
	healthCheckAveragerHalflife := v.GetDuration(healthCheckAveragerHalflifeKey)
	if healthCheckAveragerHalflife <= 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", healthCheckAveragerHalflifeKey)
	}

	// Router
	config.ConsensusRouter = &router.ChainRouter{}
	config.RouterHealthConfig.MaxDropRate = v.GetFloat64(routerHealthMaxDropRateKey)
	config.RouterHealthConfig.MaxOutstandingRequests = int(v.GetUint(routerHealthMaxOutstandingRequestsKey))
	config.RouterHealthConfig.MaxOutstandingDuration = v.GetDuration(networkHealthMaxOutstandingDurationKey)
	config.RouterHealthConfig.MaxRunTimeRequests = v.GetDuration(networkMaximumTimeoutKey)
	config.RouterHealthConfig.MaxDropRateHalflife = healthCheckAveragerHalflife
	switch {
	case config.RouterHealthConfig.MaxDropRate < 0 || config.RouterHealthConfig.MaxDropRate > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", routerHealthMaxDropRateKey)
	case config.RouterHealthConfig.MaxOutstandingDuration <= 0:
		return node.Config{}, fmt.Errorf("%s must be positive", networkHealthMaxOutstandingDurationKey)
	}

	// IPCs
	ipcsChainIDs := v.GetString(ipcsChainIDsKey)
	if ipcsChainIDs != "" {
		config.IPCDefaultChainIDs = strings.Split(ipcsChainIDs, ",")
	}

	ipcsPath := v.GetString(ipcsPathKey)
	if ipcsPath == defaultString {
		config.IPCPath = ipcs.DefaultBaseURL
	} else {
		config.IPCPath = ipcsPath
	}

	// Throttling
	config.MaxNonStakerPendingMsgs = v.GetUint32(maxNonStakerPendingMsgsKey)
	config.StakerMSGPortion = v.GetFloat64(stakerMsgReservedKey)
	config.StakerCPUPortion = v.GetFloat64(stakerCPUReservedKey)
	config.SendQueueSize = v.GetUint32(sendQueueSizeKey)
	config.MaxPendingMsgs = v.GetUint32(maxPendingMsgsKey)
	if config.MaxPendingMsgs < config.MaxNonStakerPendingMsgs {
		return node.Config{}, errors.New("maximum pending messages must be >= maximum non-staker pending messages")
	}

	// Health
	config.HealthCheckFreq = v.GetDuration(healthCheckFreqKey)
	// Network Health Check
	config.NetworkHealthConfig.MaxTimeSinceMsgSent = v.GetDuration(networkHealthMaxTimeSinceMsgSentKey)
	config.NetworkHealthConfig.MaxTimeSinceMsgReceived = v.GetDuration(networkHealthMaxTimeSinceMsgReceivedKey)
	config.NetworkHealthConfig.MaxPortionSendQueueBytesFull = v.GetFloat64(networkHealthMaxPortionSendQueueFillKey)
	config.NetworkHealthConfig.MinConnectedPeers = v.GetUint(networkHealthMinPeersKey)
	config.NetworkHealthConfig.MaxSendFailRate = v.GetFloat64(networkHealthMaxSendFailRateKey)
	config.NetworkHealthConfig.MaxSendFailRateHalflife = healthCheckAveragerHalflife
	switch {
	case config.NetworkHealthConfig.MaxTimeSinceMsgSent < 0:
		return node.Config{}, fmt.Errorf("%s must be > 0", networkHealthMaxTimeSinceMsgSentKey)
	case config.NetworkHealthConfig.MaxTimeSinceMsgReceived < 0:
		return node.Config{}, fmt.Errorf("%s must be > 0", networkHealthMaxTimeSinceMsgReceivedKey)
	case config.NetworkHealthConfig.MaxSendFailRate < 0 || config.NetworkHealthConfig.MaxSendFailRate > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", networkHealthMaxSendFailRateKey)
	case config.NetworkHealthConfig.MaxPortionSendQueueBytesFull < 0 || config.NetworkHealthConfig.MaxPortionSendQueueBytesFull > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", networkHealthMaxPortionSendQueueFillKey)
	}

	// Network Timeout
	config.NetworkConfig.InitialTimeout = v.GetDuration(networkInitialTimeoutKey)
	config.NetworkConfig.MinimumTimeout = v.GetDuration(networkMinimumTimeoutKey)
	config.NetworkConfig.MaximumTimeout = v.GetDuration(networkMaximumTimeoutKey)
	config.NetworkConfig.TimeoutHalflife = v.GetDuration(networkTimeoutHalflifeKey)
	config.NetworkConfig.TimeoutCoefficient = v.GetFloat64(networkTimeoutCoefficientKey)

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
	config.BenchlistConfig.Threshold = v.GetInt(benchlistFailThresholdKey)
	config.BenchlistConfig.PeerSummaryEnabled = v.GetBool(benchlistPeerSummaryEnabledKey)
	config.BenchlistConfig.Duration = v.GetDuration(benchlistDurationKey)
	config.BenchlistConfig.MinimumFailingDuration = v.GetDuration(benchlistMinFailingDurationKey)
	config.BenchlistConfig.MaxPortion = (1.0 - (float64(config.ConsensusParams.Alpha) / float64(config.ConsensusParams.K))) / 3.0

	if config.ConsensusGossipFrequency < 0 {
		return node.Config{}, errors.New("gossip frequency can't be negative")
	}
	if config.ConsensusShutdownTimeout < 0 {
		return node.Config{}, errors.New("gossip frequency can't be negative")
	}

	// File Descriptor Limit
	fdLimit := v.GetUint64(fdLimitKey)
	if err := ulimit.Set(fdLimit); err != nil {
		return node.Config{}, fmt.Errorf("failed to set fd limit correctly due to: %w", err)
	}

	// Network Parameters
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		txFee := v.GetUint64(txFeeKey)
		creationTxFee := v.GetUint64(creationTxFeeKey)
		uptimeRequirement := v.GetFloat64(uptimeRequirementKey)
		config.TxFee = txFee
		config.CreationTxFee = creationTxFee
		config.UptimeRequirement = uptimeRequirement

		minValidatorStake := v.GetUint64(minValidatorStakeKey)
		maxValidatorStake := v.GetUint64(maxValidatorStakeKey)
		minDelegatorStake := v.GetUint64(minDelegatorStakeKey)
		minDelegationFee := v.GetUint64(minDelegatorFeeKey)
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

		config.EpochFirstTransition = time.Unix(v.GetInt64(snowEpochFirstTransition), 0)
		config.EpochDuration = v.GetDuration(snowEpochDuration)
	} else {
		config.Params = *genesis.GetParams(networkID)
	}

	// Load genesis data
	config.GenesisBytes, config.AvaxAssetID, err = genesis.Genesis(networkID, v.GetString(genesisConfigFileKey))
	if err != nil {
		return node.Config{}, fmt.Errorf("unable to load genesis file: %w", err)
	}

	// Assertions
	config.EnableAssertions = v.GetBool(assertionsEnabledKey)

	// Crypto
	config.EnableCrypto = v.GetBool(signatureVerificationEnabledKey)

	// Coreth Plugin
	corethConfigString := v.GetString(corethConfigKey)
	if corethConfigString != defaultString {
		corethConfigValue := v.Get(corethConfigKey)
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

	// Bootstrap configs
	config.RetryBootstrap = v.GetBool(retryBootstrap)
	config.RetryBootstrapMaxAttempts = v.GetInt(retryBootstrapMaxAttempts)

	// Peer alias
	config.PeerAliasTimeout = v.GetDuration(peerAliasTimeoutKey)

	// Plugin config
	config.PluginMode = v.GetBool(PluginModeKey)

	return config, nil
}

// Initialize config.BootstrapPeers.
func initBootstrapPeers(v *viper.Viper, config *node.Config) error {
	bootstrapIPs := v.GetString(BootstrapIPsKey)
	bootstrapIDs := v.GetString(BootstrapIDsKey)

	defaultBootstrapIPs, defaultBootstrapIDs := genesis.SampleBeacons(config.NetworkID, 5)
	if bootstrapIPs == defaultString { // If no IPs specified, use default bootstrap node IPs
		bootstrapIPs = strings.Join(defaultBootstrapIPs, ",")
	}
	if bootstrapIDs == defaultString { // If no node IDs given, use node IDs of nodes in [bootstrapIPs]
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

func GetConfig() (node.Config, error) {
	v, err := GetViper()
	if err != nil {
		return node.Config{}, err
	}

	if v.GetBool(versionKey) {
		format := "%s ["
		args := []interface{}{
			node.Version,
		}

		networkID, err := constants.NetworkID(v.GetString(networkNameKey))
		if err != nil {
			return node.Config{}, err
		}
		networkGeneration := constants.NetworkName(networkID)
		if networkID == constants.MainnetID {
			format += "network=%s"
		} else {
			format += "network=testnet/%s"
		}
		args = append(args, networkGeneration)

		format += ", database=%s"
		args = append(args, DBVersion)

		if GitCommit != "" {
			format += ", commit=%s"
			args = append(args, GitCommit)
		}

		format += "]\n"

		fmt.Printf(format, args...)
		os.Exit(0)
	}

	return getConfigFromViper(v)
}
