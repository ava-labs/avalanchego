// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

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
	PID      int      `yaml:"pid"`
	URIs     []string `yaml:"uris"`
}

type rpcFile struct {
	Rpc string `json:"rpc"`
}

const (
	DYNAMIC_RPC_FILE = "contract-examples/dynamic_rpc.json"
)

func writeRPC(rpcUrl string) error {
	rpcFileData := rpcFile{
		Rpc: rpcUrl,
	}

	file, err := json.MarshalIndent(rpcFileData, "", " ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(DYNAMIC_RPC_FILE, file, 0644)
	return err
}

func UpdateHardhatConfig() error {
	yamlFile, err := ioutil.ReadFile(outputFile)
	if err != nil {
		return err
	}
	var o output
	if err := yaml.Unmarshal(yamlFile, &o); err != nil {
		return err
	}

	color.Yellow("Updating hardhat config with RPC URL: %s%s/rpc", o.URIs[0], o.Endpoint)

	rpc := fmt.Sprintf("%s%s/rpc", o.URIs[0], o.Endpoint)
	if err = writeRPC(rpc); err != nil {
		return err
	}
	return nil
}
