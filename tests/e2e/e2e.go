// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"sync"
	"time"

	runner_sdk "github.com/ava-labs/avalanche-network-runner-sdk"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	// Enough for primary.NewWallet to fetch initial UTXOs.
	DefaultWalletCreationTimeout = 5 * time.Second

	// Defines default tx confirmation timeout.
	// Enough for test/custom networks.
	DefaultConfirmTxTimeout = 10 * time.Second
)

var (
	runnerMu     sync.RWMutex
	runnerCli    runner_sdk.Client
	runnerGRPCEp string
)

func SetRunnerClient(logLevel string, gRPCEp string) (cli runner_sdk.Client, err error) {
	runnerMu.Lock()
	defer runnerMu.Unlock()

	cli, err = runner_sdk.New(runner_sdk.Config{
		LogLevel:    logLevel,
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if runnerCli != nil {
		runnerCli.Close()
	}
	runnerCli = cli
	runnerGRPCEp = gRPCEp
	return cli, err
}

func GetRunnerClient() (cli runner_sdk.Client) {
	runnerMu.RLock()
	cli = runnerCli
	runnerMu.RUnlock()
	return cli
}

func CloseRunnerClient() (err error) {
	runnerMu.Lock()
	err = runnerCli.Close()
	runnerMu.Unlock()
	return err
}

func GetRunnerGRPCEndpoint() (ep string) {
	runnerMu.RLock()
	ep = runnerGRPCEp
	runnerMu.RUnlock()
	return ep
}

var (
	urisMu sync.RWMutex
	uris   []string
)

func SetURIs(us []string) {
	urisMu.Lock()
	uris = us
	urisMu.Unlock()
}

func GetURIs() []string {
	urisMu.RLock()
	us := uris
	urisMu.RUnlock()
	return us
}

var (
	testKeysMu sync.RWMutex
	testKeys   []*crypto.PrivateKeySECP256K1R
)

func SetTestKeys(ks []*crypto.PrivateKeySECP256K1R) {
	testKeysMu.Lock()
	testKeys = ks
	testKeysMu.Unlock()
}

func GetTestKeys() []*crypto.PrivateKeySECP256K1R {
	testKeysMu.RLock()
	ks := testKeys
	testKeysMu.RUnlock()
	return ks
}

func LoadTestKeys() ([]*crypto.PrivateKeySECP256K1R, []ids.ShortID, *secp256k1fx.Keychain) {
	testKeys := GetTestKeys()
	testKeyAddrs := make([]ids.ShortID, len(testKeys))
	for i := range testKeyAddrs {
		testKeyAddrs[i] = testKeys[i].PublicKey().Address()
	}
	keyChain := secp256k1fx.NewKeychain(testKeys...)
	return testKeys, testKeyAddrs, keyChain
}

var (
	enableWhitelistTxTestMu sync.RWMutex
	enableWhitelistTxTests  bool
)

func SetEnableWhitelistTxTests(b bool) {
	enableWhitelistTxTestMu.Lock()
	enableWhitelistTxTests = b
	enableWhitelistTxTestMu.Unlock()
}

func GetEnableWhitelistTxTests() (b bool) {
	enableWhitelistTxTestMu.RLock()
	b = enableWhitelistTxTests
	enableWhitelistTxTestMu.RUnlock()
	return b
}
