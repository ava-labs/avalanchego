// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"

	"github.com/ava-labs/subnet-evm/cmd/simulator/worker"
)

func main() {
	ctx := context.Background()
	log.Fatal(worker.Run(ctx))
}
