// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	gomock "github.com/golang/mock/gomock"

	plugin "github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
)

// plugin_test collects objects and helpers generally helpful for various rpc tests

const (
	chainVMTestKey                                 = "chainVMTest"
	stateSyncEnabledTestKey                        = "stateSyncEnabledTest"
	getOngoingSyncStateSummaryTestKey              = "getOngoingSyncStateSummaryTest"
	getLastStateSummaryTestKey                     = "getLastStateSummaryTest"
	parseStateSummaryTestKey                       = "parseStateSummaryTest"
	getStateSummaryTestKey                         = "getStateSummaryTest"
	acceptStateSummaryTestKey                      = "acceptStateSummaryTest"
	lastAcceptedBlockPostStateSummaryAcceptTestKey = "lastAcceptedBlockPostStateSummaryAcceptTest"
)

var (
	TestHandshake = plugin.HandshakeConfig{
		ProtocolVersion:  protocolVersion,
		MagicCookieKey:   "VM_PLUGIN",
		MagicCookieValue: "dynamic",
	}

	TestClientPluginMap = map[string]plugin.Plugin{
		chainVMTestKey: &testVMPlugin{},
	}

	TestServerPluginMap = map[string]func(*testing.T, bool) (plugin.Plugin, *gomock.Controller){
		chainVMTestKey:                                 chainVMTestPlugin,
		stateSyncEnabledTestKey:                        stateSyncEnabledTestPlugin,
		getOngoingSyncStateSummaryTestKey:              getOngoingSyncStateSummaryTestPlugin,
		getLastStateSummaryTestKey:                     getLastStateSummaryTestPlugin,
		parseStateSummaryTestKey:                       parseStateSummaryTestPlugin,
		getStateSummaryTestKey:                         getStateSummaryTestPlugin,
		acceptStateSummaryTestKey:                      acceptStateSummaryTestPlugin,
		lastAcceptedBlockPostStateSummaryAcceptTestKey: lastAcceptedBlockPostStateSummaryAcceptTestPlugin,
	}
)

// helperProcess helps with creating the plugin binary for testing.
func helperProcess(s ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--"}
	cs = append(cs, s...)
	env := []string{
		"TEST_PROCESS=1",
	}
	run := os.Args[0]
	cmd := exec.Command(run, cs...)
	env = append(env, os.Environ()...)
	cmd.Env = env
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("TEST_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}

		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "failed to receive command\n")
		os.Exit(2)
	}

	plugins := make(map[string]plugin.Plugin)
	controllersList := make([]*gomock.Controller, 0, len(args))
	for _, testKey := range args {
		mockedPlugin, ctrl := TestServerPluginMap[testKey](t, true /*loadExpectations*/)
		controllersList = append(controllersList, ctrl)
		plugins[testKey] = mockedPlugin
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: TestHandshake,
		Plugins:         plugins,

		// A non-nil value here enables gRPC serving for this plugin.
		GRPCServer: grpcutils.NewDefaultServer,
	})

	for _, ctrl := range controllersList {
		ctrl.Finish()
	}
	os.Exit(0)
}
