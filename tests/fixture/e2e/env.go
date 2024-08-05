// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Env is used to access shared test fixture. Intended to be
// initialized from SynchronizedBeforeSuite. Not exported to limit
// access to the shared env to GetEnv which adds a test context.
var env *TestEnvironment

func InitSharedTestEnvironment(t require.TestingT, envBytes []byte) {
	require := require.New(t)
	require.Nil(env, "env already initialized")
	env = &TestEnvironment{}
	require.NoError(json.Unmarshal(envBytes, env))
}

type TestEnvironment struct {
	// The directory where the test network configuration is stored
	NetworkDir string
	// URIs used to access the API endpoints of nodes of the network
	URIs []tmpnet.NodeURI
	// The URI used to access the http server that allocates test data
	TestDataServerURI string
	// The duration to wait before shutting down private networks. A
	// non-zero value may be useful to ensure all metrics can be
	// scraped before shutdown.
	PrivateNetworkShutdownDelay time.Duration

	testContext tests.TestContext
}

// Retrieve the test environment configured with the provided test context.
func GetEnv(tc tests.TestContext) *TestEnvironment {
	return &TestEnvironment{
		NetworkDir:                  env.NetworkDir,
		URIs:                        env.URIs,
		TestDataServerURI:           env.TestDataServerURI,
		PrivateNetworkShutdownDelay: env.PrivateNetworkShutdownDelay,
		testContext:                 tc,
	}
}

func (te *TestEnvironment) Marshal() []byte {
	bytes, err := json.Marshal(te)
	require.NoError(te.testContext, err)
	return bytes
}

// Initialize a new test environment with a shared network (either pre-existing or newly created).
func NewTestEnvironment(tc tests.TestContext, flagVars *FlagVars, desiredNetwork *tmpnet.Network) *TestEnvironment {
	require := require.New(tc)

	var network *tmpnet.Network
	// Need to load the network if it is being stopped or reused
	if flagVars.StopNetwork() || flagVars.ReuseNetwork() {
		networkDir := flagVars.NetworkDir()
		var networkSymlink string // If populated, prompts removal of the referenced symlink if --stop-network is specified
		if len(networkDir) == 0 {
			// Attempt to reuse the network at the default owner path
			symlinkPath, err := tmpnet.GetReusableNetworkPathForOwner(desiredNetwork.Owner)
			require.NoError(err)
			_, err = os.Stat(symlinkPath)
			if !errors.Is(err, os.ErrNotExist) {
				// Try to load the existing network
				require.NoError(err)
				networkDir = symlinkPath
				// Enable removal of the referenced symlink if --stop-network is specified
				networkSymlink = symlinkPath
			}
		}

		if len(networkDir) > 0 {
			var err error
			network, err = tmpnet.ReadNetwork(networkDir)
			require.NoError(err)
			tc.Outf("{{yellow}}Loaded a network configured at %s{{/}}\n", network.Dir)
		}

		if flagVars.StopNetwork() {
			if len(networkSymlink) > 0 {
				// Remove the symlink to avoid attempts to reuse the stopped network
				tc.Outf("Removing symlink %s\n", networkSymlink)
				if err := os.Remove(networkSymlink); !errors.Is(err, os.ErrNotExist) {
					require.NoError(err)
				}
			}
			if network != nil {
				tc.Outf("Stopping network\n")
				require.NoError(network.Stop(tc.DefaultContext()))
			} else {
				tc.Outf("No network to stop\n")
			}
			os.Exit(0)
		}
	}

	// Start a new network
	if network == nil {
		network = desiredNetwork
		StartNetwork(
			tc,
			network,
			flagVars.AvalancheGoExecPath(),
			flagVars.PluginDir(),
			flagVars.NetworkShutdownDelay(),
			flagVars.ReuseNetwork(),
		)

		// Wait for chains to have bootstrapped on all nodes
		tc.Eventually(func() bool {
			for _, subnet := range network.Subnets {
				for _, validatorID := range subnet.ValidatorIDs {
					uri, err := network.GetURIForNodeID(validatorID)
					require.NoError(err)
					infoClient := info.NewClient(uri)
					for _, chain := range subnet.Chains {
						isBootstrapped, err := infoClient.IsBootstrapped(tc.DefaultContext(), chain.ChainID.String())
						// Ignore errors since a chain id that is not yet known will result in a recoverable error.
						if err != nil || !isBootstrapped {
							return false
						}
					}
				}
			}
			return true
		}, DefaultTimeout, DefaultPollingInterval, "failed to see all chains bootstrap before timeout")
	}

	uris := network.GetNodeURIs()
	require.NotEmpty(uris, "network contains no nodes")
	tc.Outf("{{green}}network URIs: {{/}} %+v\n", uris)

	testDataServerURI, err := fixture.ServeTestData(fixture.TestData{
		PreFundedKeys: network.PreFundedKeys,
	})
	tc.Outf("{{green}}test data server URI: {{/}} %+v\n", testDataServerURI)
	require.NoError(err)

	return &TestEnvironment{
		NetworkDir:                  network.Dir,
		URIs:                        uris,
		TestDataServerURI:           testDataServerURI,
		PrivateNetworkShutdownDelay: flagVars.NetworkShutdownDelay(),
		testContext:                 tc,
	}
}

// Retrieve a random URI to naively attempt to spread API load across
// nodes.
func (te *TestEnvironment) GetRandomNodeURI() tmpnet.NodeURI {
	r := rand.New(rand.NewSource(time.Now().Unix())) //#nosec G404
	nodeURI := te.URIs[r.Intn(len(te.URIs))]
	te.testContext.Outf("{{blue}} targeting node %s with URI: %s{{/}}\n", nodeURI.NodeID, nodeURI.URI)
	return nodeURI
}

// Retrieve the network to target for testing.
func (te *TestEnvironment) GetNetwork() *tmpnet.Network {
	network, err := tmpnet.ReadNetwork(te.NetworkDir)
	require.NoError(te.testContext, err)
	return network
}

// Retrieve the specified number of funded keys allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocatePreFundedKeys(count int) []*secp256k1.PrivateKey {
	keys, err := fixture.AllocatePreFundedKeys(te.TestDataServerURI, count)
	require.NoError(te.testContext, err)
	te.testContext.Outf("{{blue}} allocated pre-funded key(s): %+v{{/}}\n", keys)
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
	require := require.New(te.testContext)
	// Use the same configuration as the shared network
	sharedNetwork, err := tmpnet.ReadNetwork(te.NetworkDir)
	require.NoError(err)

	pluginDir, err := sharedNetwork.DefaultFlags.GetStringVal(config.PluginDirKey)
	require.NoError(err)

	StartNetwork(
		te.testContext,
		network,
		sharedNetwork.DefaultRuntimeConfig.AvalancheGoPath,
		pluginDir,
		te.PrivateNetworkShutdownDelay,
		false, /* reuseNetwork */
	)
}
