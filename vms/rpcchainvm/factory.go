// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"fmt"
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
type Factory struct {
	Path   string
	Config string
}

// New ...
func (f *Factory) New(ctx *snow.Context) (interface{}, error) {
	// Ignore warning from launching an executable with a variable command
	// because the command is a controlled and required input
	// #nosec G204
	config := &plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             exec.Command(f.Path, fmt.Sprintf("--config=%s", f.Config)),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
		// We kill this client by calling kill() when the chain running this VM shuts down.
		// However, there are some cases where the VM's Shutdown method is not called.
		// Namely, if:
		// 1) The node shuts down after the client is created but before the
		//    chain is registered with the message router.
		// 2) The chain doesn't handle a shutdown message before the node times out on the
		//    chain's shutdown and dies, leaving the shutdown message unhandled.
		// We set managed to true so that we can call plugin.CleanupClients on node shutdown
		// to ensure every plugin subprocess is killed.
		Managed: true,
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
