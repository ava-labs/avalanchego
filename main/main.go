// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// main is the entry point to AvalancheGo.
func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	// Get the config
	nodeConfig, err := config.GetConfig()
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		exitCode = 1
		return
	}

	nodeConfig.LoggingConfig.Directory += "/daemon"
	logFactory := logging.NewFactory(nodeConfig.LoggingConfig)
	defer logFactory.Close()

	log, err := logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		exitCode = 1
		return
	}
	binaryManager := newNodeProcessManager(nodeConfig.BuildDir, log)

	_ = utils.HandleSignals(
		func(os.Signal) {
			binaryManager.killAll()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	//migrationManager := newMigrationManager(binaryManager, v, nodeConfig, log)
	//if err := migrationManager.migrate(); err != nil {
	//	log.Error("error while running migration: %s", err)
	//	exitCode = 1
	//	return
	//}
	//
	//if err := binaryManager.runNormal(v); err != nil {
	//	exitCode = 1
	//	return
	//}

	// Ignore warning from launching an executable with a variable command
	// because the command is a controlled and required input

	v, err := config.GetViper()
	if err != nil {
		fmt.Printf("couldn't get viper: %s\n", err)
		exitCode = 1
		return
	}
	args := []string{}
	for k, v := range v.AllSettings() {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	// #nosec G204
	config := &plugin.ClientConfig{
		HandshakeConfig: appplugin.Handshake,
		Plugins:         appplugin.PluginMap,
		Cmd:             exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-v1.3.2/avalanchego-inner", args...),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
		SyncStdout: os.Stdout,
		SyncStderr: os.Stderr,
		Logger:     hclog.New(&hclog.LoggerOptions{Level: hclog.Error}),
	}

	client := plugin.NewClient(config)
	defer client.Kill()
	rpcClient, err := client.Client()
	if err != nil {
		fmt.Println(err)
		return
	}
	raw, err := rpcClient.Dispense("nodeProcess")
	if err != nil {
		fmt.Println(err)
		return
	}
	node, ok := raw.(*appplugin.Client)
	if !ok {
		fmt.Printf("expected *node.NodeClient but got %T", raw)
		return
	}
	fmt.Println(node.Start()) // TODO remove this is just here for testing
}
