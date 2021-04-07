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

	needDBUpgrade := false
	if config.DBEnabled {
		dbManager, err := manager.New(config.DBPath, logging.NoLog{}, dbVersion, true)
		if err != nil {
			fmt.Printf("couldn't create db manager at %s: %s\n", config.DBPath, err)
			return
		}
		currentDBBootstrapped, err := dbManager.CurrentDBBootstrapped()
		if err != nil {
			fmt.Printf("couldn't get if database is bootstrapped: %s", err)
			return
		}
		if currentDBBootstrapped {
			if _, exists := dbManager.Previous(); exists {
				needDBUpgrade = true
			}
		}
		if err := dbManager.Close(); err != nil {
			fmt.Printf("couldn't close database manager: %s", err)
			return
		}
	}
	fmt.Printf("upgrading database version: %v\n", needDBUpgrade)

	if !needDBUpgrade {
		viper.Set("fetch-only", "false")
		cmd := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-v1.3.2") // TODO replace with dynamic binary path
		for k, v := range viper.AllSettings() {
			cmd.Args = append(cmd.Args, fmt.Sprintf("--%s=%v", k, v))
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			fmt.Println(err)
		}
		return
	}
	oldNodeCmd := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-1.3.1/avalanchego") // TODO replace with dynamic binary path
	fmt.Println(oldNodeCmd.Dir)
	for k, v := range viper.AllSettings() {
		if k == "fetch-only" {
			continue // Don't pass new flags into an old binary
		}
		oldNodeCmd.Args = append(oldNodeCmd.Args, fmt.Sprintf("--%s=%v", k, v))
	}
	oldNodeCmd.Stdout = os.Stdout
	oldNodeCmd.Stderr = os.Stderr
	if err = oldNodeCmd.Start(); err != nil {
		fmt.Println("error from old node")
		fmt.Println(err)
	}

	newNode := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-v1.3.2") // TODO replace with dynamic binary path
	viper.Set("fetch-only", "true")
	viper.Set("bootstrap-ips", fmt.Sprintf("127.0.0.1:%d", config.StakingIP.Port)) // Bootstrap from local node when in fetch only mode
	viper.Set("bootstrap-ids", fmt.Sprintf("%s%s", constants.NodeIDPrefix, config.NodeID))
	viper.Set("http-port", config.HTTPPort+2)                          // TODO what port to use?
	viper.Set("log-dir", config.LoggingConfig.Directory+"/fetch-only") // In fetch only mode, use a different log directory
	for k, v := range viper.AllSettings() {
		newNode.Args = append(newNode.Args, fmt.Sprintf("--%s=%v", k, v))
	}
	newNode.Stdout = os.Stdout
	if err = newNode.Run(); err != nil {
		fmt.Println(err)
	}
	// cmd := exec.Command("/home/danlaine/go/src/github.com/ava-labs/avalanchego/build/avalanchego-1.3.2")
	// for k, v := range viper.AllSettings() {
	// 	cmd.Args = append(cmd.Args, fmt.Sprintf("--%s=%v", k, v))
	// }
	// cmd.Stdout = os.Stdout
	// if err = cmd.Run(); err != nil {
	// 	fmt.Println(err)
	// }
	// fmt.Println("here")
	// cmd.Wait()

	/*
		exitCode := 0
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
