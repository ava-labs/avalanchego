// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admission

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/l1s/libevm"
)

func TestMain(m *testing.M) {
	libevm.RegisterExtras()
	os.Exit(m.Run())
}

func TestTxAllowList(t *testing.T) {
	const upgradeTime = 10
	var (
		sender  = common.Address{1}
		genesis = utils.PointerTo[uint64](0)
		upgrade = utils.PointerTo[uint64](upgradeTime)
	)

	tests := []struct {
		name       string
		activateAt *uint64
		disableAt  *uint64
		time       uint64
		role       allowlist.Role
		wantErr    error
	}{
		{
			name: "inactive",
			role: allowlist.NoRole,
		},
		{
			name:       "enabled",
			activateAt: genesis,
			role:       allowlist.EnabledRole,
		},
		{
			name:       "admin",
			activateAt: genesis,
			role:       allowlist.AdminRole,
		},
		{
			name:       "manager",
			activateAt: genesis,
			role:       allowlist.ManagerRole,
		},
		{
			name:       "no_role",
			activateAt: genesis,
			role:       allowlist.NoRole,
			wantErr:    vmerrors.ErrSenderAddressNotAllowListed,
		},
		{
			name:       "disabled",
			activateAt: genesis,
			disableAt:  upgrade,
			time:       upgradeTime,
			role:       allowlist.NoRole,
		},
		{
			name:       "before_activation",
			activateAt: upgrade,
			time:       upgradeTime - 1,
			role:       allowlist.NoRole,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := params.Copy(params.TestChainConfig)
			extra := params.GetExtra(&cfg)
			if tt.activateAt != nil {
				extra.GenesisPrecompiles = extras.Precompiles{
					txallowlist.ConfigKey: txallowlist.NewConfig(
						tt.activateAt,
						nil, // admins
						nil, // enableds
						nil, // managers
					),
				}
			}
			if tt.disableAt != nil {
				extra.PrecompileUpgrades = []extras.PrecompileUpgrade{
					{Config: txallowlist.NewDisableConfig(tt.disableAt)},
				}
			}

			sdb, err := state.New(
				types.EmptyRootHash,
				state.NewDatabase(rawdb.NewMemoryDatabase()),
				nil,
			)
			require.NoError(t, err, "state.New()")
			txallowlist.SetTxAllowListStatus(
				extstate.New(sdb),
				sender,
				tt.role,
			)

			hdr := &types.Header{
				Number: big.NewInt(0),
				Time:   tt.time,
			}
			settled := hook.SettledView{
				Header: hdr,
				Rules:  cfg.Rules(hdr.Number, params.IsMergeTODO, hdr.Time),
				State:  sdb,
			}
			require.ErrorIs(t, TxAllowList(sender, settled), tt.wantErr)
		})
	}
}
