// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlisttest

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
	allowlistbindings "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest/bindings"
)

// RunAllowListEventTests runs the standard AllowList event emission tests.
// This can be used by any precompile that uses the AllowList pattern.
func RunAllowListEventTests(
	t *testing.T,
	precompileCfg precompileconfig.Config,
	contractAddress common.Address,
	adminAuth *bind.TransactOpts,
	fundedAddrs ...common.Address,
) {
	t.Helper()

	testKey, _ := crypto.GenerateKey()
	testAddress := crypto.PubkeyToAddress(testKey.PublicKey)

	type testCase struct {
		name           string
		testRun        func(*allowlistbindings.IAllowList, *bind.TransactOpts, *sim.Backend, *testing.T, common.Address)
		expectedEvents []allowlistbindings.IAllowListRoleSet
	}

	testCases := []testCase{
		{
			name: "should emit event after set admin",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetAdmin(auth, addr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.AdminRole.Big(),
					Account: testAddress,
					Sender:  adminAuth.From,
					OldRole: allowlist.NoRole.Big(),
				},
			},
		},
		{
			name: "should emit event after set manager",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetManager(auth, addr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.ManagerRole.Big(),
					Account: testAddress,
					Sender:  adminAuth.From,
					OldRole: allowlist.NoRole.Big(),
				},
			},
		},
		{
			name: "should emit event after set enabled",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetEnabled(auth, addr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.EnabledRole.Big(),
					Account: testAddress,
					Sender:  adminAuth.From,
					OldRole: allowlist.NoRole.Big(),
				},
			},
		},
		{
			name: "should emit event after set none",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetEnabled(auth, addr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				tx, err = allowList.SetNone(auth, addr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.EnabledRole.Big(),
					Account: testAddress,
					Sender:  adminAuth.From,
					OldRole: allowlist.NoRole.Big(),
				},
				{
					Role:    allowlist.NoRole.Big(),
					Account: testAddress,
					Sender:  adminAuth.From,
					OldRole: allowlist.EnabledRole.Big(),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)

			backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, fundedAddrs)
			defer backend.Close()

			allowList, err := allowlistbindings.NewIAllowList(contractAddress, backend.Client())
			require.NoError(err)

			tc.testRun(allowList, adminAuth, backend, t, testAddress)

			iter, err := allowList.FilterRoleSet(nil, nil, nil, nil)
			require.NoError(err)
			defer iter.Close()

			for _, expectedEvent := range tc.expectedEvents {
				require.True(iter.Next(), "expected to find RoleSet event")
				event := iter.Event
				require.Zero(expectedEvent.Role.Cmp(event.Role), "role mismatch")
				require.Equal(expectedEvent.Account, event.Account, "account mismatch")
				require.Equal(expectedEvent.Sender, event.Sender, "sender mismatch")
				require.Zero(expectedEvent.OldRole.Cmp(event.OldRole), "oldRole mismatch")
			}

			require.False(iter.Next(), "expected no more RoleSet events")
			require.NoError(iter.Error())
		})
	}
}
