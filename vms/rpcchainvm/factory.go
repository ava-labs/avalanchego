// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"io/ioutil"
	"log"
	"os/exec"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

var (
	errWrongVM = errors.New("wrong vm type")
)

// Factory ...
type Factory struct{ Path string }

// New ...
func (f *Factory) New(ctx *snow.Context) (interface{}, error) {
	// Ignore warning from launching an executable with a variable command
	// because the command is a controlled and required input
	// #nosec G204
	config := &plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             exec.Command(f.Path),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
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
	return vm, nil
}
