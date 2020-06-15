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
	"github.com/ava-labs/gecko/nat"
	"github.com/ava-labs/gecko/node"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/staking"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/random"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	dbVersion = "v0.5.0"
)

// Results of parsing the CLI
var (
	Config                 = node.Config{}
	Err                    error
	defaultNetworkName     = genesis.TestnetName
	defaultDbDir           = os.ExpandEnv(filepath.Join("$HOME", ".gecko", "db"))
	defaultStakingKeyPath  = os.ExpandEnv(filepath.Join("$HOME", ".gecko", "staking", "staker.key"))
	defaultStakingCertPath = os.ExpandEnv(filepath.Join("$HOME", ".gecko", "staking", "staker.crt"))

	defaultPluginDirs = []string{
		"./build/plugins",
		"./plugins",
		os.ExpandEnv(filepath.Join("$HOME", ".gecko", "plugins")),
	}
)

var (
	errBootstrapMismatch  = errors.New("more bootstrap IDs provided than bootstrap IPs")
	errStakingRequiresTLS = errors.New("if staking is enabled, network TLS must also be enabled")
)

// GetIPs returns the default IPs for each network
func GetIPs(networkID uint32) []string {
	switch networkID {
	case genesis.DenaliID:
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
	case genesis.CascadeID:
		return []string{
			"3.227.207.132:21001",
			"34.207.133.167:21001",
			"54.162.71.9:21001",
			"54.197.215.186:21001",
			"18.234.153.22:21001",
		}
	default:
		return nil
	}
}

