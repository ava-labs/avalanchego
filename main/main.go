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
	"github.com/ava-labs/avalanchego/version"
)

var (
	previousVersion = version.NewDefaultVersion(1, 3, 1)
	currentVersion  = version.NewDefaultVersion(1, 3, 2) // TODO only have 1 copy of this
)

// main is the primary entry point to Avalanche.
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

	//TODO handle the path retrieval
	folderPath, err := os.Getwd()
	if err != nil {
		log.Fatal("couldn't get working directory: %s", err)
		exitCode = 1
		return
	}
	binaryManager := newBinaryManager(folderPath, log)
	log.Info("folder path: %s", folderPath) // todo remove this

	_ = utils.HandleSignals(
		func(os.Signal) {
			binaryManager.killAll()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	// Get the config
	v, err := config.GetViper()
	if err != nil {
		log.Fatal("couldn't get viper: %s", err)
		exitCode = 1
		return
	}

	migrationManager := newMigrationManager(binaryManager, v, nodeConfig, log)
	if err := migrationManager.migrate(); err != nil {
		log.Error("error while running migration: %s", err)
		exitCode = 1
		return
	}

	if err := binaryManager.runNormal(v); err != nil {
		exitCode = 1
		return
	}
}
