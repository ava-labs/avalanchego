// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

// This fetches the current validator set of both Fuji and Mainnet.
func main() {
	ctx := context.Background()

	fujiValidators, err := getCurrentValidators(ctx, primary.FujiAPIURI)
	if err != nil {
		log.Fatalf("failed to fetch Fuji validators: %v", err)
	}

	mainnetValidators, err := getCurrentValidators(ctx, primary.MainnetAPIURI)
	if err != nil {
		log.Fatalf("failed to fetch Mainnet validators: %v", err)
	}

	validators := map[string]set.Set[ids.NodeID]{
		constants.FujiName:    fujiValidators,
		constants.MainnetName: mainnetValidators,
	}
	validatorsJSON, err := json.MarshalIndent(validators, "", "\t")
	if err != nil {
		log.Fatalf("failed to marshal validators: %v", err)
	}

	if err := perms.WriteFile("validators.json", validatorsJSON, perms.ReadWrite); err != nil {
		log.Fatalf("failed to write validators: %v", err)
	}
}

func getCurrentValidators(ctx context.Context, uri string) (set.Set[ids.NodeID], error) {
	client := platformvm.NewClient(uri)
	currentValidators, err := client.GetCurrentValidators(
		ctx,
		constants.PrimaryNetworkID,
		nil, // fetch all validators
	)
	if err != nil {
		return nil, err
	}

	var nodeIDs set.Set[ids.NodeID]
	for _, validator := range currentValidators {
		nodeIDs.Add(validator.NodeID)
	}
	return nodeIDs, nil
}