// GetIDs returns the default IDs for each network
func GetIDs(networkID uint32) []string {
	switch networkID {
	case genesis.DenaliID:
		return []string{
			"NpagUxt6KQiwPch9Sd4osv8kD1TZnkjdk",
			"2m38qc95mhHXtrhjyGbe7r2NhniqHHJRB",
			"LQwRLm4cbJ7T2kxcxp4uXCU5XD8DFrE1C",
			"hArafGhY2HFTbwaaVh1CSCUCUCiJ2Vfb",
			"4QBwET5o8kUhvt9xArhir4d3R25CtmZho",
			"HGZ8ae74J3odT8ESreAdCtdnvWG1J4X5n",
			"4KXitMCoE9p2BHA6VzXtaTxLoEjNDo2Pt",
			"JyE4P8f4cTryNV8DCz2M81bMtGhFFHexG",
			"EzGaipqomyK9UKx9DBHV6Ky3y68hoknrF",
			"CYKruAjwH1BmV3m37sXNuprbr7dGQuJwG",
			"LegbVf6qaMKcsXPnLStkdc1JVktmmiDxy",
			"FesGqwKq7z5nPFHa5iwZctHE5EZV9Lpdq",
			"BFa1padLXBj7VHa2JYvYGzcTBPQGjPhUy",
			"4B4rc5vdD1758JSBYL1xyvE5NHGzz6xzH",
			"EDESh4DfZFC15i613pMtWniQ9arbBZRnL",
			"CZmZ9xpCzkWqjAyS7L4htzh5Lg6kf1k18",
			"CTtkcXvVdhpNp6f97LEUXPwsRD3A2ZHqP",
			"84KbQHSDnojroCVY7vQ7u9Tx7pUonPaS",
			"JjvzhxnLHLUQ5HjVRkvG827ivbLXPwA9u",
			"4CWTbdvgXHY1CLXqQNAp22nJDo5nAmts6",
		}
	case genesis.CascadeID:
		return []string{
			"NX4zVkuiRJZYe6Nzzav7GXN3TakUet3Co",
			"CMsa8cMw4eib1Hb8GG4xiUKAq5eE1BwUX",
			"DsMP6jLhi1MkDVc3qx9xx9AAZWx8e87Jd",
			"N86eodVZja3GEyZJTo3DFUPGpxEEvjGHs",
			"EkKeGSLUbHrrtuayBtbwgWDRUiAziC3ao",
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

	sampler := random.Uniform{N: len(ips)}
	for i := 0; i < count; i++ {
		i := sampler.Sample()
		sampledIPs = append(sampledIPs, ips[i])
		sampledIDs = append(sampledIDs, ids[i])
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

	// Ava fees:
	fs.Uint64Var(&Config.AvaTxFee, "ava-tx-fee", 0, "Ava transaction fee, in $nAva")

	// Assertions:
	fs.BoolVar(&loggingConfig.Assertions, "assertions-enabled", true, "Turn on assertion execution")

	// Crypto:
	fs.BoolVar(&Config.EnableCrypto, "signature-verification-enabled", true, "Turn on signature verification")

	// Database:
	db := fs.Bool("db-enabled", true, "Turn on persistent storage")
	dbDir := fs.String("db-dir", defaultDbDir, "Database directory for Ava state")

	// IP:
	consensusIP := fs.String("public-ip", "", "Public IP of this node")

	// HTTP Server:
	httpHost := fs.String("http-host", "", "Address of the HTTP server")
	httpPort := fs.Uint("http-port", 9650, "Port of the HTTP server")
	fs.BoolVar(&Config.EnableHTTPS, "http-tls-enabled", false, "Upgrade the HTTP server to HTTPs")
	fs.StringVar(&Config.HTTPSKeyFile, "http-tls-key-file", "", "TLS private key file for the HTTPs server")
	fs.StringVar(&Config.HTTPSCertFile, "http-tls-cert-file", "", "TLS certificate file for the HTTPs server")

	// Bootstrapping:
	bootstrapIPs := fs.String("bootstrap-ips", "default", "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	bootstrapIDs := fs.String("bootstrap-ids", "default", "Comma separated list of bootstrap peer ids to connect to. Example: JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	// Staking:
	consensusPort := fs.Uint("staking-port", 9651, "Port of the consensus server")
	// TODO - keeping same flag for backwards compatibility, should be changed to "staking-enabled"
	fs.BoolVar(&Config.EnableStaking, "staking-tls-enabled", true, "Enable staking. If enabled, Network TLS is required.")
	fs.BoolVar(&Config.EnableP2PTLS, "p2p-tls-enabled", true, "Require TLS to authenticate network communication")
	fs.StringVar(&Config.StakingKeyFile, "staking-tls-key-file", defaultStakingKeyPath, "TLS private key for staking")
	fs.StringVar(&Config.StakingCertFile, "staking-tls-cert-file", defaultStakingCertPath, "TLS certificate for staking")

	// Plugins:
	fs.StringVar(&Config.PluginDir, "plugin-dir", defaultPluginDirs[0], "Plugin directory for Ava VMs")

	// Logging:
	logsDir := fs.String("log-dir", "", "Logging directory for Ava")
	logLevel := fs.String("log-level", "info", "The log level. Should be one of {verbo, debug, info, warn, error, fatal, off}")
	logDisplayLevel := fs.String("log-display-level", "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, info, warn, error, fatal, off}")

	fs.IntVar(&Config.ConsensusParams.K, "snow-sample-size", 5, "Number of nodes to query for each network poll")
	fs.IntVar(&Config.ConsensusParams.Alpha, "snow-quorum-size", 4, "Alpha value to use for required number positive results")
	fs.IntVar(&Config.ConsensusParams.BetaVirtuous, "snow-virtuous-commit-threshold", 20, "Beta value to use for virtuous transactions")
	fs.IntVar(&Config.ConsensusParams.BetaRogue, "snow-rogue-commit-threshold", 30, "Beta value to use for rogue transactions")
	fs.IntVar(&Config.ConsensusParams.Parents, "snow-avalanche-num-parents", 5, "Number of vertexes for reference from each new vertex")
	fs.IntVar(&Config.ConsensusParams.BatchSize, "snow-avalanche-batch-size", 30, "Number of operations to batch in each new vertex")
	fs.IntVar(&Config.ConsensusParams.ConcurrentRepolls, "snow-concurrent-repolls", 1, "Minimum number of concurrent polls for finalizing consensus")

	// Enable/Disable APIs:
	fs.BoolVar(&Config.AdminAPIEnabled, "api-admin-enabled", true, "If true, this node exposes the Admin API")
	fs.BoolVar(&Config.InfoAPIEnabled, "api-info-enabled", true, "If true, this node exposes the Info API")
	fs.BoolVar(&Config.KeystoreAPIEnabled, "api-keystore-enabled", true, "If true, this node exposes the Keystore API")
	fs.BoolVar(&Config.MetricsAPIEnabled, "api-metrics-enabled", true, "If true, this node exposes the Metrics API")
	fs.BoolVar(&Config.HealthAPIEnabled, "api-health-enabled", true, "If true, this node exposes the Health API")
	fs.BoolVar(&Config.IPCEnabled, "api-ipcs-enabled", false, "If true, IPCs can be opened")

	// Throughput Server
	throughputPort := fs.Uint("xput-server-port", 9652, "Port of the deprecated throughput test server")
	fs.BoolVar(&Config.ThroughputServerEnabled, "xput-server-enabled", false, "If true, throughput test server is created")

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

	Config.Nat = nat.NewRouter()

	var ip net.IP
	// If public IP is not specified, get it using shell command dig
	if *consensusIP == "" {
		ip, err = Config.Nat.IP()
		if err != nil {
			ip = net.IPv4zero // Couldn't get my IP...set to 0.0.0.0
		}
	} else {
		ip = net.ParseIP(*consensusIP)
	}

	if ip == nil {
		errs.Add(fmt.Errorf("Invalid IP Address %s", *consensusIP))
		return
	}

	Config.StakingIP = utils.IPDesc{
		IP:   ip,
		Port: uint16(*consensusPort),
	}

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

	if Config.EnableP2PTLS {
		i := 0
		cb58 := formatting.CB58{}
		for _, id := range strings.Split(*bootstrapIDs, ",") {
			if id != "" {
				err = cb58.FromString(id)
				if err != nil {
					errs.Add(fmt.Errorf("couldn't parse bootstrap peer id to bytes: %w", err))
					return
				}
				peerID, err := ids.ToShortID(cb58.Bytes)
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
			errs.Add(fmt.Errorf("More bootstrap IPs, %d, provided than bootstrap IDs, %d", len(Config.BootstrapPeers), i))
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

	Config.LoggingConfig = loggingConfig

	// Throughput:
	Config.ThroughputPort = uint16(*throughputPort)

	// Router used for consensus
	Config.ConsensusRouter = &router.ChainRouter{}
}
