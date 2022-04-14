// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"

	"google.golang.org/grpc"

	"github.com/chain4travel/caminogo/vms/rpcchainvm/grpcutils"

	vmpb "github.com/chain4travel/caminogo/proto/pb/vm"
)

var (
	TestHandshake = plugin.HandshakeConfig{
		ProtocolVersion:  protocolVersion,
		MagicCookieKey:   "VM_PLUGIN",
		MagicCookieValue: "dynamic",
	}

	TestPluginMap = map[string]plugin.Plugin{
		"vm": &testVMPlugin{},
	}

	_ plugin.Plugin     = &testVMPlugin{}
	_ plugin.GRPCPlugin = &testVMPlugin{}
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

func TestHelperProcess(*testing.T) {
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

	pluginLogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
	})

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: TestHandshake,
		Plugins: map[string]plugin.Plugin{
			"vm": NewTestVM(&TestSubnetVM{logger: pluginLogger}),
		},

		GRPCServer: grpcutils.NewDefaultServer,
	})
	os.Exit(0)
}

type testVMPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	vm TestVM
}

func NewTestVM(vm *TestSubnetVM) plugin.Plugin {
	return &testVMPlugin{vm: vm}
}

func (p *testVMPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	vmpb.RegisterVMServer(s, NewTestServer(p.vm))
	return nil
}

func (p *testVMPlugin) GRPCClient(ctx context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return NewTestClient(vmpb.NewVMClient(c)), nil
}
