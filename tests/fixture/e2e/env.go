// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/ephnet"
	"github.com/ava-labs/avalanchego/tests/fixture/ephnet/local"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Env is used to access shared test fixture. Intended to be
// initialized from SynchronizedBeforeSuite.
var Env *TestEnvironment

func InitSharedTestEnvironment(envBytes []byte) {
	require := require.New(ginkgo.GinkgoT())
	require.Nil(Env, "env already initialized")
	Env = &TestEnvironment{
		require: require,
	}
	require.NoError(json.Unmarshal(envBytes, Env))
}

type TestEnvironment struct {
	// The directory where the test network configuration is stored
	NetworkDir string
	// URIs used to access the API endpoints of nodes of the network
	URIs []ephnet.NodeURI
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
func NewTestEnvironment(flagVars *FlagVars) *TestEnvironment {
	require := require.New(ginkgo.GinkgoT())

	networkDir := flagVars.NetworkDir()

	// Load or create a test network
	var network *local.LocalNetwork
	if len(networkDir) > 0 {
		var err error
		network, err = local.ReadNetwork(networkDir)
		require.NoError(err)
		tests.Outf("{{yellow}}Using an existing network configured at %s{{/}}\n", network.Dir)
	} else {
		network = StartLocalNetwork(flagVars.AvalancheGoExecPath(), DefaultNetworkDir)
	}

	uris := network.GetURIs()
	require.NotEmpty(uris, "network contains no nodes")
	tests.Outf("{{green}}network URIs: {{/}} %+v\n", uris)

	testDataServerURI, err := fixture.ServeTestData(fixture.TestData{
		FundedKeys: network.FundedKeys,
	})
	tests.Outf("{{green}}test data server URI: {{/}} %+v\n", testDataServerURI)
	require.NoError(err)

	return &TestEnvironment{
		NetworkDir:        network.Dir,
		URIs:              uris,
		TestDataServerURI: testDataServerURI,
	}
}

// Retrieve a random URI to naively attempt to spread API load across
// nodes.
func (te *TestEnvironment) GetRandomNodeURI() ephnet.NodeURI {
	r := rand.New(rand.NewSource(time.Now().Unix())) //#nosec G404
	nodeURI := te.URIs[r.Intn(len(te.URIs))]
	tests.Outf("{{blue}} targeting node %s with URI: %s{{/}}\n", nodeURI.NodeID, nodeURI.URI)
	return nodeURI
}

// Retrieve the network to target for testing.
func (te *TestEnvironment) GetNetwork() ephnet.Network {
	network, err := local.ReadNetwork(te.NetworkDir)
	te.require.NoError(err)
	return network
}

// Retrieve the specified number of funded keys allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKeys(count int) []*secp256k1.PrivateKey {
	keys, err := fixture.AllocateFundedKeys(te.TestDataServerURI, count)
	te.require.NoError(err)
	tests.Outf("{{blue}} allocated funded key(s): %+v{{/}}\n", keys)
	return keys
}

// Retrieve a funded key allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKey() *secp256k1.PrivateKey {
	return te.AllocateFundedKeys(1)[0]
}

// Create a new keychain with the specified number of test keys.
func (te *TestEnvironment) NewKeychain(count int) *secp256k1fx.Keychain {
	keys := te.AllocateFundedKeys(count)
	return secp256k1fx.NewKeychain(keys...)
}

// Create a new private network that is not shared with other tests.
func (te *TestEnvironment) NewPrivateNetwork() ephnet.Network {
	// Load the shared network to retrieve its path and exec path
	sharedNetwork, err := local.ReadNetwork(te.NetworkDir)
	te.require.NoError(err)

	// The private networks dir is under the shared network dir to ensure it
	// will be included in the artifact uploaded in CI.
	privateNetworksDir := filepath.Join(sharedNetwork.Dir, PrivateNetworksDirName)
	te.require.NoError(os.MkdirAll(privateNetworksDir, perms.ReadWriteExecute))

	return StartLocalNetwork(sharedNetwork.ExecPath, privateNetworksDir)
}
