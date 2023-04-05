// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var TestSet Set = testSet{}

type testSet struct{}

func (testSet) GetMinimumHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (testSet) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (testSet) GetSubnetID(context.Context, ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (testSet) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

func (testSet) GetValidatorIDs(ids.ID) ([]ids.NodeID, bool) {
	return nil, false
}
func (testSet) Track(ids.ID) {}
