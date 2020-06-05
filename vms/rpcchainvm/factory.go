// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"
	"os/exec"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/gecko/utils/logging"
)

var (
	errWrongVM = errors.New("wrong vm type")
)

// Factory ...
type Factory struct {
	Log  logging.Logger
	Path string
}

// New ...
func (f *Factory) New() (interface{}, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             exec.Command("sh", "-c", f.Path),
		Stderr:          f.Log,
		SyncStdout:      f.Log,
		SyncStderr:      f.Log,
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
	})

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
