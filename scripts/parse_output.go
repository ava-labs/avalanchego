package main

import (
	"io/ioutil"
	"os"
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

func main() {
	if len(os.Args) != 2 {
		panic("missing file arg")
	}

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
	color.Green("PID: %d", o.PID)
	color.Green("\n")

	color.Green("RPC Endpoints (use with MetaMask):")
	for _, uri := range o.URIs {
		color.Green("- %s%s/rpc", uri, o.Endpoint)
	}
	color.Green("\n")

	color.Green("WS Endpoints:")
	for _, uri := range o.URIs {
		wsURI := strings.ReplaceAll(uri, "http", "ws")
		color.Green("- %s%s/ws", wsURI, o.Endpoint)
	}
}
