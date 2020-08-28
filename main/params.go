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

	"github.com/ava-labs/gecko/database/leveldb"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/ipcs"
	"github.com/ava-labs/gecko/nat"
	"github.com/ava-labs/gecko/node"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/staking"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/sampler"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	dbVersion = "v0.6.1"
)

// Results of parsing the CLI
var (
	Config             = node.Config{}
	Err                error
	defaultNetworkName = constants.TestnetName

	homeDir                = os.ExpandEnv("$HOME")
	defaultDbDir           = filepath.Join(homeDir, ".gecko", "db")
	defaultStakingKeyPath  = filepath.Join(homeDir, ".gecko", "staking", "staker.key")
	defaultStakingCertPath = filepath.Join(homeDir, ".gecko", "staking", "staker.crt")
	defaultPluginDirs      = []string{
		filepath.Join(".", "build", "plugins"),
		filepath.Join(".", "plugins"),
		filepath.Join("/", "usr", "local", "lib", "gecko"),
		filepath.Join(homeDir, ".gecko", "plugins"),
	}
)

var (
	errBootstrapMismatch    = errors.New("more bootstrap IDs provided than bootstrap IPs")
	errStakingRequiresTLS   = errors.New("if staking is enabled, network TLS must also be enabled")
	errInvalidStakerWeights = errors.New("staking weights must be positive")
)

// GetIPs returns the default IPs for each network
func GetIPs(networkID uint32) []string {
	switch networkID {
	case constants.EverestID:
		return []string{
			"18.188.121.35:21001",
			"3.133.83.66:21001",
			"3.15.206.239:21001",
			"18.224.140.156:21001",
			"3.133.131.39:21001",
			"18.191.29.54:21001",
			"18.224.172.110:21001",
			"18.223.211.203:21001",
			"18.216.130.143:21001",
			"18.223.184.147:21001",
			"52.15.48.84:21001",
			"18.189.194.220:21001",
			"18.223.119.104:21001",
			"3.133.155.41:21001",
			"13.58.170.174:21001",
			"3.21.245.246:21001",
			"52.15.190.149:21001",
			"18.188.95.241:21001",
			"3.12.197.248:21001",
			"3.17.39.236:21001",
		}
	default:
		return nil
	}
}

// GetIDs returns the default IDs for each network
func GetIDs(networkID uint32) []string {
	switch networkID {
	case constants.EverestID:
		return []string{
			"NodeID-NpagUxt6KQiwPch9Sd4osv8kD1TZnkjdk",
			"NodeID-2m38qc95mhHXtrhjyGbe7r2NhniqHHJRB",
			"NodeID-LQwRLm4cbJ7T2kxcxp4uXCU5XD8DFrE1C",
			"NodeID-hArafGhY2HFTbwaaVh1CSCUCUCiJ2Vfb",
			"NodeID-4QBwET5o8kUhvt9xArhir4d3R25CtmZho",
			"NodeID-HGZ8ae74J3odT8ESreAdCtdnvWG1J4X5n",
			"NodeID-4KXitMCoE9p2BHA6VzXtaTxLoEjNDo2Pt",
			"NodeID-JyE4P8f4cTryNV8DCz2M81bMtGhFFHexG",
			"NodeID-EzGaipqomyK9UKx9DBHV6Ky3y68hoknrF",
			"NodeID-CYKruAjwH1BmV3m37sXNuprbr7dGQuJwG",
			"NodeID-LegbVf6qaMKcsXPnLStkdc1JVktmmiDxy",
			"NodeID-FesGqwKq7z5nPFHa5iwZctHE5EZV9Lpdq",
			"NodeID-BFa1padLXBj7VHa2JYvYGzcTBPQGjPhUy",
			"NodeID-4B4rc5vdD1758JSBYL1xyvE5NHGzz6xzH",
			"NodeID-EDESh4DfZFC15i613pMtWniQ9arbBZRnL",
			"NodeID-CZmZ9xpCzkWqjAyS7L4htzh5Lg6kf1k18",
			"NodeID-CTtkcXvVdhpNp6f97LEUXPwsRD3A2ZHqP",
			"NodeID-84KbQHSDnojroCVY7vQ7u9Tx7pUonPaS",
			"NodeID-JjvzhxnLHLUQ5HjVRkvG827ivbLXPwA9u",
			"NodeID-4CWTbdvgXHY1CLXqQNAp22nJDo5nAmts6",
		}
	default:
		return nil
	}
}

// GetDefaultBootstraps returns the default bootstraps this node should connect
// to
func GetDefaultBootstraps(networkID uint32, count int) ([]string, []string) {
	ips := GetIPs(networkID)
	ids := GetIDs(networkID)

	if numIPs := len(ips); numIPs < count {
		count = numIPs
	}

	sampledIPs := make([]string, 0, count)
	sampledIDs := make([]string, 0, count)

	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(ips)))
	indices, _ := s.Sample(count)
	for _, index := range indices {
		sampledIPs = append(sampledIPs, ips[int(index)])
		sampledIDs = append(sampledIDs, ids[int(index)])
	}

	return sampledIPs, sampledIDs
}

