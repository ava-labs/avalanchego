// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Env is used to access shared test fixture. Intended to be
// initialized from SynchronizedBeforeSuite. Not exported to limit
// access to the shared env to GetEnv which adds a test context.
var env *TestEnvironment

func InitSharedTestEnvironment(tc tests.TestContext, envBytes []byte) {
	require := require.New(tc)
	require.Nil(env, "env already initialized")
	env = &TestEnvironment{}
	require.NoError(json.Unmarshal(envBytes, env))
	env.testContext = tc

	// Ginkgo parallelization is at the process level, so a given key
	// can safely be used by all tests in a given process without fear
	// of conflicting usage.
	network := env.GetNetwork()
	env.PreFundedKey = network.PreFundedKeys[ginkgo.GinkgoParallelProcess()]
	require.NotNil(env.PreFundedKey)
}

type TestEnvironment struct {
	// The parent directory of network directories
	RootNetworkDir string
	// The directory where the test network configuration is stored
	NetworkDir string
	// Pre-funded key for this ginkgo process
	PreFundedKey *secp256k1.PrivateKey
	// The duration to wait before shutting down private networks. A
	// non-zero value may be useful to ensure all metrics can be
	// scraped before shutdown.
	PrivateNetworkShutdownDelay time.Duration

	testContext tests.TestContext
}

// Retrieve the test environment configured with the provided test context.
func GetEnv(tc tests.TestContext) *TestEnvironment {
	if env == nil {
		return nil
	}
	return &TestEnvironment{
		RootNetworkDir:              env.RootNetworkDir,
		NetworkDir:                  env.NetworkDir,
		PreFundedKey:                env.PreFundedKey,
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

	networkCmd, err := flagVars.NetworkCmd()
	require.NoError(err)

	// Consider monitoring flags for any command but stop
	if networkCmd != StopNetworkCmd {
		if flagVars.StartMetricsCollector() {
			require.NoError(tmpnet.StartPrometheus(tc.DefaultContext(), tc.Log()))
		}
		if flagVars.StartLogsCollector() {
			require.NoError(tmpnet.StartPromtail(tc.DefaultContext(), tc.Log()))
		}

		// Register cleanups before network start to ensure they run after the network is stopped (LIFO)

		if flagVars.CheckMetricsCollected() {
			tc.DeferCleanup(func() {
				if network == nil {
					tc.Log().Warn("unable to check that metrics were collected from an uninitialized network")
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckMetricsExist(ctx, tc.Log(), network.UUID))
			})
		}

		if flagVars.CheckLogsCollected() {
			tc.DeferCleanup(func() {
				if network == nil {
					tc.Log().Warn("unable to check that logs were collected from an uninitialized network")
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckLogsExist(ctx, tc.Log(), network.UUID))
			})
		}
	}

	// Attempt to load the network if it may already be running
	if networkCmd == StopNetworkCmd || networkCmd == ReuseNetworkCmd || networkCmd == RestartNetworkCmd {
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
			network, err = tmpnet.ReadNetwork(tc.DefaultContext(), tc.Log(), networkDir)
			require.NoError(err)
			tc.Log().Info("loaded a network",
				zap.String("networkDir", networkDir),
			)
		}

		if networkCmd == StopNetworkCmd {
			if len(networkSymlink) > 0 {
				// Remove the symlink to avoid attempts to reuse the stopped network
				tc.Log().Info("removing symlink",
					zap.String("path", networkSymlink),
				)
				if err := os.Remove(networkSymlink); !errors.Is(err, os.ErrNotExist) {
					require.NoError(err)
				}
			}
			if network != nil {
				tc.Log().Info("stopping network")
				require.NoError(network.Stop(tc.DefaultContext()))
			} else {
				tc.Log().Warn("no network to stop")
			}
			os.Exit(0)
		}

		if network != nil && networkCmd == RestartNetworkCmd {
			require.NoError(network.Restart(tc.DefaultContext()))
		}
	}

	// Start a new network
	if network == nil {
		// TODO(marun) Maybe accept a factory function for the desired network
		// that is only run when a new network will be started?

		network = desiredNetwork
		runtimeConfig, err := flagVars.NodeRuntimeConfig()
		require.NoError(err)
		network.DefaultRuntimeConfig = *runtimeConfig

		StartNetwork(
			tc,
			network,
			flagVars.RootNetworkDir(),
			flagVars.NetworkShutdownDelay(),
			networkCmd,
		)
	}

	// Once one or more nodes are running it should be safe to wait for promtail to report readiness
	if flagVars.StartLogsCollector() {
		runtimeConfig, err := flagVars.NodeRuntimeConfig()
		require.NoError(err)
		if runtimeConfig.Kube != nil {
			// TODO(marun) Maybe make this configurable to enable the check for a test suite that writes service
			// discovery configuration for its own metrics endpoint?
			tc.Log().Warn("skipping check for logs collection readiness since kube nodes won't create have created the required service discovery config")
		} else {
			require.NoError(tmpnet.WaitForPromtailReadiness(tc.DefaultContext(), tc.Log()))
		}
	}

	if networkCmd == StartNetworkCmd {
		os.Exit(0)
	}

	suiteConfig, _ := ginkgo.GinkgoConfiguration()
	require.Greater(
		len(network.PreFundedKeys),
		suiteConfig.ParallelTotal,
		"not enough pre-funded keys for the requested number of parallel test processes",
	)

	env := &TestEnvironment{
		RootNetworkDir:              flagVars.RootNetworkDir(),
		NetworkDir:                  network.Dir,
		PrivateNetworkShutdownDelay: flagVars.NetworkShutdownDelay(),
		testContext:                 tc,
	}

	uris := network.GetNodeURIs()
	require.NotEmpty(uris, "network contains no nodes")
	tc.Log().Info("network nodes are available",
		zap.Any("uris", uris),
	)

	return env
}

