// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/subprocess"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
)

var (
	errWrongVM         = errors.New("wrong vm type")
	_          Factory = &factory{}
)

type Factory interface {
	// New returns an instance of a virtual machine.
	New(*snow.Context) (interface{}, error)
}

type factory struct {
	path string
}

func NewFactory(path string) Factory {
	return &factory{
		path: path,
	}
}

func (f *factory) New(ctx *snow.Context) (interface{}, error) {
	config := &plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             subprocess.New(f.path),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolGRPC,
		},
		// We kill this client by calling kill() when the chain running this VM
		// shuts down. However, there are some cases where the VM's Shutdown
		// method is not called. Namely, if:
		// 1) The node shuts down after the client is created but before the
		//    chain is registered with the message router.
		// 2) The chain doesn't handle a shutdown message before the node times
		//    out on the chain's shutdown and dies, leaving the shutdown message
		//    unhandled.
		// We set managed to true so that we can call plugin.CleanupClients on
		// node shutdown to ensure every plugin subprocess is killed.
		Managed:         true,
		GRPCDialOptions: grpcutils.DefaultDialOptions,
	}
	if ctx != nil {
		log.SetOutput(ctx.Log)
		config.Stderr = ctx.Log
		config.Logger = hclog.New(&hclog.LoggerOptions{
			Output: ctx.Log,
			Level:  hclog.Info,
		})
	} else {
		log.SetOutput(io.Discard)
		config.Stderr = io.Discard
		config.Logger = hclog.New(&hclog.LoggerOptions{
			Output: io.Discard,
		})
	}
	client := plugin.NewClient(config)

	pluginName := filepath.Base(f.path)
	pluginErr := func(err error) error {
		return fmt.Errorf("plugin: %q: %w", pluginName, err)
	}

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, pluginErr(err)
	}

	raw, err := rpcClient.Dispense("vm")
	if err != nil {
		client.Kill()
		return nil, pluginErr(err)
	}

	vm, ok := raw.(*VMClient)
	if !ok {
		client.Kill()
		return nil, pluginErr(errWrongVM)
	}

	vm.SetProcess(client)
	vm.ctx = ctx
	return vm, nil
}
