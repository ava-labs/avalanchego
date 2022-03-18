// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"sync"
	"time"

	runner_client "github.com/ava-labs/avalanche-network-runner/client"
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
	runnerCli    runner_client.Client
	runnerGRPCEp string
)

func SetRunnerClient(logLevel string, gRPCEp string) (cli runner_client.Client, err error) {
	runnerMu.Lock()
	defer runnerMu.Unlock()

	cli, err = runner_client.New(runner_client.Config{
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

func GetRunnerClient() (cli runner_client.Client) {
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
	execPathMu sync.RWMutex
	execPath   string
)

func SetExecPath(p string) {
	execPathMu.Lock()
	execPath = p
	execPathMu.Unlock()
}

func GetExecPath() (p string) {
	execPathMu.RLock()
	p = execPath
	execPathMu.RUnlock()
	return p
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
