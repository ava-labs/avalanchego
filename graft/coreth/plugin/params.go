// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/ava-labs/coreth/plugin/evm"
)

var (
	cliConfig evm.CommandLineConfig
	version   bool
)

func init() {
	fs := flag.NewFlagSet("coreth", flag.ContinueOnError)

	config := fs.String("config", "default", "Pass in CLI Config to set runtime attributes for Coreth")
	fs.BoolVar(&version, "version", false, "If true, print version and quit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		cliConfig.ParsingError = err
		return
	}

	if *config == "default" {
		cliConfig.EthAPIEnabled = true
		cliConfig.PersonalAPIEnabled = true
		cliConfig.TxPoolAPIEnabled = true
		cliConfig.NetAPIEnabled = true
		cliConfig.Web3APIEnabled = true
		cliConfig.RPCGasCap = 2500000000  // 25000000 x 100
		cliConfig.RPCTxFeeCap = 100       // 100 AVAX
		cliConfig.APIMaxDuration = 0      // Default to no maximum API Call duration
		cliConfig.MaxBlocksPerRequest = 0 // Default to no maximum on the number of blocks per getLogs request
	} else {
		// TODO only overwrite values that were explicitly set
		cliConfig.ParsingError = json.Unmarshal([]byte(*config), &cliConfig)
	}
}
