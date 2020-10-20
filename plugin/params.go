package main

import (
	"encoding/json"
	"flag"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/plugin/evm"
)

const (
	name = "coreth"
)

var (
	cliConfig evm.CommandLineConfig
	errs      wrappers.Errs
)

func init() {
	errs := wrappers.Errs{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)

	config := fs.String("coreth-config", "default", "Pass in CLI Config to set runtime attributes for Coreth")
	if *config == "default" {
		cliConfig.EthAPIEnabled = true
	} else {
		errs.Add(json.Unmarshal([]byte(*config), &cliConfig))
	}
}
