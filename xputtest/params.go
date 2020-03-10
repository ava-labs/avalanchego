// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"net"

	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

var (
	config Config
	err    error
)

// Parse the CLI arguments
func init() {
	errs := &wrappers.Errs{}
	defer func() { err = errs.Err }()

	loggingConfig, err := logging.DefaultConfig()
	errs.Add(err)

	// NetworkID:
	networkName := flag.String("network-id", genesis.LocalName, "Network ID this node will connect to")

	// Ava fees:
	flag.Uint64Var(&config.AvaTxFee, "ava-tx-fee", 0, "Ava transaction fee, in $nAva")

	// Assertions:
	flag.BoolVar(&loggingConfig.Assertions, "assertions-enabled", true, "Turn on assertion execution")

	// Crypto:
	flag.BoolVar(&config.EnableCrypto, "signature-verification-enabled", true, "Turn on signature verification")

	// Remote Server:
	ip := flag.String("ip", "127.0.0.1", "IP address of the remote server socket")
	port := flag.Uint("port", 9652, "Port of the remote server socket")

	// Logging:
	logsDir := flag.String("log-dir", "", "Logging directory for Ava")
	logLevel := flag.String("log-level", "info", "The log level. Should be one of {all, debug, info, warn, error, fatal, off}")

	// Test Variables:
	chain := flag.Bool("chain", false, "Execute chain transactions")
	dag := flag.Bool("dag", false, "Execute dag transactions")
	flag.IntVar(&config.Key, "key", 0, "Index of the genesis key list to use")
	flag.IntVar(&config.MaxOutstandingTxs, "max_outstanding", 1000, "Maximum number of transactions to leave outstanding")

	flag.Parse()

	networkID, err := genesis.NetworkID(*networkName)
	errs.Add(err)

	if networkID != genesis.LocalID {
		errs.Add(fmt.Errorf("the only supported networkID is: %s", genesis.LocalName))
	}

	config.NetworkID = networkID

	// Remote:
	parsedIP := net.ParseIP(*ip)
	if parsedIP == nil {
		errs.Add(fmt.Errorf("invalid IP Address %s", *ip))
	}
	config.RemoteIP = utils.IPDesc{
		IP:   parsedIP,
		Port: uint16(*port),
	}

	// Logging:
	if *logsDir != "" {
		loggingConfig.Directory = *logsDir
	}
	level, err := logging.ToLevel(*logLevel)
	errs.Add(err)
	loggingConfig.LogLevel = level
	loggingConfig.DisplayLevel = level
	config.LoggingConfig = loggingConfig

	// Test Variables:
	switch {
	case *chain:
		config.Chain = ChainChain
	case *dag:
		config.Chain = DagChain
	default:
		config.Chain = UnknownChain
	}
}
