package main

import (
	"fmt"
	"os"
	"os/exec"
)

type BinaryManager struct {
	oldNodeErrChan chan error
	newNodeErrChan chan error
}

func NewBinaryManager() *BinaryManager {
	return &BinaryManager{
		oldNodeErrChan: make(chan error),
		newNodeErrChan: make(chan error),
	}
}

func (b *BinaryManager) StartOldNode() {
	cmd := exec.Command("/Users/pedro/go/src/github.com/ava-labs/avalanchego/build/avalanchego")

	cmd.Stdout = os.Stdout

	// start the command after having set up the pipe
	if err := cmd.Start(); err != nil {
		fmt.Println(err)
	}

	if err := cmd.Wait(); err != nil {
		b.oldNodeErrChan <- err
	}
}

func (b *BinaryManager) StartNewNode() {
	cmd := exec.Command("/Users/pedro/go/src/github.com/ava-labs/avalanchego-internal/build/avalanchego-1.3.2")

	cmd.Stdout = os.Stdout

	// start the command after having set up the pipe
	if err := cmd.Start(); err != nil {
		fmt.Println(err)
	}

	if err := cmd.Wait(); err != nil {
		b.oldNodeErrChan <- err
	}
}
