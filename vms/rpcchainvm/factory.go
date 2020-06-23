// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"io/ioutil"
	"log"
	"os/exec"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/gecko/snow"
)

var (
	errWrongVM = errors.New("wrong vm type")
)

// Factory ...
type Factory struct{ Path string }

// New ...
func (f *Factory) New(ctx *snow.Context) (interface{}, error) {
	config := &plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             exec.Command("sh", "-c", f.Path),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
	}
	if ctx != nil {
		// disable go-plugin logging (since it is not controlled by Gecko's own
		// logging facility)
		log.SetOutput(ioutil.Discard)
		config.Stderr = ctx.Log
		config.SyncStdout = ctx.Log
		config.SyncStderr = ctx.Log
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
	return vm, nil
}
