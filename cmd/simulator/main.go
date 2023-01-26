// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ava-labs/subnet-evm/cmd/simulator/worker"
	"github.com/spf13/cobra"
)

func init() {
	cobra.EnablePrefixMatching = true
}

func main() {
	rootCmd := newCommand()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "simulator failed %v\n", err)
		os.Exit(1)
	}
}

var (
	version     = "v0.0.1"
	versionFlag bool
	timeout     time.Duration
	keysDir     string

	rpcEndpoints []string

	concurrency int
	baseFee     uint64
	priorityFee uint64

	defaultLocalNetworkCChainEndpoints = []string{
		"http://127.0.0.1:9650/ext/bc/C/rpc",
		"http://127.0.0.1:9652/ext/bc/C/rpc",
		"http://127.0.0.1:9654/ext/bc/C/rpc",
		"http://127.0.0.1:9656/ext/bc/C/rpc",
		"http://127.0.0.1:9658/ext/bc/C/rpc",
	}
)

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "simulator",
		Short:      "Load simulator for subnet-evm + C-chain",
		SuggestFor: []string{"simulators"},
		Run:        runFunc,
	}

	cmd.PersistentFlags().BoolVarP(&versionFlag, "version", "v", false, "Print the version of the simulator and exit.")
	cmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", time.Minute, "Duration to run simulator")
	cmd.PersistentFlags().StringVarP(&keysDir, "keys", "k", ".simulator/keys", "Directory to find key files")
	cmd.PersistentFlags().StringSliceVarP(&rpcEndpoints, "rpc-endpoints", "e", defaultLocalNetworkCChainEndpoints, `Specifies a comma separated list of RPC Endpoints to use for the load test. Ex. "http://127.0.0.1:9650/ext/bc/C/rpc,http://127.0.0.1:9652/ext/bc/C/rpc". Defaults to the default RPC Endpoints for the C-Chain on a 5 Node local network.`)
	cmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "c", 10, "Number of concurrent workers to use during the load test")
	cmd.PersistentFlags().Uint64VarP(&baseFee, "base-fee", "f", 25, "Base fee to use for each transaction issued into the load test")
	cmd.PersistentFlags().Uint64VarP(&priorityFee, "priority-fee", "p", 1, "Priority fee to use for each transaction issued into the load test")

	return cmd
}

func runFunc(cmd *cobra.Command, args []string) {
	if versionFlag {
		fmt.Printf("%s\n", version)
		return
	}
	// TODO: use geth logger
	log.Printf("launching simulator with rpc endpoints %q timeout %v, concurrency %d, base fee %d, priority fee %d",
		rpcEndpoints, timeout, concurrency, baseFee, priorityFee)

	cfg := &worker.Config{
		Endpoints:   rpcEndpoints,
		Concurrency: concurrency,
		BaseFee:     baseFee,
		PriorityFee: priorityFee,
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	errc := make(chan error)
	go func() {
		errc <- worker.Run(ctx, cfg, keysDir)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case sig := <-sigs:
		log.Printf("received OS signal %v; canceling context", sig.String())
		cancel()
	case err := <-errc:
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			log.Fatalf("worker.Run returned an error %v", err)
		}
	}
}
