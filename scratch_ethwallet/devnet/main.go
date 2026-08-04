// Starts a local tmpnet network with the latest upgrade active from genesis,
// which is what the eth facade is gated on. tmpnetctl leaves the latest
// upgrade unscheduled, so it cannot be used for this.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

func main() {
	rootDir := flag.String("root-network-dir", "", "where to create the network dir")
	avalanchegoPath := flag.String("avalanchego-path", "", "avalanchego binary")
	nodeCount := flag.Int("node-count", 3, "number of nodes")
	flag.Parse()

	defaultFlags, err := tmpnet.UpgradeFlags(tmpnet.UpgradeConfig(0))
	must(err)

	network := &tmpnet.Network{
		Owner:        "pchain-v2-eth-facade",
		Nodes:        tmpnet.NewNodesOrPanic(*nodeCount),
		DefaultFlags: defaultFlags,
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				AvalancheGoPath: *avalanchegoPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	log, err := tests.LoggerForFormat("", "auto")
	must(err)
	must(tmpnet.BootstrapNewNetwork(ctx, log, network, *rootDir))

	fmt.Printf("NETWORK_DIR=%s\n", network.Dir)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
