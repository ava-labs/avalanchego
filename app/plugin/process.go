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

package plugin

import (
	"fmt"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/chain4travel/caminogo/app"
	"github.com/chain4travel/caminogo/utils/subprocess"
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
