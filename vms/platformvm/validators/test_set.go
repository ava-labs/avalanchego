// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var _ validators.State = (*TestSet)(nil)

type TestSet struct{}

func (*TestSet) GetMinimumHeight(_ context.Context) (uint64, error) {
	return 0, nil
}

func (*TestSet) GetCurrentHeight(_ context.Context) (uint64, error) {
	return 0, nil
}

func (*TestSet) GetSubnetID(_ context.Context, _ ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (*TestSet) GetValidatorSet(_ context.Context, _ uint64, _ ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

func (*TestSet) GetValidatorIDs(_ ids.ID) ([]ids.NodeID, bool) {
	return nil, false
}

func (*TestSet) Track(_ ids.ID) {}
