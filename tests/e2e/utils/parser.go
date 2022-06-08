// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"fmt"
	"io/ioutil"
	"strings"

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

const (
	HARDHAT_CONFIG = "contract-examples/hardhat.config.ts"
)

func updateHardhat(rpc string) {
	input, err := ioutil.ReadFile(HARDHAT_CONFIG)
	if err != nil {
		panic(err)
	}

	lines := strings.Split(string(input), "\n")

	inSubnet := false
	for i, line := range lines {
		if strings.Contains(line, "subnet") {
			inSubnet = true
		} else if inSubnet && strings.Contains(line, "url:") {
			lines[i] = "      url: \"" + rpc + "\","
			break
		}
	}
	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(HARDHAT_CONFIG, []byte(output), 0644)
	if err != nil {
		panic(err)
	}
}

func UpdateHardhatConfig() {
	yamlFile, err := ioutil.ReadFile(outputFile)
	if err != nil {
		panic(err)
	}
	var o output
	if err := yaml.Unmarshal(yamlFile, &o); err != nil {
		panic(err)
	}

	// color.Green("\n")
	color.Yellow("Updating hardhat config with RPC URL: %s%s/rpc", o.URIs[0], o.Endpoint)

	rpc := fmt.Sprintf("%s%s/rpc", o.URIs[0], o.Endpoint)
	updateHardhat(rpc)
}
