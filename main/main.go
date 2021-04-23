// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"path/filepath"
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

var (
	// GitCommit should be optionally set at compile time.
	GitCommit string
)

// main is the entry point to AvalancheGo.
func main() {
	fmt.Println(header)

	// Get the config
	rootConfig, v, version, displayVersion, err := config.GetConfig(GitCommit)
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		os.Exit(1)
	}

	if displayVersion {
		fmt.Print(version)
		os.Exit(0)
	}

	// Set the log directory for this process by adding a subdirectory
	// "daemon" to the log directory given in the config
	logConfigCopy := rootConfig.LoggingConfig
	logConfigCopy.Directory = filepath.Join(logConfigCopy.Directory, "daemon")
	logFactory := logging.NewFactory(logConfigCopy)
	defer logFactory.Close()

	log, err := logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		os.Exit(1)
	}
	log.Info("using build directory at path '%s'", rootConfig.BuildDir)

	nodeManager := newNodeManager(rootConfig.BuildDir, log)
	_ = utils.HandleSignals(
		func(os.Signal) {
			// SIGINT and SIGTERM cause all running nodes
			// to be ended and this program to exit with
			// exit code 0
			nodeManager.shutdown()
			os.Exit(0)
		},
		syscall.SIGINT, syscall.SIGTERM,
	)

	// Migrate the database if necessary
	migrationManager := newMigrationManager(nodeManager, v, rootConfig, log)
	if err := migrationManager.migrate(); err != nil {
		log.Error("error while running migration: %s", err)
		nodeManager.shutdown()
		os.Exit(1)
	}

	// Run normally
	log.Info("starting to run node in normal execution mode")
	exitCode, err := nodeManager.runNormal(v)
	log.Debug("node manager returned exit code %s, error %v", exitCode, err)
	nodeManager.shutdown() // make sure all the nodes are stopped
	os.Exit(exitCode)
}
