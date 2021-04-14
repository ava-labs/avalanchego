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
	binaryManager := newNodeProcessManager(nodeConfig.BuildDir, log)
	_ = utils.HandleSignals(
		func(os.Signal) {
			binaryManager.stopAll()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)
	exitCode, err = binaryManager.runNormal(v)
	fmt.Printf("exit code: %d\n", exitCode)
	fmt.Printf("err: %v\n", err)
}
