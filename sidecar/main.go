// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"log"
	"net"

	"google.golang.org/grpc"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
	"github.com/ava-labs/avalanchego/sidecar/solanarpc"
)

func main() {
	addr := flag.String("addr", ":9900", "address to listen on")
	sourceType := flag.String("source-type", "solana", "source chain type (solana)")
	solanaRPC := flag.String("solana-rpc", "https://api.mainnet-beta.solana.com", "Solana JSON-RPC endpoint (used when --source-type=solana)")
	flag.Parse()

	var v oracleVerifier
	switch *sourceType {
	case "solana":
		v = solanarpc.NewSolanaVerifier(*solanaRPC, nil)
	default:
		log.Fatalf("unknown source type %q", *sourceType)
	}

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *addr, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterOracleSidecarServer(grpcServer, NewServer(v))

	log.Printf("starting oracle sidecar (gRPC) on %s, source-type: %s", *addr, *sourceType)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
