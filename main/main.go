// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/config"
)

// main is the primary entry point to Avalanche.
func main() {
	exitCode := 0
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
		return
	}
	dbVersion := config.DBVersion
	prevDBVersion := config.PrevDBVersion
	nodeConfig, err := config.GetConfig()
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
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
		case err := <-prevVersionChan:
			if err != nil {
				fmt.Println("previous version node errored")
				exitCode = 1
			}
			break
		case err := <-newVersionChan:
			if err != nil {
				fmt.Println("current version node errored")
				exitCode = 1
			}
			break
		}
	}
}
