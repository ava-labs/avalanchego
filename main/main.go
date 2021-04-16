// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	header = "" +
		`     _____               .__                       .__` + "\n" +
		`    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o` + "\n" +
		`   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,` + "\n" +
		`  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |` + "\n" +
		`  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\` + "\n" +
		`          \/           \/          \/     \/     \/     \/     \/`
)

// main is the entry point to AvalancheGo.
func main() {
	fmt.Println(header)

	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	// Get the config
	nodeConfig, v, err := config.GetConfig()
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

	nodeManager := newNodeManager(nodeConfig.BuildDir, log)
	_ = utils.HandleSignals(
		func(os.Signal) {
			nodeManager.shutdown()
			os.Exit(exitCode)
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	migrationManager := newMigrationManager(nodeManager, v, nodeConfig, log)
	if err := migrationManager.migrate(); err != nil {
		log.Error("error while running migration: %s", err)
		exitCode = 1
		return
	}

	log.Info("starting to run node in normal execution mode")
	exitCode, err = nodeManager.runNormal(v, nodeConfig)
	log.Debug("node manager returned exit code %s, error %v", exitCode, err)
	nodeManager.shutdown() // make sure all the nodes are stopped
}
