package main

import (
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/network"
)

const (
	publicApi = "https://api.avax-test.network/ext/info"
)

func trackBootstrappers(network network.Network, bootstrappers []genesis.Bootstrapper) []genesis.Bootstrapper {
	if len(bootstrappers) == 0 {
		// We need to initially connect to some nodes in the network before peer
		// gossip will enable connecting to all the remaining nodes in the network.
		bootstrappers = genesis.SampleBootstrappers(NetworkId, 5)
	} 

	for _, bootstrapper := range bootstrappers {
		network.ManuallyTrack(bootstrapper.ID, bootstrapper.IP)
	}
	return bootstrappers
}
