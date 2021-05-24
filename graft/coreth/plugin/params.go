// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ava-labs/coreth/plugin/evm"
)

var (
	cliConfig evm.CommandLineConfig
	version   bool
)

func corethFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("coreth", flag.ContinueOnError)

	fs.String(configKey, "", "Pass in CLI Config to set runtime attributes for Coreth")
	fs.Bool(versionKey, false, "If true, print version and quit")

	return fs
}

// getViper returns the viper environment for the plugin binary
func getViper() (*viper.Viper, error) {
	v := viper.New()

	fs := corethFlagSet()
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		return nil, err
	}

	return v, nil
}

func init() {
	v, err := getViper()
	if err != nil {
		cliConfig.FlagError = err
		return
	}

	if v.GetBool(versionKey) {
		fmt.Println(evm.Version)
		os.Exit(0)
	}

	if v.IsSet(configKey) {
		config := v.GetString(configKey)
		cliConfig.FlagError = json.Unmarshal([]byte(config), &cliConfig)
	} else {
		cliConfig.EthAPIEnabled = true
		cliConfig.NetAPIEnabled = true
		cliConfig.Web3APIEnabled = true
		cliConfig.RPCGasCap = 2500000000  // 25000000 x 100
		cliConfig.RPCTxFeeCap = 100       // 100 AVAX
		cliConfig.APIMaxDuration = 0      // Default to no maximum API Call duration
		cliConfig.MaxBlocksPerRequest = 0 // Default to no maximum on the number of blocks per getLogs request
	}
}
