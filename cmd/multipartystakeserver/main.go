// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	var (
		uri  string
		addr string
	)

	rootCmd := &cobra.Command{
		Use:   "multipartystakeserver",
		Short: "HTTP API server for multi-party staking on Avalanche P-chain",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer(uri, addr)
		},
	}
	rootCmd.Flags().StringVar(&uri, "uri", primary.LocalAPIURI, "Avalanche node URI")
	rootCmd.Flags().StringVar(&addr, "addr", ":8080", "Listen address")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(uri, addr string) error {
	handler := NewHandler(uri)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	log.Printf("Starting multi-party stake server on %s (node: %s)", addr, uri)
	fmt.Printf("Listening on %s\n", addr)
	return server.ListenAndServe()
}
