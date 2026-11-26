// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

// TestDeployerAllowListPrecompileUpgrades asserts that a deployer allowlist
// upgrade MUST apply to deploys in the first block at or after its timestamp.
func TestDeployerAllowListPrecompileUpgrades(t *testing.T) {
	disableTime := testStartTime.Add(blockBuildAdvance)
	reenableTime := disableTime.Add(blockBuildAdvance)

	wallet := newWallet(t, 1)
	enabledConfig := allowlist.AllowListConfig{
		AdminAddresses: []common.Address{{'a', 'd', 'm', 'i', 'n'}},
	}
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withGenesisPrecompiles(extras.Precompiles{
			deployerallowlist.ConfigKey: &deployerallowlist.Config{
				AllowListConfig: enabledConfig,
				Upgrade: precompileconfig.Upgrade{
					BlockTimestamp: new(uint64(0)),
				},
			},
		}),
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{
				{
					Config: &deployerallowlist.Config{
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(disableTime.Unix())), //#nosec G115 -- Known non-negative
							Disable:        true,
						},
					},
				},
				{
					Config: &deployerallowlist.Config{
						AllowListConfig: enabledConfig,
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(reenableTime.Unix())), //#nosec G115 -- Known non-negative
						},
					},
				},
			},
		}),
	)

	steps := []struct {
		name       string
		time       time.Time
		wantStatus uint64
	}{
		{
			name:       "genesis",
			time:       testStartTime,
			wantStatus: types.ReceiptStatusFailed,
		},
		{
			name:       "disable",
			time:       disableTime,
			wantStatus: types.ReceiptStatusSuccessful,
		},
		{
			name:       "reenable",
			time:       reenableTime,
			wantStatus: types.ReceiptStatusFailed,
		},
	}
	for _, s := range steps {
		sut.clock.Set(s.time)
		tx := wallet.SetNonceAndSign(t, 0, deployTx())
		sut.send(ctx, t, tx)
		sut.RunConsensusLoop(ctx, t)

		receipt, err := sut.EthClient().TransactionReceipt(ctx, tx.Hash())
		require.NoErrorf(t, err, "%s: TransactionReceipt()", s.name)
		require.Equalf(t, s.wantStatus, receipt.Status, "%s: deploy status", s.name)
	}
}
