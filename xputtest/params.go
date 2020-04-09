// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"os"

	stdnet "net"

	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/formatting"
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

	fs := flag.NewFlagSet("xputtest", flag.ContinueOnError)

	// NetworkID:
	networkName := fs.String("network-id", genesis.LocalName, "Network ID this node will connect to")

	// Ava fees:
	fs.Uint64Var(&config.AvaTxFee, "ava-tx-fee", 0, "Ava transaction fee, in $nAva")

	// Assertions:
	fs.BoolVar(&loggingConfig.Assertions, "assertions-enabled", true, "Turn on assertion execution")

	// Crypto:
	fs.BoolVar(&config.EnableCrypto, "signature-verification-enabled", true, "Turn on signature verification")

	// Remote Server:
	ip := fs.String("ip", "127.0.0.1", "IP address of the remote server socket")
	port := fs.Uint("port", 9652, "Port of the remote server socket")

	// Logging:
	logsDir := fs.String("log-dir", "", "Logging directory for Ava")
	logLevel := fs.String("log-level", "info", "The log level. Should be one of {all, debug, info, warn, error, fatal, off}")

	// Test Variables:
	spchain := fs.Bool("sp-chain", false, "Execute simple payment chain transactions")
	spdag := fs.Bool("sp-dag", false, "Execute simple payment dag transactions")
	avm := fs.Bool("avm", false, "Execute avm transactions")
	key := fs.String("key", "", "Funded key in the genesis key to use to issue transactions")
	fs.IntVar(&config.NumTxs, "num-txs", 25000, "Total number of transaction to issue")
	fs.IntVar(&config.MaxOutstandingTxs, "max-outstanding", 1000, "Maximum number of transactions to leave outstanding")

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

	config.NetworkID = networkID

	// Remote:
	parsedIP := stdnet.ParseIP(*ip)
	if parsedIP == nil {
		errs.Add(fmt.Errorf("invalid IP Address %s", *ip))
	}
	config.RemoteIP = utils.IPDesc{
		IP:   parsedIP,
		Port: uint16(*port),
	}

	cb58 := formatting.CB58{}
	errs.Add(cb58.FromString(*key))
	config.Key = cb58.Bytes

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
	case *spchain:
		config.Chain = spChain
	case *spdag:
		config.Chain = spDAG
	case *avm:
		config.Chain = avmDAG
	default:
		config.Chain = unknown
	}
}
