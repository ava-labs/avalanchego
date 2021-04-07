// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
)

// main is the primary entry point to Avalanche.
func main() {
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
}
