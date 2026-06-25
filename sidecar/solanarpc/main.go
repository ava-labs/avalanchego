// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"log"
	"net"

	"google.golang.org/grpc"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

func main() {
	addr := flag.String("addr", ":9900", "address to listen on")
	solanaRPC := flag.String("solana-rpc", "https://api.mainnet-beta.solana.com", "Solana JSON-RPC endpoint")
	flag.Parse()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *addr, err)
	}

	verifier := NewSolanaVerifier(*solanaRPC, nil)
	grpcServer := grpc.NewServer()
	pb.RegisterOracleSidecarServer(grpcServer, NewServer(verifier))

	log.Printf("starting oracle sidecar (gRPC) on %s, Solana RPC: %s", *addr, *solanaRPC)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
