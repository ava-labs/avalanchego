// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"encoding/json"
	"math/rand"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Env is used to access shared test fixture. Intended to be
// initialized from SynchronizedBeforeSuite.
var Env *TestEnvironment

func InitSharedTestEnvironment(envBytes []byte) {
	require := require.New(ginkgo.GinkgoT())
	require.Nil(Env, "env already initialized")
	Env = &TestEnvironment{}
	require.NoError(json.Unmarshal(envBytes, Env))
	Env.require = require
}

type TestEnvironment struct {
	// The directory where the test network configuration is stored
	NetworkDir string
	// URIs used to access the API endpoints of nodes of the network
	URIs []tmpnet.NodeURI
	// The URI used to access the http server that allocates test data
	TestDataServerURI string

	require *require.Assertions
}

func (te *TestEnvironment) Marshal() []byte {
	bytes, err := json.Marshal(te)
	require.NoError(ginkgo.GinkgoT(), err)
	return bytes
}

// Initialize a new test environment with a shared network (either pre-existing or newly created).
func NewTestEnvironment(flagVars *FlagVars, desiredNetwork *tmpnet.Network) *TestEnvironment {
	require := require.New(ginkgo.GinkgoT())

	networkDir := flagVars.NetworkDir()

	// Load or create a test network
	var network *tmpnet.Network
	if len(networkDir) > 0 {
		var err error
		network, err = tmpnet.ReadNetwork(networkDir)
		require.NoError(err)
		tests.Outf("{{yellow}}Using an existing network configured at %s{{/}}\n", network.Dir)

		// Set the desired subnet configuration to ensure subsequent creation.
		for _, subnet := range desiredNetwork.Subnets {
			if existing := network.GetSubnet(subnet.Name); existing != nil {
				// Already present
				continue
			}
			network.Subnets = append(network.Subnets, subnet)
		}
	} else {
		network = desiredNetwork
		StartNetwork(network, flagVars.AvalancheGoExecPath(), flagVars.PluginDir())
	}

	// A new network will always need subnet creation and an existing
	// network will also need subnets to be created the first time it
	// is used.
	require.NoError(network.CreateSubnets(DefaultContext(), ginkgo.GinkgoWriter))

	// Wait for chains to have bootstrapped on all nodes
	Eventually(func() bool {
		for _, subnet := range network.Subnets {
			for _, validatorID := range subnet.ValidatorIDs {
				uri, err := network.GetURIForNodeID(validatorID)
				require.NoError(err)
				infoClient := info.NewClient(uri)
				for _, chain := range subnet.Chains {
					isBootstrapped, err := infoClient.IsBootstrapped(DefaultContext(), chain.ChainID.String())
					// Ignore errors since a chain id that is not yet known will result in a recoverable error.
					if err != nil || !isBootstrapped {
						return false
					}
				}
			}
		}
		return true
	}, DefaultTimeout, DefaultPollingInterval, "failed to see all chains bootstrap before timeout")

	uris := network.GetNodeURIs()
	require.NotEmpty(uris, "network contains no nodes")
	tests.Outf("{{green}}network URIs: {{/}} %+v\n", uris)

	testDataServerURI, err := fixture.ServeTestData(fixture.TestData{
		PreFundedKeys: network.PreFundedKeys,
	})
	tests.Outf("{{green}}test data server URI: {{/}} %+v\n", testDataServerURI)
	require.NoError(err)

	return &TestEnvironment{
		NetworkDir:        network.Dir,
		URIs:              uris,
		TestDataServerURI: testDataServerURI,
		require:           require,
	}
}

// Retrieve a random URI to naively attempt to spread API load across
// nodes.
func (te *TestEnvironment) GetRandomNodeURI() tmpnet.NodeURI {
	r := rand.New(rand.NewSource(time.Now().Unix())) //#nosec G404
	nodeURI := te.URIs[r.Intn(len(te.URIs))]
	tests.Outf("{{blue}} targeting node %s with URI: %s{{/}}\n", nodeURI.NodeID, nodeURI.URI)
	return nodeURI
}

// Retrieve the network to target for testing.
func (te *TestEnvironment) GetNetwork() *tmpnet.Network {
	network, err := tmpnet.ReadNetwork(te.NetworkDir)
	te.require.NoError(err)
	return network
}

// Retrieve the specified number of funded keys allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocatePreFundedKeys(count int) []*secp256k1.PrivateKey {
	keys, err := fixture.AllocatePreFundedKeys(te.TestDataServerURI, count)
	te.require.NoError(err)
	tests.Outf("{{blue}} allocated pre-funded key(s): %+v{{/}}\n", keys)
	return keys
}

// Retrieve a funded key allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocatePreFundedKey() *secp256k1.PrivateKey {
	return te.AllocatePreFundedKeys(1)[0]
}

// Create a new keychain with the specified number of test keys.
func (te *TestEnvironment) NewKeychain(count int) *secp256k1fx.Keychain {
	keys := te.AllocatePreFundedKeys(count)
	return secp256k1fx.NewKeychain(keys...)
}

// Create a new private network that is not shared with other tests.
func (te *TestEnvironment) StartPrivateNetwork(network *tmpnet.Network) {
	// Use the same configuration as the shared network
	sharedNetwork, err := tmpnet.ReadNetwork(te.NetworkDir)
	te.require.NoError(err)

	pluginDir, err := sharedNetwork.DefaultFlags.GetStringVal(config.PluginDirKey)
	te.require.NoError(err)

	StartNetwork(
		network,
		sharedNetwork.DefaultRuntimeConfig.AvalancheGoPath,
		pluginDir,
	)
}
