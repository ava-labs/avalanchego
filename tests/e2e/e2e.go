// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/onsi/gomega"

	runner_sdk "github.com/ava-labs/avalanche-network-runner-sdk"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type ClusterType byte

const (
	StandAlone ClusterType = iota
	PreExisting

	// Enough for primary.NewWallet to fetch initial UTXOs.
	DefaultWalletCreationTimeout = 5 * time.Second

	// Defines default tx confirmation timeout.
	// Enough for test/custom networks.
	DefaultConfirmTxTimeout = 20 * time.Second

	DefaultShutdownTimeout = 2 * time.Minute
)

// Env is the global struct containing all we need to test
var (
	Env = &TestEnvironment{
		testEnvironmentConfig: &testEnvironmentConfig{},
	}

	errNoKeyFile           = errors.New("test keys file not provided")
	errNotNetworkRunnerCLI = errors.New("not network-runner cli")
)

type testEnvironmentConfig struct {
	clusterType         ClusterType
	avalancheGoExecPath string
	avalancheGoLogLevel string
}

type TestEnvironment struct {
	*testEnvironmentConfig

	setupCalled bool

	runnerMu     sync.RWMutex
	runnerCli    runner_sdk.Client
	runnerGRPCEp string

	urisMu sync.RWMutex
	uris   []string

	// isNetworkPristine is set to true after network creation and
	// false after a call to GetURIsRW to know when a test needs the
	// network to be recreated. Not used if clusterType is
	// PreExisting.
	isNetworkPristine bool

	testKeysMu sync.RWMutex
	testKeys   []*secp256k1.PrivateKey
}

// Setup ensures the environment is configured with a network that can
// be targeted for testing.
func (te *TestEnvironment) Setup(
	networkRunnerClientLogLevel string,
	networkRunnerGRPCEp string,
	avalancheGoExecPath string,
	avalancheGoLogLevel string,
	testKeysFile string,
	usePersistentNetwork bool,
) error {
	// TODO(marun) Use testify instead of returning errors
	if te.setupCalled {
		return errors.New("setup has already been called and should only be called once")
	} else {
		te.setupCalled = true
	}

	// Ensure the network-runner client is initialized and can access the network-runner.
	err := te.initRunnerClient(networkRunnerClientLogLevel, networkRunnerGRPCEp)
	if err != nil {
		return err
	}

	// TODO(marun) Maybe lazy-load so that errors only fail dependent tests?
	err = te.LoadKeys(testKeysFile)
	if err != nil {
		return err
	}

	if usePersistentNetwork {
		te.clusterType = PreExisting

		tests.Outf("{{yellow}}Using a pre-existing network{{/}}\n")

		// Read the URIs for the existing network so that tests can access the nodes.

		err = te.refreshURIs()
		if err != nil {
			return err
		}
	} else {
		te.clusterType = StandAlone

		// Create a new network

		if avalancheGoExecPath != "" {
			if _, err := os.Stat(avalancheGoExecPath); err != nil {
				return fmt.Errorf("could not find avalanchego binary: %w", err)
			}
		}

		te.avalancheGoExecPath = avalancheGoExecPath
		te.avalancheGoLogLevel = avalancheGoLogLevel

		err := te.startCluster()
		if err != nil {
			return err
		}
	}

	return nil
}

