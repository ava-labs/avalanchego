// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// main is the primary entry point to Avalanche.
func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	// Get the config
	viper, err := config.GetViper()
	if err != nil {
		fmt.Printf("couldn't get viper: %s", err)
		return
	}
	dbVersion := config.DBVersion
	config, err := config.GetConfig()
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		return
	}

	// Decide based on the database whether to run normally or in fetch mode
	needDBUpgrade := false
	if config.DBEnabled {
		dbManager, err := manager.New(config.DBPath, logging.NoLog{}, dbVersion, true)
		if err != nil {
			fmt.Printf("couldn't create db manager at %s: %s\n", config.DBPath, err)
			exitCode = 1
			return
		}
		currentDBBootstrapped, err := dbManager.CurrentDBBootstrapped()
		if err != nil {
			fmt.Printf("couldn't get if database is bootstrapped: %s", err)
			exitCode = 1
			return
		}
		if currentDBBootstrapped {
			if _, exists := dbManager.Previous(); exists {
				needDBUpgrade = true
			}
		}
		if err := dbManager.Close(); err != nil {
			fmt.Printf("couldn't close database manager: %s", err)
			exitCode = 1
			return
		}
	}
	fmt.Printf("upgrading database version: %v\n", needDBUpgrade)

	if !needDBUpgrade {
		// Don't need to do a database upgrade. Run normally.
		viper.Set("fetch-only", "false")                                                                                        // Ignore this flag, if it was set.
		cmd := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-v1.3.2/avalanchego-inner") // TODO replace with dynamic binary path
		for k, v := range viper.AllSettings() {                                                                                 // Pass args
			cmd.Args = append(cmd.Args, fmt.Sprintf("--%s=%v", k, v))
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			exitCode = 1
		}
		if err := cmd.Wait(); err != nil {
			exitCode = 1
		}
		return
	}

	// Need to do a database upgrade. Starting two nodes. One will run the last version
	// before the database migration. The other will run a post-database upgrade version.
	// The latter will bootstrap from the former. When it is done, both nodes will stop
	// and the post-database upgrade version restarts.
	oldNodeCmd := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-v1.3.1/avalanchego") // TODO replace with dynamic binary path
	for k, v := range viper.AllSettings() {
		if k == "fetch-only" {
			continue // Don't pass new flags into an old binary
		}
		oldNodeCmd.Args = append(oldNodeCmd.Args, fmt.Sprintf("--%s=%v", k, v))
	}
	//oldNodeCmd.Stdout = os.Stdout
	//oldNodeCmd.Stderr = os.Stderr
	if err = oldNodeCmd.Start(); err != nil {
		fmt.Println(err)
	}
	defer oldNodeCmd.Process.Signal(os.Interrupt)

	newNode := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-v1.3.2/avalanchego-inner") // TODO replace with dynamic binary path
	viper.Set("fetch-only", "true")
	viper.Set("bootstrap-ips", fmt.Sprintf("127.0.0.1:%d", config.StakingIP.Port)) // Bootstrap from local node when in fetch only mode
	viper.Set("bootstrap-ids", fmt.Sprintf("%s%s", constants.NodeIDPrefix, config.NodeID))
	viper.Set("http-port", config.HTTPPort+2)                          // TODO what port to use?
	viper.Set("log-dir", config.LoggingConfig.Directory+"/fetch-only") // In fetch only mode, use a different log directory
	for k, v := range viper.AllSettings() {
		newNode.Args = append(newNode.Args, fmt.Sprintf("--%s=%v", k, v)) // Pass args
	}
	newNode.Stdout = os.Stdout
	if err = newNode.Run(); err != nil {
		exitCode = 1
		if err := oldNodeCmd.Process.Signal(os.Interrupt); err != nil {
			exitCode = 1
		}
	}

	/*
		binaryManager := NewBinaryManager()

		defer func() {
			binaryManager.KillAll()
			os.Exit(exitCode)
		}()

		migrationManager := NewMigrationManager()

		if migrationManager.ShouldMigrate() {
			migrationManager.Migrate(binaryManager)
		}

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
					fmt.Println("previous version node errored")
					exitCode = 1
				}
				break
			}
			return
		}
	*/
}
