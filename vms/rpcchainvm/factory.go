// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/subprocess"
	"github.com/ava-labs/avalanchego/vms"
)

var (
	errWrongVM = errors.New("wrong vm type")

	serverOptions = []grpc.ServerOption{
		grpc.MaxRecvMsgSize(math.MaxInt),
		grpc.MaxSendMsgSize(math.MaxInt),
	}
	dialOptions = []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(math.MaxInt)),
	}
)

type Factory struct {
	Path string
}

func (f *Factory) New(ctx *snow.Context) (interface{}, error) {
	config := &plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             subprocess.New(f.Path),
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
		GRPCDialOptions: dialOptions,
	}
	if ctx != nil {
		log.SetOutput(ctx.Log)
		config.Stderr = ctx.Log
		config.Logger = hclog.New(&hclog.LoggerOptions{
			Output: ctx.Log,
			Level:  hclog.Info,
		})
	} else {
		log.SetOutput(ioutil.Discard)
		config.Stderr = ioutil.Discard
		config.Logger = hclog.New(&hclog.LoggerOptions{
			Output: ioutil.Discard,
		})
	}
	client := plugin.NewClient(config)

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}

	raw, err := rpcClient.Dispense("vm")
	if err != nil {
		client.Kill()
		return nil, err
	}

	vm, ok := raw.(*VMClient)
	if !ok {
		client.Kill()
		return nil, errWrongVM
	}

	vm.SetProcess(client)
	vm.ctx = ctx
	return vm, nil
}

// RegisterPlugins iterates over a given plugin dir and registers rpcchain VMs
// for each of the discovered plugins.
func RegisterPlugins(pluginDir string, manager vms.Manager) error {
	files, err := ioutil.ReadDir(pluginDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		nameWithExtension := file.Name()
		// Strip any extension from the file. This is to support windows .exe
		// files.
		name := nameWithExtension[:len(nameWithExtension)-len(filepath.Ext(nameWithExtension))]

		// Skip hidden files.
		if len(name) == 0 {
			continue
		}

		vmID, err := manager.Lookup(name)
		if err != nil {
			// there is no alias with plugin name, try to use full vmID.
			vmID, err = ids.FromString(name)
			if err != nil {
				return fmt.Errorf("invalid vmID %s", name)
			}
		}

		_, err = manager.GetFactory(vmID)
		if err == nil {
			// If we already have the VM registered, we shouldn't attempt to
			// register it again.
			continue
		}

		// If the error isn't "not found", then we should report the error.
		if !errors.Is(err, vms.ErrNotFound) {
			return err
		}

		err = manager.RegisterFactory(
			vmID,
			&Factory{
				Path: filepath.Join(pluginDir, file.Name()),
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}