// initRunnerClient configures the network-runner client and checks
// that it can reach the network-runner endpoint.
func (te *TestEnvironment) initRunnerClient(logLevel string, gRPCEp string) error {
	tests.Outf("Configuring network-runner client\n")

	if len(gRPCEp) == 0 {
		return errors.New("network runner grpc endpoint is required")
	}

	err := te.setRunnerClient(logLevel, gRPCEp)
	if err != nil {
		return fmt.Errorf("could not setup network-runner client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	presp, err := te.GetRunnerClient().Ping(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("could not ping network-runner client: %w", err)
	}

	tests.Outf("{{green}}network-runner running in PID %d{{/}}\n", presp.Pid)

	return nil
}

func (te *TestEnvironment) LoadKeys(testKeysFile string) error {
	// load test keys
	if len(testKeysFile) == 0 {
		return errNoKeyFile
	}
	testKeys, err := tests.LoadHexTestKeys(testKeysFile)
	if err != nil {
		return fmt.Errorf("failed loading test keys: %w", err)
	}
	te.setTestKeys(testKeys)
	return nil
}

// EnsurePristineNetwork attempts to ensure that the currently active
// network is compatible with tests that require a pristine
// (unmodified) state.
func (te *TestEnvironment) EnsurePristineNetwork() {
	if te.clusterType == PreExisting {
		tests.Outf("{{yellow}}unable to ensure pristine initial state with a pre-existing network.{{/}}\n")
		return
	}

	if te.isNetworkPristine {
		tests.Outf("{{green}}network is already pristine.{{/}}\n")
		return
	}

	tests.Outf("{{yellow}}network state is not pristine, the current network needs to be replaced.{{/}}\n")

	tests.Outf("{{magenta}}shutting down the current network.{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	_, err := te.GetRunnerClient().Stop(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	tests.Outf("{{green}}network shutdown successful.{{/}}\n")

	err = te.startCluster()
	gomega.Expect(err).Should(gomega.BeNil())
}

// startCluster launches a new network via the network-runner client.
func (te *TestEnvironment) startCluster() error {
	tests.Outf("{{magenta}}starting network with %q{{/}}\n", te.avalancheGoExecPath)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	resp, err := te.GetRunnerClient().Start(ctx, te.avalancheGoExecPath,
		runner_sdk.WithNumNodes(5),
		runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{"log-level":"%s"}`, te.avalancheGoLogLevel)),
	)
	cancel()
	if err != nil {
		return fmt.Errorf("could not start network: %w", err)
	}

	tests.Outf("{{green}}successfully started network: {{/}} %+v\n", resp.ClusterInfo.NodeNames)

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	_, err = te.GetRunnerClient().Health(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("could not check network health: %w", err)
	}

	tests.Outf("{{green}}network reporting health{{/}}\n")

	te.isNetworkPristine = true

	err = te.refreshURIs()
	return err
}

func (te *TestEnvironment) refreshURIs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	uriSlice, err := te.GetRunnerClient().URIs(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("could not retrieve URIs: %w", err)
	}
	te.setURIs(uriSlice)
	tests.Outf("{{green}}URIs:{{/}} %q\n", uriSlice)
	return nil
}

// setRunnerClient initializes the network-runner client that will be
// used for interacting with test networks.
func (te *TestEnvironment) setRunnerClient(logLevel string, gRPCEp string) error {
	te.runnerMu.Lock()
	defer te.runnerMu.Unlock()

	cli, err := runner_sdk.New(runner_sdk.Config{
		LogLevel:    logLevel,
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		return err
	}
	if te.runnerCli != nil {
		te.runnerCli.Close()
	}
	te.runnerCli = cli
	te.runnerGRPCEp = gRPCEp
	return err
}

func (te *TestEnvironment) GetRunnerClient() (cli runner_sdk.Client) {
	te.runnerMu.RLock()
	cli = te.runnerCli
	te.runnerMu.RUnlock()
	return cli
}

func (te *TestEnvironment) closeRunnerClient() (err error) {
	te.runnerMu.Lock()
	err = te.runnerCli.Close()
	te.runnerMu.Unlock()
	return err
}

func (te *TestEnvironment) GetRunnerGRPCEndpoint() (ep string) {
	te.runnerMu.RLock()
	ep = te.runnerGRPCEp
	te.runnerMu.RUnlock()
	return ep
}

func (te *TestEnvironment) setURIs(us []string) {
	te.urisMu.Lock()
	te.uris = us
	te.urisMu.Unlock()
}

// GetURIsRW returns the current network URIs for read and write
// operations. The network is assumed to have been modified from its
// initial state after this function has been called.
//
// TODO(marun) Find a better way of determining whether a network has
// been modified from its initial state. Or maybe ensure most (if not
// all) tests are capable of targeting any network in a
// non-pathological state?
func (te *TestEnvironment) GetURIsRW() []string {
	te.urisMu.RLock()
	us := te.uris
	te.isNetworkPristine = false
	te.urisMu.RUnlock()
	return us
}

// GetURIsRO returns the current network URIs for read-only operations.
func (te *TestEnvironment) GetURIsRO() []string {
	te.urisMu.RLock()
	us := te.uris
	te.urisMu.RUnlock()
	return us
}

func (te *TestEnvironment) setTestKeys(ks []*secp256k1.PrivateKey) {
	te.testKeysMu.Lock()
	te.testKeys = ks
	te.testKeysMu.Unlock()
}

func (te *TestEnvironment) GetTestKeys() ([]*secp256k1.PrivateKey, []ids.ShortID, *secp256k1fx.Keychain) {
	te.testKeysMu.RLock()
	testKeys := te.testKeys
	te.testKeysMu.RUnlock()
	testKeyAddrs := make([]ids.ShortID, len(testKeys))
	for i := range testKeyAddrs {
		testKeyAddrs[i] = testKeys[i].PublicKey().Address()
	}
	keyChain := secp256k1fx.NewKeychain(testKeys...)
	return testKeys, testKeyAddrs, keyChain
}

func (te *TestEnvironment) Teardown() error {
	gomega.Expect(te.setupCalled).Should(gomega.BeTrue())

	if te.clusterType == PreExisting {
		tests.Outf("{{yellow}}teardown not required for persistent network{{/}}\n")
		return nil
	}

	// Teardown the active network.

	runnerCli := te.GetRunnerClient()
	if runnerCli == nil {
		return errNotNetworkRunnerCLI
	}

	tests.Outf("{{red}}shutting down network-runner cluster{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	_, err := runnerCli.Stop(ctx)
	cancel()
	if err != nil {
		return err
	}

	tests.Outf("{{red}}shutting down network-runner client{{/}}\n")
	return te.closeRunnerClient()
}
