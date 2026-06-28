// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/sidecar/solanarpc"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

func main() {
	addr := flag.String("addr", ":9900", "address to listen on")
	verifierType := flag.String("verifier-type", "", "verifier implementation to use (e.g. solanarpc)")
	configPath := flag.String("config-path", "", "path to verifier config file")
	flag.Parse()

	if *verifierType == "" {
		log.Fatal("--verifier-type is required")
	}

	var configBytes []byte
	if *configPath != "" {
		var err error
		configBytes, err = os.ReadFile(*configPath)
		if err != nil {
			log.Fatalf("failed to read config file %s: %v", *configPath, err)
		}
	}

	var v oracleVerifier
	switch *verifierType {
	case "solanarpc":
		sv, err := solanarpc.NewSolanaVerifier(configBytes, nil)
		if err != nil {
			log.Fatalf("invalid solanarpc config: %v", err)
		}
		v = sv
	default:
		log.Fatalf("unknown verifier type %q", *verifierType)
	}

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *addr, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterOracleSidecarServer(grpcServer, NewServer(v))

	log.Printf("starting oracle sidecar (gRPC) on %s", *addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
