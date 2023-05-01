// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ethereum/go-ethereum/common"
)

var validFeeConfig = commontype.FeeConfig{
	GasLimit:        big.NewInt(8_000_000),
	TargetBlockRate: 2, // in seconds

	MinBaseFee:               big.NewInt(25_000_000_000),
	TargetGas:                big.NewInt(15_000_000),
	BaseFeeChangeDenominator: big.NewInt(36),

	MinBlockGasCost:  big.NewInt(0),
	MaxBlockGasCost:  big.NewInt(1_000_000),
	BlockGasCostStep: big.NewInt(200_000),
}

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	invalidFeeConfig := validFeeConfig
	invalidFeeConfig.GasLimit = big.NewInt(0)
	tests := map[string]testutils.ConfigVerifyTest{
		"invalid initial fee manager config": {
			Config:        NewConfig(big.NewInt(3), admins, nil, &invalidFeeConfig),
			ExpectedError: "gasLimit = 0 cannot be less than or equal to 0",
		},
		"nil initial fee manager config": {
			Config:        NewConfig(big.NewInt(3), admins, nil, &commontype.FeeConfig{}),
			ExpectedError: "gasLimit cannot be nil",
		},
	}
	allowlist.VerifyPrecompileWithAllowListTests(t, Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	enableds := []common.Address{allowlist.TestEnabledAddr}
	tests := map[string]testutils.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			Other:    precompileconfig.NewNoopStatefulPrecompileConfig(),
			Expected: false,
		},
		"different timestamp": {
			Config:   NewConfig(big.NewInt(3), admins, nil, nil),
			Other:    NewConfig(big.NewInt(4), admins, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config:   NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			Other:    NewConfig(big.NewInt(3), admins, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			Other: NewConfig(big.NewInt(3), admins, nil,
				func() *commontype.FeeConfig {
					c := validFeeConfig
					c.GasLimit = big.NewInt(123)
					return &c
				}()),
			Expected: false,
		},
		"same config": {
			Config:   NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			Other:    NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			Expected: true,
		},
	}
	allowlist.EqualPrecompileWithAllowListTests(t, Module, tests)
}
