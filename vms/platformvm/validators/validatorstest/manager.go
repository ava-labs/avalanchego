// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validatorstest

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"

	snowvalidators "github.com/ava-labs/avalanchego/snow/validators"
	vmvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

var Manager vmvalidators.Manager = testManager{}

type testManager struct{}

func (testManager) GetMinimumHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (testManager) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (testManager) GetSubnetID(context.Context, ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (testManager) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*snowvalidators.GetValidatorOutput, error) {
	return nil, nil
}

func (testManager) OnAcceptedBlockID(ids.ID) {}
