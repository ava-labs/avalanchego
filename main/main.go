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

var (
	// GitCommit should be optionally set at compile time.
	GitCommit string
)

// main is the entry point to AvalancheGo.
func main() {
	// Get the config
	rootConfig, version, displayVersion, _, err := config.GetConfig(GitCommit)
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		os.Exit(1)
	}

	if displayVersion {
		fmt.Print(version)
		os.Exit(0)
	}

	fmt.Println(process.Header)

	// Set the log directory for this process by adding a subdirectory
	// "daemon" to the log directory given in the config
	logConfigCopy := rootConfig.LoggingConfig
	logConfigCopy.Directory = filepath.Join(logConfigCopy.Directory, "daemon")
	logFactory := logging.NewFactory(logConfigCopy)

	log, err := logFactory.Make()
	if err != nil {
		logFactory.Close()

		fmt.Printf("starting logger failed with: %s\n", err)
		os.Exit(1)
	}

	log.Info("using build directory at path '%s'", rootConfig.BuildDir)

	nodeManager := newNodeManager(rootConfig.BuildDir, log)
	_ = utils.HandleSignals(
		func(os.Signal) {
			// SIGINT and SIGTERM cause all running nodes to stop
			nodeManager.shutdown()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	// Migrate the database if necessary
	migrationManager := newMigrationManager(nodeManager, rootConfig, log)
	if err := migrationManager.migrate(); err != nil {
		log.Error("error while running migration: %s", err)
		nodeManager.shutdown()
	}

	// Run normally
	exitCode, err := nodeManager.runNormal()
	log.Debug("node returned exit code %s, error %v", exitCode, err)
	nodeManager.shutdown() // make sure all the nodes are stopped

	logFactory.Close()
	os.Exit(exitCode)
}
