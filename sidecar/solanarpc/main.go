// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", ":9900", "address to listen on")
	solanaRPC := flag.String("solana-rpc", "https://api.mainnet-beta.solana.com", "Solana JSON-RPC endpoint")
	flag.Parse()

	verifier := NewSolanaVerifier(*solanaRPC, nil)
	srv := &http.Server{
		Addr:         *addr,
		Handler:      NewServer(verifier),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("starting oracle sidecar on %s, Solana RPC: %s", *addr, *solanaRPC)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
