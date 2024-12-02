package main

import (
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/network"
)

const (
	publicApi = "https://api.avax-test.network/ext/info"
)

func trackBootstrappers(network network.Network) []genesis.Bootstrapper {
	// We need to initially connect to some nodes in the network before peer
	// gossip will enable connecting to all the remaining nodes in the network.
	bootstrappers := genesis.SampleBootstrappers(NetworkId, 5)
	for _, bootstrapper := range bootstrappers {
		network.ManuallyTrack(bootstrapper.ID, bootstrapper.IP)
	}
	return bootstrappers
}