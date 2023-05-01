// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	enableds := []common.Address{allowlist.TestEnabledAddr}
	tests := map[string]testutils.ConfigVerifyTest{
		"invalid allow list config in native minter allowlist": {
			Config:        NewConfig(big.NewInt(3), admins, admins, nil),
			ExpectedError: "cannot set address",
		},
		"duplicate admins in config in native minter allowlist": {
			Config:        NewConfig(big.NewInt(3), append(admins, admins[0]), enableds, nil),
			ExpectedError: "duplicate address",
		},
		"duplicate enableds in config in native minter allowlist": {
			Config:        NewConfig(big.NewInt(3), admins, append(enableds, enableds[0]), nil),
			ExpectedError: "duplicate address",
		},
		"nil amount in native minter config": {
			Config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): nil,
				}),
			ExpectedError: "initial mint cannot contain nil",
		},
		"negative amount in native minter config": {
			Config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(-1),
				}),
			ExpectedError: "initial mint cannot contain invalid amount",
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
		"different timestamps": {
			Config:   NewConfig(big.NewInt(3), admins, nil, nil),
			Other:    NewConfig(big.NewInt(4), admins, nil, nil),
			Expected: false,
		},
		"different initial mint amounts": {
			Config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Other: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(2),
				}),
			Expected: false,
		},
		"different initial mint addresses": {
			Config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Other: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(1),
				}),
			Expected: false,
		},
		"same config": {
			Config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Other: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Expected: true,
		},
	}
	allowlist.EqualPrecompileWithAllowListTests(t, Module, tests)
}
