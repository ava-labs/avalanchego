// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"fmt"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/app"
	"github.com/ava-labs/avalanchego/utils/subprocess"
)

func Exec(path string, args []string, forwardIO bool) (app.App, *plugin.Client, error) {
	clientConfig := &plugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          PluginMap,
		Cmd:              subprocess.New(path, args...),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:           hclog.New(&hclog.LoggerOptions{Level: hclog.Error}),
	}
	if forwardIO {
		clientConfig.SyncStdout = os.Stdout
		clientConfig.SyncStderr = os.Stderr
	}

	client := plugin.NewClient(clientConfig)
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("couldn't get client at path %s: %w", path, err)
	}

	raw, err := rpcClient.Dispense(Name)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("couldn't dispense plugin at path %s': %w", path, err)
	}

	app, ok := raw.(app.App)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("expected app.App but got %T", raw)
	}
	return app, client, nil
}
