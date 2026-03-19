// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/utils"
)

func TestVerify(t *testing.T) {
	tests := []precompiletest.ConfigVerifyTest{
		{
			Name:          "quorum numerator less than minimum",
			Config:        NewConfig(utils.PointerTo[uint64](3), WarpQuorumNumeratorMinimum-1, false),
			ExpectedErr: ErrInvalidQuorumRatio,
		},
		{
			Name:          "quorum numerator greater than quorum denominator",
			Config:        NewConfig(utils.PointerTo[uint64](3), WarpQuorumDenominator+1, false),
			ExpectedErr: ErrInvalidQuorumRatio,
		},
		{
			Name:   "default quorum numerator",
			Config: NewDefaultConfig(utils.PointerTo[uint64](3)),
		},
		{
			Name:   "valid quorum numerator 1 less than denominator",
			Config: NewConfig(utils.PointerTo[uint64](3), WarpQuorumDenominator-1, false),
		},
		{
			Name:   "valid quorum numerator 1 more than minimum",
			Config: NewConfig(utils.PointerTo[uint64](3), WarpQuorumNumeratorMinimum+1, false),
		},
		{
			Name:   "invalid cannot activated before Durango activation",
			Config: NewConfig(utils.PointerTo[uint64](3), 0, false),
			ChainConfig: func() precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(gomock.NewController(t))
				config.EXPECT().IsDurango(gomock.Any()).Return(false)
				return config
			}(),
			ExpectedErr: errWarpCannotBeActivated,
		},
	}
	precompiletest.RunVerifyTests(t, tests)
}

func TestEqualWarpConfig(t *testing.T) {
	tests := []precompiletest.ConfigEqualTest{
		{
			Name:     "non-nil config and nil other",
			Config:   NewDefaultConfig(utils.PointerTo[uint64](3)),
			Expected: false,
		},
		{
			Name:     "different type",
			Config:   NewDefaultConfig(utils.PointerTo[uint64](3)),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		{
			Name:     "different timestamp",
			Config:   NewDefaultConfig(utils.PointerTo[uint64](3)),
			Other:    NewDefaultConfig(utils.PointerTo[uint64](4)),
			Expected: false,
		},
		{
			Name:     "different quorum numerator",
			Config:   NewConfig(utils.PointerTo[uint64](3), WarpQuorumNumeratorMinimum+1, false),
			Other:    NewConfig(utils.PointerTo[uint64](3), WarpQuorumNumeratorMinimum+2, false),
			Expected: false,
		},
		{
			Name:     "same default config",
			Config:   NewDefaultConfig(utils.PointerTo[uint64](3)),
			Other:    NewDefaultConfig(utils.PointerTo[uint64](3)),
			Expected: true,
		},
		{
			Name:     "same non-default config",
			Config:   NewConfig(utils.PointerTo[uint64](3), WarpQuorumNumeratorMinimum+5, false),
			Other:    NewConfig(utils.PointerTo[uint64](3), WarpQuorumNumeratorMinimum+5, false),
			Expected: true,
		},
	}
	precompiletest.RunEqualTests(t, tests)
}
