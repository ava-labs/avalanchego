package main

import (
	"context"
	"log"

	"github.com/ava-labs/subnet-evm/cmd/simulator/worker"
)

func main() {
	c, err := worker.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	log.Fatal(worker.Run(ctx, c))
}
