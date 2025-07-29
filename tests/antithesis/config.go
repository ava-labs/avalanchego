// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"flag"
	"os"
	"strings"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	URIsKey     = "uris"
	ChainIDsKey = "chain-ids"
	DurationKey = "duration"

	EnvPrefix = "avawl"
)

type Config struct {
	URIs     []string
	ChainIDs []string
	Duration time.Duration
}

type SubnetsForNodesFunc func(nodes ...*tmpnet.Node) []*tmpnet.Subnet

func NewConfig(tc tests.TestContext, defaultNetwork *tmpnet.Network) *Config {
	return NewConfigWithSubnets(tc, defaultNetwork, nil)
}

func NewConfigWithSubnets(tc tests.TestContext, defaultNetwork *tmpnet.Network, getSubnets SubnetsForNodesFunc) *Config {
	// tmpnet configuration
	flagVars := e2e.RegisterFlags()

	var (
		duration time.Duration
		uris     CSV
		// Accept a list of chain IDs, assume they each belong to a separate subnet
		// TODO(marun) Revisit how chain IDs are provided when 1:n subnet:chain configuration is required.
		chainIDs CSV
	)
	flag.DurationVar(&duration, DurationKey, 0, "[optional] the duration to execute the workload for")
	flag.Var(&uris, URIsKey, "[optional] URIs of nodes that the workload can communicate with")
	flag.Var(&chainIDs, ChainIDsKey, "[optional] IDs of chains to target for testing")

	flag.Parse()

	// Env vars take priority over flags
	envURIs := os.Getenv(config.EnvVarName(EnvPrefix, URIsKey))
	if len(envURIs) > 0 {
		//nolint:errcheck // CSV.Set doesn't actually return an error
		uris.Set(envURIs)
	}
	envChainIDs := os.Getenv(config.EnvVarName(EnvPrefix, ChainIDsKey))
	if len(envChainIDs) > 0 {
		//nolint:errcheck // CSV.Set doesn't actually return an error
		chainIDs.Set(envChainIDs)
	}

	// Use the network configuration provided
	if len(uris) != 0 {
		require.NoError(tc, awaitHealthyNodes(tc.DefaultContext(), tc.Log(), uris), "failed to see healthy nodes")
		return &Config{
			URIs:     uris,
			ChainIDs: chainIDs,
			Duration: duration,
		}
	}

	// Create a new network
	return configForNewNetwork(tc, defaultNetwork, getSubnets, flagVars, duration)
}

// configForNewNetwork creates a new network and returns the resulting config.
func configForNewNetwork(
	tc tests.TestContext,
	defaultNetwork *tmpnet.Network,
	getSubnets SubnetsForNodesFunc,
	flagVars *e2e.FlagVars,
	duration time.Duration,
) *Config {
	if defaultNetwork.Nodes == nil {
		nodeCount, err := flagVars.NodeCount()
		require.NoError(tc, err)
		defaultNetwork.Nodes = tmpnet.NewNodesOrPanic(nodeCount)
	}
	if defaultNetwork.Subnets == nil && getSubnets != nil {
		defaultNetwork.Subnets = getSubnets(defaultNetwork.Nodes...)
	}

	testEnv := e2e.NewTestEnvironment(tc, flagVars, defaultNetwork)

	c := &Config{
		Duration: duration,
	}
	network := testEnv.GetNetwork()
	uris := network.GetNodeURIs()
	c.URIs = make(CSV, len(uris))
	for i, nodeURI := range uris {
		c.URIs[i] = nodeURI.URI
	}
	c.ChainIDs = make(CSV, len(network.Subnets))
	for i, subnet := range network.Subnets {
		c.ChainIDs[i] = subnet.Chains[0].ChainID.String()
	}

	return c
}

// CSV is a custom type that implements the flag.Value interface
type CSV []string

// String returns the string representation of the CSV type
func (c *CSV) String() string {
	return strings.Join(*c, ",")
}

// Set splits the input string by commas and sets the CSV type
func (c *CSV) Set(value string) error {
	*c = strings.Split(value, ",")
	return nil
}
