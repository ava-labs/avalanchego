// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// relayer carries PChain.sol warp messages to the P-chain.
//
//	go run ./tests/warpauth/relayer -node http://127.0.0.1:9650 -helper 0x...
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	warpclient "github.com/ava-labs/avalanchego/graft/coreth/warp"
	"github.com/ava-labs/avalanchego/tests/warpauth"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
)

func main() {
	node := flag.String("node", "http://127.0.0.1:9650", "avalanchego API base URI")
	helper := flag.String("helper", "", "PChain.sol address on the C-chain")
	fromBlock := flag.Uint64("from-block", 0, "first C-chain block to scan")
	flag.Parse()
	if *helper == "" {
		fmt.Fprintln(os.Stderr, "-helper is required")
		os.Exit(1)
	}

	eth, err := ethclient.Dial(*node + "/ext/bc/C/rpc")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	warpClient, err := warpclient.NewClient(*node, "C")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := logging.NewLogger("relayer", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Plain.ConsoleEncoder()))
	r := &warpauth.Relayer{
		Log:    log,
		Eth:    eth,
		Warp:   warpClient,
		PChain: platformvm.NewClient(*node),
		Helper: common.HexToAddress(*helper),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := r.Run(ctx, *fromBlock); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