// Parse the CLI arguments
func init() {
	errs := &wrappers.Errs{}
	defer func() { Err = errs.Err }()

	loggingConfig, err := logging.DefaultConfig()
	if errs.Add(err); errs.Errored() {
		return
	}

	fs := flag.NewFlagSet("gecko", flag.ContinueOnError)

	// If this is true, print the version and quit.
	version := fs.Bool("version", false, "If true, print version and quit")

	// NetworkID:
	networkName := fs.String("network-id", defaultNetworkName, "Network ID this node will connect to")

	// AVAX fees:
	fs.Uint64Var(&Config.TxFee, "tx-fee", units.MilliAvax, "Transaction fee, in nAVAX")

	// Minimum stake, in nAVAX, required to validate the primary network
	fs.Uint64Var(&Config.MinStake, "min-stake", 5*units.MilliAvax, "Minimum stake, in nAVAX, required to validate the primary network")

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
	fs.BoolVar(&Config.EnableHTTPS, "http-tls-enabled", false, "Upgrade the HTTP server to HTTPs")
	fs.StringVar(&Config.HTTPSKeyFile, "http-tls-key-file", "", "TLS private key file for the HTTPs server")
	fs.StringVar(&Config.HTTPSCertFile, "http-tls-cert-file", "", "TLS certificate file for the HTTPs server")

	// Bootstrapping:
	bootstrapIPs := fs.String("bootstrap-ips", "default", "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	bootstrapIDs := fs.String("bootstrap-ids", "default", "Comma separated list of bootstrap peer ids to connect to. Example: JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	// Staking:
	consensusPort := fs.Uint("staking-port", 9651, "Port of the consensus server")
	fs.BoolVar(&Config.EnableStaking, "staking-enabled", true, "Enable staking. If enabled, Network TLS is required.")
	fs.BoolVar(&Config.EnableP2PTLS, "p2p-tls-enabled", true, "Require TLS to authenticate network communication")
	fs.StringVar(&Config.StakingKeyFile, "staking-tls-key-file", defaultStakingKeyPath, "TLS private key for staking")
	fs.StringVar(&Config.StakingCertFile, "staking-tls-cert-file", defaultStakingCertPath, "TLS certificate for staking")
	fs.Uint64Var(&Config.DisabledStakingWeight, "staking-disabled-weight", 1, "Weight to provide to each peer when staking is disabled")

	// Throttling:
	fs.Float64Var(&Config.StakerMsgPortion, "staker-msg-reserved", 0.2, "Reserve a portion of the chain message queue's space for stakers.")
	fs.Float64Var(&Config.StakerCPUPortion, "staker-cpu-reserved", 0.2, "Reserve a portion of the chain's CPU time for stakers.")

	// Plugins:
	fs.StringVar(&Config.PluginDir, "plugin-dir", defaultPluginDirs[0], "Plugin directory for Avalanche VMs")

	// Logging:
	logsDir := fs.String("log-dir", "", "Logging directory for Avalanche")
	logLevel := fs.String("log-level", "info", "The log level. Should be one of {verbo, debug, info, warn, error, fatal, off}")
	logDisplayLevel := fs.String("log-display-level", "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, info, warn, error, fatal, off}")
	logDisplayHighlight := fs.String("log-display-highlight", "auto", "Whether to color/highlight display logs. Default highlights when the output is a terminal. Otherwise, should be one of {auto, plain, colors}")

	fs.IntVar(&Config.ConsensusParams.K, "snow-sample-size", 5, "Number of nodes to query for each network poll")
	fs.IntVar(&Config.ConsensusParams.Alpha, "snow-quorum-size", 4, "Alpha value to use for required number positive results")
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

	ferr := fs.Parse(os.Args[1:])

	if *version { // If --version used, print version and exit
		networkID, err := genesis.NetworkID(defaultNetworkName)
		if errs.Add(err); err != nil {
			return
		}
		networkGeneration := genesis.NetworkName(networkID)
		fmt.Printf(
			"%s [database=%s, network=%s/%s]\n",
			node.Version, dbVersion, defaultNetworkName, networkGeneration,
		)
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

	networkID, err := genesis.NetworkID(*networkName)
	if errs.Add(err); err != nil {
		return
	}

	Config.NetworkID = networkID

	// DB:
	if *db {
		*dbDir = os.ExpandEnv(*dbDir) // parse any env variables
		dbPath := path.Join(*dbDir, genesis.NetworkName(Config.NetworkID), dbVersion)
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

	defaultBootstrapIPs, defaultBootstrapIDs := GetDefaultBootstraps(networkID, 5)

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
}
