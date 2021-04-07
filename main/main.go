// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"time"
)

// main is the primary entry point to Avalanche.
func main() {

	// run both versions
	// exit if any of the versions are exiting
	binaryManager := NewBinaryManager()
	go binaryManager.StartOldNode()
	time.Sleep(10 * time.Second)
	go binaryManager.StartNewNode()

	for {
		select {
		case <-binaryManager.oldNodeErrChan:
			fmt.Println("old node errored")
			break
		case <-binaryManager.newNodeErrChan:
			fmt.Println("new node errored")
			break
		}
	}
}
