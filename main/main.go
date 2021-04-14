// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"github.com/hashicorp/go-plugin"
	"os"
	"os/exec"
	"syscall"

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
	binaryManager := newBinaryManager(nodeConfig.BuildDir, log)

	_ = utils.HandleSignals(
		func(os.Signal) {
			binaryManager.killAll()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	// Get the config
	_, err = config.GetViper()
	if err != nil {
		log.Fatal("couldn't get viper: %s", err)
		exitCode = 1
		return
	}

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
	// #nosec G204
	config := &plugin.ClientConfig{
		HandshakeConfig: appplugin.Handshake,
		Plugins:         appplugin.PluginMap,
		Cmd:             exec.Command("/Users/pedro/go/src/github.com/ava-labs/avalanchego-internal/build/avalanchego-v1.3.2/avalanchego-inner", "--log-level=info"),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
	}
	// TODO figure out how to pipe the log output from avalanchego to std out
	////log.SetOutput(logger)
	//config.Stderr = os.Stderr
	//config.Logger = hclog.New(&hclog.LoggerOptions{
	//	Output: logger,
	//})
	client := plugin.NewClient(config)
	defer client.Kill()
	rpcClient, err := client.Client()
	if err != nil {
		fmt.Println(err)
		return
	}
	raw, err := rpcClient.Dispense("node")
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
