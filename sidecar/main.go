// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/sidecar/config"
	"github.com/ava-labs/avalanchego/sidecar/evmrpc"
	"github.com/ava-labs/avalanchego/sidecar/solanarpc"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

// Add a case here when registering a new source type in
// network/p2p/oracle/message.go (KnownSourceTypes).
func buildVerifier(sourceType string, body json.RawMessage) (oracleVerifier, error) {
	switch sourceType {
	case oracle.SourceTypeSolana:
		return solanarpc.NewSolanaVerifier(body, nil)
	case oracle.SourceTypeEVM:
		return evmrpc.NewEVMVerifier(body, nil)
	default:
		return nil, fmt.Errorf("no verifier implementation for source type %q", sourceType)
	}
}

func main() {
	addr := flag.String("addr", "", "address to listen on (overrides bind_addr in config)")
	configPath := flag.String("config-path", "", "path to sidecar config file (required)")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("--config-path is required")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load sidecar config: %v", err)
	}

	verifiers := make(map[string]oracleVerifier, len(cfg.Verifiers))
	for sourceType, body := range cfg.Verifiers {
		if !oracle.IsKnownSourceType(sourceType) {
			log.Fatalf("sidecar config declares unknown source type %q; register it in network/p2p/oracle first", sourceType)
		}
		v, err := buildVerifier(sourceType, body)
		if err != nil {
			log.Fatalf("failed to build %q verifier: %v", sourceType, err)
		}
		verifiers[sourceType] = v
	}

	bindAddr := cfg.BindAddr
	if *addr != "" {
		bindAddr = *addr
	}
	if bindAddr == "" {
		log.Fatal("no bind address: set --addr or bind_addr in config")
	}

	lis, err := net.Listen("tcp", bindAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", bindAddr, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterOracleSidecarServer(grpcServer, NewServer(verifiers))

	log.Printf("starting oracle sidecar (gRPC) on %s with verifiers %v", bindAddr, keys(verifiers))
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func keys(m map[string]oracleVerifier) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
