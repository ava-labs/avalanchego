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
	"strings"

	"github.com/ava-labs/go-ethereum/p2p/nat"

	"github.com/ava-labs/gecko/database/leveldb"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/node"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

// Results of parsing the CLI
var (
	Config = node.Config{}
	Err    error
)

var (
	errBootstrapMismatch = errors.New("more bootstrap IDs provided than bootstrap IPs")
)

// Parse the CLI arguments
func init() {
	errs := &wrappers.Errs{}
	defer func() { Err = errs.Err }()

	loggingConfig, err := logging.DefaultConfig()
	errs.Add(err)

	fs := flag.NewFlagSet("gecko", flag.ContinueOnError)

	// NetworkID:
	networkName := fs.String("network-id", genesis.LocalName, "Network ID this node will connect to")

	// Ava fees:
	fs.Uint64Var(&Config.AvaTxFee, "ava-tx-fee", 0, "Ava transaction fee, in $nAva")

	// Assertions:
	fs.BoolVar(&loggingConfig.Assertions, "assertions-enabled", true, "Turn on assertion execution")

	// Crypto:
	fs.BoolVar(&Config.EnableCrypto, "signature-verification-enabled", true, "Turn on signature verification")

	// Database:
	db := fs.Bool("db-enabled", true, "Turn on persistent storage")
	dbDir := fs.String("db-dir", "db", "Database directory for Ava state")

	// IP:
	consensusIP := fs.String("public-ip", "", "Public IP of this node")

	// HTTP Server:
	httpPort := fs.Uint("http-port", 9650, "Port of the HTTP server")
	fs.BoolVar(&Config.EnableHTTPS, "http-tls-enabled", false, "Upgrade the HTTP server to HTTPs")
	fs.StringVar(&Config.HTTPSKeyFile, "http-tls-key-file", "", "TLS private key file for the HTTPs server")
	fs.StringVar(&Config.HTTPSCertFile, "http-tls-cert-file", "", "TLS certificate file for the HTTPs server")

	// Bootstrapping:
	bootstrapIPs := fs.String("bootstrap-ips", "", "Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631")
	bootstrapIDs := fs.String("bootstrap-ids", "", "Comma separated list of bootstrap peer ids to connect to. Example: JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	// Staking:
	consensusPort := fs.Uint("staking-port", 9651, "Port of the consensus server")
	fs.BoolVar(&Config.EnableStaking, "staking-tls-enabled", true, "Require TLS to authenticate staking connections")
	fs.StringVar(&Config.StakingKeyFile, "staking-tls-key-file", "", "TLS private key file for staking connections")
	fs.StringVar(&Config.StakingCertFile, "staking-tls-cert-file", "", "TLS certificate file for staking connections")

	// Logging:
	logsDir := fs.String("log-dir", "", "Logging directory for Ava")
	logLevel := fs.String("log-level", "info", "The log level. Should be one of {verbo, debug, info, warn, error, fatal, off}")
	logDisplayLevel := fs.String("log-display-level", "", "The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, info, warn, error, fatal, off}")

	fs.IntVar(&Config.ConsensusParams.K, "snow-sample-size", 20, "Number of nodes to query for each network poll")
	fs.IntVar(&Config.ConsensusParams.Alpha, "snow-quorum-size", 18, "Alpha value to use for required number positive results")
	fs.IntVar(&Config.ConsensusParams.BetaVirtuous, "snow-virtuous-commit-threshold", 20, "Beta value to use for virtuous transactions")
	fs.IntVar(&Config.ConsensusParams.BetaRogue, "snow-rogue-commit-threshold", 30, "Beta value to use for rogue transactions")
	fs.IntVar(&Config.ConsensusParams.Parents, "snow-avalanche-num-parents", 5, "Number of vertexes for reference from each new vertex")
	fs.IntVar(&Config.ConsensusParams.BatchSize, "snow-avalanche-batch-size", 30, "Number of operations to batch in each new vertex")

	// Enable/Disable APIs:
	fs.BoolVar(&Config.AdminAPIEnabled, "api-admin-enabled", true, "If true, this node exposes the Admin API")
	fs.BoolVar(&Config.KeystoreAPIEnabled, "api-keystore-enabled", true, "If true, this node exposes the Keystore API")
	fs.BoolVar(&Config.MetricsAPIEnabled, "api-metrics-enabled", true, "If true, this node exposes the Metrics API")
	fs.BoolVar(&Config.IPCEnabled, "api-ipcs-enabled", false, "If true, IPCs can be opened")

	// Throughput Server
	throughputPort := fs.Uint("xput-server-port", 9652, "Port of the deprecated throughput test server")
	fs.BoolVar(&Config.ThroughputServerEnabled, "xput-server-enabled", false, "If true, throughput test server is created")

	ferr := fs.Parse(os.Args[1:])

	if ferr == flag.ErrHelp {
		// display usage/help text and exit successfully
		os.Exit(0)
	}

	if ferr != nil {
		// other type of error occurred when parsing args
		os.Exit(2)
	}

	networkID, err := genesis.NetworkID(*networkName)
	errs.Add(err)

	if networkID != genesis.LocalID {
		errs.Add(fmt.Errorf("the only supported networkID is: %s", genesis.LocalName))
	}

	Config.NetworkID = networkID

	// DB:
	if *db && err == nil {
		// TODO: Add better params here
		dbPath := path.Join(*dbDir, genesis.NetworkName(Config.NetworkID))
		db, err := leveldb.New(dbPath, 0, 0, 0)
		Config.DB = db
		errs.Add(err)
	} else {
		Config.DB = memdb.New()
	}

	Config.Nat = nat.Any()

	var ip net.IP
	// If public IP is not specified, get it using shell command dig
	if *consensusIP == "" {
		ip, err = Config.Nat.ExternalIP()
		errs.Add(fmt.Errorf("%s\nIf you are trying to create a local network, try adding --public-ip=127.0.0.1", err))
	} else {
		ip = net.ParseIP(*consensusIP)
	}

	if ip == nil {
		errs.Add(fmt.Errorf("Invalid IP Address %s", *consensusIP))
	}
	Config.StakingIP = utils.IPDesc{
		IP:   ip,
		Port: uint16(*consensusPort),
	}

	// Bootstrapping:
	for _, ip := range strings.Split(*bootstrapIPs, ",") {
		if ip != "" {
			addr, err := utils.ToIPDesc(ip)
			errs.Add(err)
			Config.BootstrapPeers = append(Config.BootstrapPeers, &node.Peer{
				IP: addr,
			})
		}
	}
	if Config.EnableStaking {
		i := 0
		cb58 := formatting.CB58{}
		for _, id := range strings.Split(*bootstrapIDs, ",") {
			if id != "" {
				errs.Add(cb58.FromString(id))
				cert, err := ids.ToShortID(cb58.Bytes)
				errs.Add(err)

				if len(Config.BootstrapPeers) <= i {
					errs.Add(errBootstrapMismatch)
					continue
				}
				Config.BootstrapPeers[i].ID = cert
				i++
			}
		}
		if len(Config.BootstrapPeers) != i {
			errs.Add(fmt.Errorf("More bootstrap IPs, %d, provided than bootstrap IDs, %d", len(Config.BootstrapPeers), i))
		}
	} else {
		for _, peer := range Config.BootstrapPeers {
			peer.ID = ids.NewShortID(hashing.ComputeHash160Array([]byte(peer.IP.String())))
		}
	}

	// HTTP:
	Config.HTTPPort = uint16(*httpPort)

	// Logging:
	if *logsDir != "" {
		loggingConfig.Directory = *logsDir
	}
	logFileLevel, err := logging.ToLevel(*logLevel)
	errs.Add(err)
	loggingConfig.LogLevel = logFileLevel

	if *logDisplayLevel == "" {
		*logDisplayLevel = *logLevel
	}
	displayLevel, err := logging.ToLevel(*logDisplayLevel)
	errs.Add(err)
	loggingConfig.DisplayLevel = displayLevel

	Config.LoggingConfig = loggingConfig

	// Throughput:
	Config.ThroughputPort = uint16(*throughputPort)

	// Router used for consensus
	Config.ConsensusRouter = &router.ChainRouter{}
}
