// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"io/ioutil"
	"os"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
	"github.com/fatih/color"
	"gopkg.in/yaml.v2"
)

/*
===Example File===

endpoint: /ext/bc/2Z36RnQuk1hvsnFeGWzfZUfXNr7w1SjzmDQ78YxfTVNAkDq3nZ
logsDir: /var/folders/mp/6jm81gc11dv3xtcwxmrd8mcr0000gn/T/runnerlogs2984620995
pid: 55547
uris:
- http://localhost:61278
- http://localhost:61280
- http://localhost:61282
- http://localhost:61284
- http://localhost:61286
*/

type output struct {
	Endpoint string   `yaml:"endpoint"`
	Logs     string   `yaml:"logsDir"`
	URIs     []string `yaml:"uris"`
}

func main() {
	if len(os.Args) != 8 {
		panic("missing args <yaml> <chainID> <address> <avalanchego-path> <plugin-dir> <grpc-endpoint> <genesis-path>")
	}

	b := make([]byte, 32)
	vmName := "subnetevm"
	copy(b, []byte(vmName))
	var err error
	vmId, err := ids.ToID(b)
	if err != nil {
		panic(err)
	}
	utils.SetOutputFile(os.Args[1])
	utils.SetPluginDir(os.Args[5])
	err = runner.InitializeRunner(os.Args[4], os.Args[6], "info")
	if err != nil {
		panic(err)
	}
	_, err = runner.StartNetwork(vmId, vmName, os.Args[7], os.Args[5])
	if err != nil {
		panic(err)
	}
	blockchainId, logsDir, err := runner.WaitForCustomVm(vmId)
	if err != nil {
		panic(err)
	}
	runner.GetClusterInfo(blockchainId, logsDir)

	yamlFile, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var o output
	if err := yaml.Unmarshal(yamlFile, &o); err != nil {
		panic(err)
	}

	color.Green("\n")
	color.Green("Logs Directory: %s", o.Logs)
	color.Green("\n")

	color.Green("EVM Chain ID: %s", os.Args[2])
	color.Green("Funded Address: %s", os.Args[3])
	color.Green("RPC Endpoints:")
	for _, uri := range o.URIs {
		color.Green("- %s%s/rpc", uri, o.Endpoint)
	}
	color.Green("\n")

	color.Green("WS Endpoints:")
	for _, uri := range o.URIs {
		wsURI := strings.ReplaceAll(uri, "http", "ws")
		color.Green("- %s%s/ws", wsURI, o.Endpoint)
	}
	color.Green("\n")

	color.Yellow("MetaMask Quick Start:")
	color.Yellow("Funded Address: %s", os.Args[3])
	color.Yellow("Network Name: Local EVM")
	color.Yellow("RPC URL: %s%s/rpc", o.URIs[0], o.Endpoint)
	color.Yellow("Chain ID: %s", os.Args[2])
	color.Yellow("Currency Symbol: LEVM")
}
