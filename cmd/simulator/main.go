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
