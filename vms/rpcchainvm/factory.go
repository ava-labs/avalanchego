// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/subprocess"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
)

var (
	errWrongVM = errors.New("wrong vm type")

	_ vms.Factory = (*factory)(nil)
)

type factory struct {
	path           string
	processTracker resource.ProcessTracker
}

func NewFactory(path string, processTracker resource.ProcessTracker) vms.Factory {
	return &factory{
		path:           path,
		processTracker: processTracker,
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
	// createStaticHandlers will send a nil ctx to disable logs
	// TODO: create a separate log file and no-op ctx
	if ctx != nil {
		config.Stderr = ctx.Log
		config.Logger = hclog.New(&hclog.LoggerOptions{
			Output: ctx.Log,
			Level:  hclog.Info,
		})
	} else {
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

	vm.SetProcess(ctx, client, f.processTracker)
	return vm, nil
}
