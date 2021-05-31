// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ava-labs/avalanchego/app/process"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// GitCommit should be optionally set at compile time.
var GitCommit string

// main is the entry point to AvalancheGo.
func main() {
	// Get the config
	nodeConfig, processConfig, err := config.GetConfigs(GitCommit)
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		os.Exit(1)
	}

	if processConfig.DisplayVersionAndExit {
		fmt.Print(processConfig.VersionStr)
		os.Exit(0)
	}

	fmt.Println(process.Header)

	// Set the log directory for this process by adding a subdirectory
	// "daemon" to the log directory given in the config
	logConfigCopy := nodeConfig.LoggingConfig
	logConfigCopy.Directory = filepath.Join(logConfigCopy.Directory, "daemon")
	logFactory := logging.NewFactory(logConfigCopy)

	log, err := logFactory.Make("main")
	if err != nil {
		logFactory.Close()

		fmt.Printf("starting logger failed with: %s\n", err)
		os.Exit(1)
	}

	log.Info("using build directory at path '%s'", processConfig.BuildDir)

	nodeManager := newNodeManager(processConfig.BuildDir, log)
	_ = utils.HandleSignals(
		func(os.Signal) {
			// SIGINT and SIGTERM cause all running nodes to stop
			nodeManager.shutdown()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	// Migrate the database if necessary
	migrationManager := newMigrationManager(nodeManager, nodeConfig, log)
	if err := migrationManager.migrate(); err != nil {
		log.Error("error while running migration: %s", err)
		nodeManager.shutdown()
	}

	// Run normally
	exitCode, err := nodeManager.runNormal()
	if err != nil {
		log.Debug("running node returned error: %s", err)
	} else {
		log.Debug("node returned exit code %d", exitCode)
	}

	nodeManager.shutdown() // make sure all the nodes are stopped

	logFactory.Close()
	os.Exit(exitCode)
}
