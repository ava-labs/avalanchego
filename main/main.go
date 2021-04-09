// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
	"os"
)

var exitCode = 0

// main is the primary entry point to Avalanche.
func main() {
	//todo handle the path retrieval
	folderPath, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	binaryManager := NewBinaryManager(folderPath)

	defer func() {
		binaryManager.KillAll()
		os.Exit(exitCode)
	}()

	// Get the config
	viper, err := config.GetViper()
	if err != nil {
		fmt.Printf("couldn't get viper: %s", err)
		exitCode = 1
		return
	}
	dbVersion := config.DBVersion
	prevDBVersion := config.PrevDBVersion
	nodeConfig, err := config.GetConfig()
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		exitCode = 1
		return
	}

	logFactory := logging.NewFactory(nodeConfig.LoggingConfig)
	defer logFactory.Close()

	log, err := logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return
	}

	// decide the run logic
	migrationManager := NewMigrationManager(binaryManager, &nodeConfig, viper, dbVersion, prevDBVersion)
	err = migrationManager.ResolveMigration()
	if err != nil {
		exitCode = 1
		return
	}

	// start apps and waits for errors
	prevVersionChan, newVersionChan := binaryManager.Start()

	for {
		select {
		case err = <-prevVersionChan:
			if migrationManager.HandleErr("previous", err) {
				continue
			}
			log.Error("previous version node errored - %v\n", err)
			exitCode = 1
			break

		case err = <-newVersionChan:
			if migrationManager.HandleErr("current", err) {
				continue
			}

			log.Error("current version node errored - %v\n", err)
			exitCode = 1
			break
		}
		break
	}
}
