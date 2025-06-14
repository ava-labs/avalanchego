// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Simple ConnectRPC client for testing the Info API
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	infov1connect "github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	v1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"
	"github.com/bufbuild/connect-go"
	"google.golang.org/protobuf/types/known/emptypb"
)

func main() {
	// Default to localhost, but allow overriding via command line
	nodeURL := "http://localhost:9650"
	if len(os.Args) > 1 {
		nodeURL = os.Args[1]
	}

	// Create a client
	client := infov1connect.NewInfoServiceClient(
		http.DefaultClient,
		nodeURL,
	)

	// Request node version
	fmt.Println("Fetching node version...")
	versionResp, err := client.GetNodeVersion(
		context.Background(),
		connect.NewRequest(&emptypb.Empty{}),
	)
	if err != nil {
		fmt.Printf("Error getting node version: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Node Version: %s\n", versionResp.Msg.Version)
	fmt.Printf("Database Version: %s\n", versionResp.Msg.DatabaseVersion)
	fmt.Printf("RPC Protocol Version: %d\n", versionResp.Msg.RpcProtocolVersion)
	fmt.Printf("Git Commit: %s\n", versionResp.Msg.GitCommit)
	fmt.Println("VM Versions:")
	for id, version := range versionResp.Msg.VmVersions {
		fmt.Printf("  %s: %s\n", id, version)
	}
	
	// Get network name
	networkResp, err := client.GetNetworkName(
		context.Background(),
		connect.NewRequest(&emptypb.Empty{}),
	)
	if err != nil {
		fmt.Printf("Error getting network name: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("\nNetwork Name: %s\n", networkResp.Msg.NetworkName)
	
	// Check if X-Chain is bootstrapped
	fmt.Println("\nChecking if X-Chain is bootstrapped...")
	bootstrapArgs := connect.NewRequest(&v1.IsBootstrappedArgs{
		Chain: "X",
	})
	bootstrapResp, err := client.IsBootstrapped(
		context.Background(),
		bootstrapArgs,
	)
	if err != nil {
		fmt.Printf("Error checking bootstrap status: %v\n", err)
	} else {
		fmt.Printf("X-Chain is bootstrapped: %v\n", bootstrapResp.Msg.IsBootstrapped)
	}
}
