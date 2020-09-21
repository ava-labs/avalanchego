// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	dbVersion = "v1.0.0"
)

// Results of parsing the CLI
var (
	Config             = node.Config{}
	Err                error
	defaultNetworkName = constants.MainnetName

	homeDir                = os.ExpandEnv("$HOME")
	dataDirName            = fmt.Sprintf(".%s", constants.AppName)
	defaultDbDir           = filepath.Join(homeDir, dataDirName, "db")
	defaultStakingKeyPath  = filepath.Join(homeDir, dataDirName, "staking", "staker.key")
	defaultStakingCertPath = filepath.Join(homeDir, dataDirName, "staking", "staker.crt")
	defaultPluginDirs      = []string{
		filepath.Join(".", "build", "plugins"),
		filepath.Join(".", "plugins"),
		filepath.Join("/", "usr", "local", "lib", constants.AppName),
		filepath.Join(homeDir, dataDirName, "plugins"),
	}
)

var (
	errBootstrapMismatch    = errors.New("more bootstrap IDs provided than bootstrap IPs")
	errStakingRequiresTLS   = errors.New("if staking is enabled, network TLS must also be enabled")
	errInvalidStakerWeights = errors.New("staking weights must be positive")
)

// Parse the CLI arguments
func init() {
	errs := &wrappers.Errs{}
	defer func() { Err = errs.Err }()

	loggingConfig, err := logging.DefaultConfig()
	if errs.Add(err); errs.Errored() {
		return
	}

	fs := flag.NewFlagSet(constants.AppName, flag.ContinueOnError)

	// If this is true, print the version and quit.
	version := fs.Bool("version", false, "If true, print version and quit")

	// NetworkID:
	networkName := fs.String("network-id", defaultNetworkName, "Network ID this node will connect to")

	// AVAX fees:
	txFee := fs.Uint64("tx-fee", units.MilliAvax, "Transaction fee, in nAVAX")
	creationTxFee := fs.Uint64("creation-tx-fee", units.MilliAvax, "Transaction fee, in nAVAX, for transactions that create new state")

	// Uptime requirement:
	uptimeRequirement := fs.Float64("uptime-requirement", .6, "Fraction of time a validator must be online to receive rewards")

	// Minimum stake, in nAVAX, required to validate the primary network
	minValidatorStake := fs.Uint64("min-validator-stake", 2*units.KiloAvax, "Minimum stake, in nAVAX, required to validate the primary network")

	// Maximum stake amount, in nAVAX, that can be staked and delegated to a
	// validator on the primary network
	maxValidatorStake := fs.Uint64("max-validator-stake", 3*units.MegaAvax, "Maximum stake, in nAVAX, that can be placed on a validator on the primary network")

	// Minimum stake, in nAVAX, that can be delegated on the primary network
	minDelegatorStake := fs.Uint64("min-delegator-stake", 25*units.Avax, "Minimum stake, in nAVAX, that can be delegated on the primary network")

	minDelegationFee := fs.Uint64("min-delegation-fee", 20000, "Minimum delegation fee, in the range [0, 1000000], that can be charged for delegation on the primary network")

	// Minimum staking duration in nanoseconds
	minStakeDuration := fs.Uint64("min-stake-duration", uint64(24*time.Hour/time.Second), "Minimum staking duration, in seconds")

	// Maximum staking duration in nanoseconds
	maxStakeDuration := fs.Uint64("max-stake-duration", uint64(365*24*time.Hour/time.Second), "Maximum staking duration, in seconds")

	// Stake minting period in nanoseconds
	stakeMintingPeriod := fs.Uint64("stake-minting-period", uint64(365*24*time.Hour/time.Second), "Consumption period of the staking function, in seconds")

	// Assertions:
	fs.BoolVar(&loggingConfig.Assertions, "assertions-enabled", true, "Turn on assertion execution")

	// Crypto:
	fs.BoolVar(&Config.EnableCrypto, "signature-verification-enabled", true, "Turn on signature verification")

	// Database:
	db := fs.Bool("db-enabled", true, "Turn on persistent storage")
	dbDir := fs.String("db-dir", defaultDbDir, "Database directory for Avalanche state")

	// IP:
	consensusIP := fs.String("public-ip", "", "Public IP of this node")

	// HTTP Server:
	httpHost := fs.String("http-host", "127.0.0.1", "Address of the HTTP server")
	httpPort := fs.Uint("http-port", 9650, "Port of the HTTP server")
	fs.BoolVar(&Config.HTTPSEnabled, "http-tls-enabled", false, "Upgrade the HTTP server to HTTPs")
	fs.StringVar(&Config.HTTPSKeyFile, "http-tls-key-file", "", "TLS private key file for the HTTPs server")
	fs.StringVar(&Config.HTTPSCertFile, "http-tls-cert-file", "", "TLS certificate file for the HTTPs server")
	fs.BoolVar(&Config.APIRequireAuthToken, "api-auth-required", false, "Require authorization token to call HTTP APIs")
	fs.StringVar(&Config.APIAuthPassword, "api-auth-password", "", "Password used to create/validate API authorization tokens. Can be changed via API call.")

	// Bootstrapping:
	bootstrapIPs := fs.String("bootstrap-ips", "default", "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	bootstrapIDs := fs.String("bootstrap-ids", "default", "Comma separated list of bootstrap peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	// Staking:
	consensusPort := fs.Uint("staking-port", 9651, "Port of the consensus server")
	fs.BoolVar(&Config.EnableStaking, "staking-enabled", true, "Enable staking. If enabled, Network TLS is required.")
	fs.BoolVar(&Config.EnableP2PTLS, "p2p-tls-enabled", true, "Require TLS to authenticate network communication")
	fs.StringVar(&Config.StakingKeyFile, "staking-tls-key-file", defaultStakingKeyPath, "TLS private key for staking")
	fs.StringVar(&Config.StakingCertFile, "staking-tls-cert-file", defaultStakingCertPath, "TLS certificate for staking")
	fs.Uint64Var(&Config.DisabledStakingWeight, "staking-disabled-weight", 1, "Weight to provide to each peer when staking is disabled")

	// Throttling:
	fs.UintVar(&Config.MaxNonStakerPendingMsgs, "max-non-staker-pending-msgs", 3, "Maximum number of messages a non-staker is allowed to have pending.")
	fs.Float64Var(&Config.StakerMSGPortion, "staker-msg-reserved", 0.2, "Reserve a portion of the chain message queue's space for stakers.")
	fs.Float64Var(&Config.StakerCPUPortion, "staker-cpu-reserved", 0.2, "Reserve a portion of the chain's CPU time for stakers.")

	// Network Timeouts:
	networkInitialTimeout := fs.Int64("network-initial-timeout", int64(10*time.Second), "Initial timeout value of the adaptive timeout manager, in nanoseconds.")
	networkMinimumTimeout := fs.Int64("network-minimum-timeout", int64(500*time.Millisecond), "Minimum timeout value of the adaptive timeout manager, in nanoseconds.")
	networkMaximumTimeout := fs.Int64("network-maximum-timeout", int64(10*time.Second), "Maximum timeout value of the adaptive timeout manager, in nanoseconds.")
	fs.Float64Var(&Config.NetworkConfig.TimeoutMultiplier, "network-timeout-multiplier", 1.1, "Multiplier of the timeout after a failed request.")
	networkTimeoutReduction := fs.Int64("network-timeout-reduction", int64(time.Millisecond), "Reduction of the timeout after a successful request, in nanoseconds.")

	// Plugins:
	fs.StringVar(&Config.PluginDir, "plugin-dir", defaultPluginDirs[0], "Plugin directory for Avalanche VMs")

	// Logging:
	logsDir := fs.String("log-dir", "", "Logging directory for Avalanche")
	logLevel := fs.String("log-level", "info", "The log level. Should be one of {verbo, debug, info, warn, error, fatal, off}")
	logDisplayLevel := fs.String("log-display-level", "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, info, warn, error, fatal, off}")
	logDisplayHighlight := fs.String("log-display-highlight", "auto", "Whether to color/highlight display logs. Default highlights when the output is a terminal. Otherwise, should be one of {auto, plain, colors}")

	fs.IntVar(&Config.ConsensusParams.K, "snow-sample-size", 20, "Number of nodes to query for each network poll")
	fs.IntVar(&Config.ConsensusParams.Alpha, "snow-quorum-size", 16, "Alpha value to use for required number positive results")
	fs.IntVar(&Config.ConsensusParams.BetaVirtuous, "snow-virtuous-commit-threshold", 20, "Beta value to use for virtuous transactions")
	fs.IntVar(&Config.ConsensusParams.BetaRogue, "snow-rogue-commit-threshold", 30, "Beta value to use for rogue transactions")
	fs.IntVar(&Config.ConsensusParams.Parents, "snow-avalanche-num-parents", 5, "Number of vertexes for reference from each new vertex")
	fs.IntVar(&Config.ConsensusParams.BatchSize, "snow-avalanche-batch-size", 30, "Number of operations to batch in each new vertex")
	fs.IntVar(&Config.ConsensusParams.ConcurrentRepolls, "snow-concurrent-repolls", 1, "Minimum number of concurrent polls for finalizing consensus")

	// Enable/Disable APIs:
	fs.BoolVar(&Config.AdminAPIEnabled, "api-admin-enabled", false, "If true, this node exposes the Admin API")
	fs.BoolVar(&Config.InfoAPIEnabled, "api-info-enabled", true, "If true, this node exposes the Info API")
	fs.BoolVar(&Config.KeystoreAPIEnabled, "api-keystore-enabled", true, "If true, this node exposes the Keystore API")
	fs.BoolVar(&Config.MetricsAPIEnabled, "api-metrics-enabled", true, "If true, this node exposes the Metrics API")
	fs.BoolVar(&Config.HealthAPIEnabled, "api-health-enabled", true, "If true, this node exposes the Health API")
	fs.BoolVar(&Config.IPCAPIEnabled, "api-ipcs-enabled", false, "If true, IPCs can be opened")

	// Throughput Server
	throughputPort := fs.Uint("xput-server-port", 9652, "Port of the deprecated throughput test server")
	fs.BoolVar(&Config.ThroughputServerEnabled, "xput-server-enabled", false, "If true, throughput test server is created")

	// IPC
	ipcsChainIDs := fs.String("ipcs-chain-ids", "", "Comma separated list of chain ids to add to the IPC engine. Example: 11111111111111111111111111111111LpoYY,4R5p2RXDGLqaifZE4hHWH9owe34pfoBULn1DrQTWivjg8o4aH")
	fs.StringVar(&Config.IPCPath, "ipcs-path", ipcs.DefaultBaseURL, "The directory (Unix) or named pipe name prefix (Windows) for IPC sockets")

	// Router Configuration:
	consensusGossipFrequency := fs.Int64("consensus-gossip-frequency", int64(10*time.Second), "Frequency of gossiping accepted frontiers.")
	consensusShutdownTimeout := fs.Int64("consensus-shutdown-timeout", int64(1*time.Second), "Timeout before killing an unresponsive chain.")

	ferr := fs.Parse(os.Args[1:])

	if *version { // If --version used, print version and exit
		networkID, err := constants.NetworkID(*networkName)
		if errs.Add(err); err != nil {
			return
		}
		networkGeneration := constants.NetworkName(networkID)
		if networkID == constants.MainnetID {
			fmt.Printf("%s [database=%s, network=%s]\n",
				node.Version, dbVersion, networkGeneration)
		} else {
			fmt.Printf("%s [database=%s, network=testnet/%s]\n",
				node.Version, dbVersion, networkGeneration)
		}
		os.Exit(0)
	}

	if ferr == flag.ErrHelp {
		// display usage/help text and exit successfully
		os.Exit(0)
	}

	if ferr != nil {
		// other type of error occurred when parsing args
		os.Exit(2)
	}

	networkID, err := constants.NetworkID(*networkName)
	if errs.Add(err); err != nil {
		return
	}

	Config.NetworkID = networkID

	// DB:
	if *db {
		*dbDir = os.ExpandEnv(*dbDir) // parse any env variables
		dbPath := path.Join(*dbDir, constants.NetworkName(Config.NetworkID), dbVersion)
		db, err := leveldb.New(dbPath, 0, 0, 0)
		if err != nil {
			errs.Add(fmt.Errorf("couldn't create db at %s: %w", dbPath, err))
			return
		}
		Config.DB = db
	} else {
		Config.DB = memdb.New()
	}

	var ip net.IP
	// If public IP is not specified, get it using shell command dig
	if *consensusIP == "" {
		Config.Nat = nat.GetRouter()
		ip, err = Config.Nat.ExternalIP()
		if err != nil {
			ip = net.IPv4zero // Couldn't get my IP...set to 0.0.0.0
		}
	} else {
		Config.Nat = nat.NewNoRouter()
		ip = net.ParseIP(*consensusIP)
	}

	if ip == nil {
		errs.Add(fmt.Errorf("invalid IP Address %s", *consensusIP))
		return
	}

	Config.StakingIP = utils.IPDesc{
		IP:   ip,
		Port: uint16(*consensusPort),
	}
	Config.StakingLocalPort = uint16(*consensusPort)

	defaultBootstrapIPs, defaultBootstrapIDs := genesis.SampleBeacons(networkID, 5)

	// Bootstrapping:
	if *bootstrapIPs == "default" {
		*bootstrapIPs = strings.Join(defaultBootstrapIPs, ",")
	}
	for _, ip := range strings.Split(*bootstrapIPs, ",") {
		if ip != "" {
			addr, err := utils.ToIPDesc(ip)
			if err != nil {
				errs.Add(fmt.Errorf("couldn't parse ip: %w", err))
				return
			}
			Config.BootstrapPeers = append(Config.BootstrapPeers, &node.Peer{
				IP: addr,
			})
		}
	}

	if *bootstrapIDs == "default" {
		if *bootstrapIPs == "" {
			*bootstrapIDs = ""
		} else {
			*bootstrapIDs = strings.Join(defaultBootstrapIDs, ",")
		}
	}

	if Config.EnableStaking && !Config.EnableP2PTLS {
		errs.Add(errStakingRequiresTLS)
		return
	}

	if !Config.EnableStaking && Config.DisabledStakingWeight == 0 {
		errs.Add(errInvalidStakerWeights)
	}

	if Config.EnableP2PTLS {
		i := 0
		for _, id := range strings.Split(*bootstrapIDs, ",") {
			if id != "" {
				peerID, err := ids.ShortFromPrefixedString(id, constants.NodeIDPrefix)
				if err != nil {
					errs.Add(fmt.Errorf("couldn't parse bootstrap peer id: %w", err))
					return
				}
				if len(Config.BootstrapPeers) <= i {
					errs.Add(errBootstrapMismatch)
					return
				}
				Config.BootstrapPeers[i].ID = peerID
				i++
			}
		}
		if len(Config.BootstrapPeers) != i {
			errs.Add(fmt.Errorf("more bootstrap IPs, %d, provided than bootstrap IDs, %d", len(Config.BootstrapPeers), i))
			return
		}
	} else {
		for _, peer := range Config.BootstrapPeers {
			peer.ID = ids.NewShortID(hashing.ComputeHash160Array([]byte(peer.IP.String())))
		}
	}

	// Plugins
	if _, err := os.Stat(Config.PluginDir); os.IsNotExist(err) {
		for _, dir := range defaultPluginDirs {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				Config.PluginDir = dir
				break
			}
		}
	}

	// Staking
	Config.StakingCertFile = os.ExpandEnv(Config.StakingCertFile) // parse any env variable
	Config.StakingKeyFile = os.ExpandEnv(Config.StakingKeyFile)
	switch {
	// If staking key/cert locations are specified but not found, error
	case Config.StakingKeyFile != defaultStakingKeyPath || Config.StakingCertFile != defaultStakingCertPath:
		if _, err := os.Stat(Config.StakingKeyFile); os.IsNotExist(err) {
			errs.Add(fmt.Errorf("couldn't find staking key at %s", Config.StakingKeyFile))
			return
		} else if _, err := os.Stat(Config.StakingCertFile); os.IsNotExist(err) {
			errs.Add(fmt.Errorf("couldn't find staking certificate at %s", Config.StakingCertFile))
			return
		}
	default:
		// Only creates staking key/cert if [stakingKeyPath] doesn't exist
		if err := staking.GenerateStakingKeyCert(Config.StakingKeyFile, Config.StakingCertFile); err != nil {
			errs.Add(fmt.Errorf("couldn't generate staking key/cert: %w", err))
			return
		}
	}

	// HTTP:
	Config.HTTPHost = *httpHost
	Config.HTTPPort = uint16(*httpPort)
	if Config.APIRequireAuthToken {
		if Config.APIAuthPassword == "" {
			errs.Add(errors.New("api-auth-password must be provided if api-auth-required is true"))
			return
		}
		if !password.SufficientlyStrong(Config.APIAuthPassword, password.OK) {
			errs.Add(errors.New("api-auth-password is not strong enough. Add more characters"))
			return
		}
	}

	// Logging:
	if *logsDir != "" {
		loggingConfig.Directory = *logsDir
	}
	logFileLevel, err := logging.ToLevel(*logLevel)
	if errs.Add(err); err != nil {
		return
	}
	loggingConfig.LogLevel = logFileLevel

	if *logDisplayLevel == "" {
		*logDisplayLevel = *logLevel
	}
	displayLevel, err := logging.ToLevel(*logDisplayLevel)
	if errs.Add(err); err != nil {
		return
	}
	loggingConfig.DisplayLevel = displayLevel

	displayHighlight, err := logging.ToHighlight(*logDisplayHighlight, os.Stdout.Fd())
	if errs.Add(err); err != nil {
		return
	}
	loggingConfig.DisplayHighlight = displayHighlight

	Config.LoggingConfig = loggingConfig

	// Throughput:
	Config.ThroughputPort = uint16(*throughputPort)

	// Router used for consensus
	Config.ConsensusRouter = &router.ChainRouter{}

	// IPCs
	if *ipcsChainIDs != "" {
		Config.IPCDefaultChainIDs = strings.Split(*ipcsChainIDs, ",")
	}

	if *networkMinimumTimeout < 1 {
		errs.Add(errors.New("minimum timeout must be positive"))
	}
	if *networkMinimumTimeout > *networkMaximumTimeout {
		errs.Add(errors.New("maximum timeout can't be less than minimum timeout"))
	}
	if *networkInitialTimeout < *networkMinimumTimeout ||
		*networkInitialTimeout > *networkMaximumTimeout {
		errs.Add(errors.New("initial timeout should be in the range [minimumTimeout, maximumTimeout]"))
	}
	if *networkTimeoutReduction < 0 {
		errs.Add(errors.New("timeout reduction can't be negative"))
	}
	Config.NetworkConfig.InitialTimeout = time.Duration(*networkInitialTimeout)
	Config.NetworkConfig.MinimumTimeout = time.Duration(*networkMinimumTimeout)
	Config.NetworkConfig.MaximumTimeout = time.Duration(*networkMaximumTimeout)
	Config.NetworkConfig.TimeoutReduction = time.Duration(*networkTimeoutReduction)

	if *consensusGossipFrequency < 0 {
		errs.Add(errors.New("gossip frequency can't be negative"))
	}
	if *consensusShutdownTimeout < 0 {
		errs.Add(errors.New("gossip frequency can't be negative"))
	}
	Config.ConsensusGossipFrequency = time.Duration(*consensusGossipFrequency)
	Config.ConsensusShutdownTimeout = time.Duration(*consensusShutdownTimeout)

	if networkID != constants.MainnetID && networkID != constants.FujiID {
		Config.TxFee = *txFee
		Config.CreationTxFee = *creationTxFee
		Config.UptimeRequirement = *uptimeRequirement
		Config.UptimeRequirement = *uptimeRequirement

		if *minValidatorStake > *maxValidatorStake {
			errs.Add(errors.New("minimum validator stake can't be greater than maximum validator stake"))
		}

		Config.MinValidatorStake = *minValidatorStake
		Config.MaxValidatorStake = *maxValidatorStake
		Config.MinDelegatorStake = *minDelegatorStake

		if *minDelegationFee > 1000000 {
			errs.Add(errors.New("delegation fee must be in the range [0, 1000000]"))
		}
		Config.MinDelegationFee = uint32(*minDelegationFee)

		if *minStakeDuration == 0 {
			errs.Add(errors.New("min stake duration can't be zero"))
		}
		if *maxStakeDuration < *minStakeDuration {
			errs.Add(errors.New("max stake duration can't be less than min stake duration"))
		}
		if *stakeMintingPeriod < *maxStakeDuration {
			errs.Add(errors.New("stake minting period can't be less than max stake duration"))
		}

		Config.MinStakeDuration = time.Duration(*minStakeDuration) * time.Second
		Config.MaxStakeDuration = time.Duration(*maxStakeDuration) * time.Second
		Config.StakeMintingPeriod = time.Duration(*stakeMintingPeriod) * time.Second
	} else {
		Config.Params = *genesis.GetParams(networkID)
	}
}
