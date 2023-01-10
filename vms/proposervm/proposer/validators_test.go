// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestValidatorDataLess(t *testing.T) {
	require := require.New(t)

	v1 := validatorData{
		GetValidatorOutput: &validators.GetValidatorOutput{},
	}
	v2 := validatorData{
		GetValidatorOutput: &validators.GetValidatorOutput{},
	}
	require.False(v1.Less(v2))
	require.False(v2.Less(v1))

	v1 = validatorData{
		GetValidatorOutput: &validators.GetValidatorOutput{
			NodeID: ids.NodeID{1},
		},
	}
	require.False(v1.Less(v2))
	require.True(v2.Less(v1))
}