// Retrieve a random URI to naively attempt to spread API load across nodes.
func (te *TestEnvironment) GetRandomNodeURI() tmpnet.NodeURI {
	var (
		tc             = te.testContext
		r              = rand.New(rand.NewSource(time.Now().Unix())) //#nosec G404
		network        = te.GetNetwork()
		availableNodes = []*tmpnet.Node{}
	)

	for _, node := range network.Nodes {
		if node.IsEphemeral {
			// Avoid returning URIs for nodes whose lifespan is indeterminate
			continue
		}
		if !node.IsRunning() {
			// Only running nodes have URIs
			continue
		}
		availableNodes = append(availableNodes, node)
	}

	require.NotEmpty(tc, availableNodes, "no available nodes to target")

	randomNode := availableNodes[r.Intn(len(availableNodes))]
	nodeURI := tmpnet.NodeURI{
		NodeID: randomNode.NodeID,
		URI:    randomNode.GetAccessibleURI(),
	}
	tc.Log().Info("targeting random node",
		zap.Stringer("nodeID", nodeURI.NodeID),
		zap.String("uri", nodeURI.URI),
	)

	return nodeURI
}

// Retrieve the network to target for testing.
func (te *TestEnvironment) GetNetwork() *tmpnet.Network {
	tc := te.testContext
	network, err := tmpnet.ReadNetwork(tc.DefaultContext(), tc.Log(), te.NetworkDir)
	require.NoError(tc, err)
	return network
}

// Create a new keychain with the process's pre-funded key.
func (te *TestEnvironment) NewKeychain() *secp256k1fx.Keychain {
	return secp256k1fx.NewKeychain(te.PreFundedKey)
}

// Create a new private network that is not shared with other tests.
func (te *TestEnvironment) StartPrivateNetwork(network *tmpnet.Network) {
	tc := te.testContext
	require := require.New(tc)
	// Use the same configuration as the shared network
	sharedNetwork, err := tmpnet.ReadNetwork(tc.DefaultContext(), tc.Log(), te.NetworkDir)
	require.NoError(err)
	network.DefaultRuntimeConfig = sharedNetwork.DefaultRuntimeConfig

	StartNetwork(
		tc,
		network,
		te.RootNetworkDir,
		te.PrivateNetworkShutdownDelay,
		EmptyNetworkCmd,
	)
}
