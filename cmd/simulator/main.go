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
	"sigs.k8s.io/yaml"
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
	timeout time.Duration
	keysDir string

	networkRunnerOutputPath string
	rpcEndpoints            []string

	concurrency int
	baseFee     uint64
	priorityFee uint64
)

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "simulator",
		Short:      "Load simulator for subnet-evm + C-chain",
		SuggestFor: []string{"simulators"},
		Run:        runFunc,
	}

	cmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", time.Minute, "Duration to run simulator")
	cmd.PersistentFlags().StringVarP(&keysDir, "keys", "k", ".simulator/keys", "Directory for key files")
	cmd.PersistentFlags().StringVarP(&networkRunnerOutputPath, "network-runner-output", "o", "", "If non-empty, it loads the endpoints from the YAML and overwrites --config")
	cmd.PersistentFlags().StringSliceVarP(&rpcEndpoints, "endpoints", "e", nil, "If non-empty, it loads the endpoints from the YAML and overwrites --config")
	cmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "c", 10, "Concurrency")
	cmd.PersistentFlags().Uint64VarP(&baseFee, "base-fee", "f", 25, "Base fee")
	cmd.PersistentFlags().Uint64VarP(&priorityFee, "priority-fee", "p", 1, "Base fee")

	return cmd
}

func runFunc(cmd *cobra.Command, args []string) {
	log.Printf("launching simulator with rpc endpoints %q or network runner output %q, timeout %v, concurrentcy %d, base fee %d, priority fee %d",
		rpcEndpoints, networkRunnerOutputPath, timeout, concurrency, baseFee, priorityFee)

	cfg := &worker.Config{
		Endpoints:   rpcEndpoints,
		Concurrency: concurrency,
		BaseFee:     baseFee,
		PriorityFee: priorityFee,
	}

	if networkRunnerOutputPath != "" {
		log.Printf("loading network runner output %q", networkRunnerOutputPath)
		b, err := os.ReadFile(networkRunnerOutputPath)
		if err != nil {
			log.Fatalf("failed to read network-runner output %v", err)
		}
		var ci networkRunnerClusterInfo
		if err = yaml.Unmarshal(b, &ci); err != nil {
			log.Fatalf("failed to parse network-runner output %v", err)
		}

		eps := make([]string, len(ci.URIs))
		for i := range eps {
			/*
			   e.g.,

			   uris:
			   - http://127.0.0.1:32945
			   - http://127.0.0.1:38948
			   - http://127.0.0.1:47203
			   - http://127.0.0.1:54708
			   - http://127.0.0.1:64435
			   endpoint: /ext/bc/oFzgVk4nzHApgcBAPXa7JLX5mhqAJnxQkiYD915tZ6LMPcPRu
			*/
			eps[i] = ci.URIs[i] + ci.Endpoint + "/rpc"
		}
		cfg.Endpoints = eps
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

type networkRunnerClusterInfo struct {
	URIs     []string `json:"uris"`
	Endpoint string   `json:"endpoint"`
}
