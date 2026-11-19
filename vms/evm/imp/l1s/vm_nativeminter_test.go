// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
)

// nativeMinterCallGas is the gas budget supplied to `mintNativeCoin` calls.
// 100k comfortably covers intrinsic gas (~21k) + MintGasCost (30k) +
// NativeCoinMintedEventGasCost (~2.4k) post-Durango.
const nativeMinterCallGas = 100_000

// sendMintTx packs a `mintNativeCoin(target, amount)` calldata for the
// nativeminter precompile and broadcasts it from account `from`.
func sendMintTx(ctx context.Context, t *testing.T, sut *SUT, wallet *saetest.Wallet, from int, target common.Address, amount *big.Int) *types.Transaction {
	t.Helper()

	data, err := nativeminter.PackMintNativeCoin(target, amount)
	require.NoError(t, err)
	return sut.send(ctx, t, wallet, from, callTx(nativeminter.ContractAddress, data, nativeMinterCallGas))
}

// TestNativeMinterPrecompileUpgradesSAE exercises mid-chain
// `PrecompileUpgrades` for the `nativeminter` precompile under SAE end-to-end
// through the same enable -> disable -> re-enable timeline used by the
// allowlist tests.
//
// Unlike txallowlist (admission-gated) and deployerallowlist (frame-local
// CanCreateContract), nativeminter is a **pure inline precompile**: it
// performs `stateDB.AddBalance(to, amount)` directly inside its `Run` method
// (see graft/subnet-evm/precompile/contracts/nativeminter/contract.go::mintNativeCoin)
// and the role check happens inside the same call. There is no end-of-block
// deferred operation and no SAE hook plumbing involved. Worst-case parity
// falls out for free because worst-case execution runs the same EVM code on
// the same StateDB.
//
// The test asserts:
//  1. The precompile is correctly enabled / disabled at the scheduled
//     timestamp (via `StartExecutingBlock` -> `core.ApplyUpgrades`).
//  2. Admin / manager roles are observable via the generic
//     [SUT.fetchAllowListRole] helper and follow the last-executed vs
//     last-settled lag exactly as for the allowlists.
//  3. Mint txs from non-allow-listed senders are admitted at RPC and
//     included in the block, but produce a `status=failed` receipt with no
//     balance change (the precompile returns ErrCannotMint). Once the
//     precompile is fully disabled, the address has no contract code and
//     calls succeed (status=1) but perform no balance change. Once
//     re-enabled, mints from a newly-promoted manager succeed.
func TestNativeMinterPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	now := testStartTime
	disableTime := now.Add(saeparams.Tau)
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	// `target` is intentionally NOT one of the funded keychain accounts so
	// that mint effects on its balance are unambiguous.
	target := common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
	mintAmount := big.NewInt(1_000_000_000)

	wallet := newWallet(t, 2)
	addresses := wallet.Addresses()
	admin := addresses[adminIdx]
	nonAdmin := addresses[nonAdminIdx]

	ctx, sut := newSUT(
		t,
		withMaxAllocFor(addresses...),
		withGenesisPrecompiles(extras.Precompiles{
			nativeminter.ConfigKey: nativeminter.NewConfig(
				new(uint64(0)),
				[]common.Address{admin},
				nil,
				nil,
				nil, // no initial mint
			),
		}),
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{
				{
					Config: nativeminter.NewDisableConfig(new(uint64(disableTime.Unix()))), // #nosec G115 -- known positive test timestamp
				},
				{
					// Re-enable promotes `nonAdminIdx` to manager so we
					// can observe the new config land in state.
					Config: nativeminter.NewConfig(
						new(uint64(reenableTime.Unix())), // #nosec G115 -- known positive test timestamp
						[]common.Address{admin},
						nil,
						[]common.Address{nonAdmin},
						nil,
					),
				},
			},
		}),
	)

	// Step 0: at genesis, nativeminter is enabled with `admin` as the only
	// allow-listed minter. Latest and finalized agree (no upgrades yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(nativeminter.ContractAddress),
		"nativeminter must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Both mint txs are admitted at RPC. The admin's call succeeds and
	// credits `target` by exactly `mintAmount`; the non-admin's call reverts
	// because the precompile returns ErrCannotMint, producing a status=failed
	// receipt with no balance change.
	preBalance, err := sut.EthClient().BalanceAt(ctx, target, nil)
	require.NoError(t, err)
	require.Zero(t, preBalance.Sign(), "target balance must be 0 before any mint")

	adminMint := sendMintTx(ctx, t, sut, wallet, adminIdx, target, mintAmount)
	block := sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, adminMint.Hash(), block.Transactions()[0].Hash())
	sut.requireReceiptStatus(ctx, t, adminMint, types.ReceiptStatusSuccessful)

	sut.clock.Advance(blockBuildAdvance)
	nonAdminMint := sendMintTx(ctx, t, sut, wallet, nonAdminIdx, target, mintAmount)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, nonAdminMint.Hash(), block.Transactions()[0].Hash())
	sut.requireReceiptStatus(ctx, t, nonAdminMint, types.ReceiptStatusFailed)

	postBalance, err := sut.EthClient().BalanceAt(ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postBalance.Cmp(mintAmount),
		"target balance must reflect exactly one successful mint (want=%s got=%s)", mintAmount, postBalance)

	// Step 1: advance past `disableTime` and produce the activation block.
	// `StartExecutingBlock` runs the disable upgrade BEFORE tx execution
	// in that block, so the admin's mint in the same block sees the
	// precompile already disabled.
	//
	// Note on EVM semantics post-disable: once the precompile is disabled,
	// libevm no longer overrides `nativeminter.ContractAddress`, so the
	// address has no contract code and the call is treated as a plain
	// value-transfer to an EOA. With value=0 and arbitrary calldata, this
	// succeeds with status=1 BUT performs no balance change on `target`
	// (the calldata is just ignored). We assert the latter — that the
	// post-disable mint had no effect on `target`'s balance — as the
	// observable proof the precompile is no longer active.
	sut.clock.Set(disableTime.Add(time.Second))
	postDisableMint := sendMintTx(ctx, t, sut, wallet, adminIdx, target, mintAmount)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, postDisableMint.Hash(), block.Transactions()[0].Hash())

	postDisableBalance, err := sut.EthClient().BalanceAt(ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postDisableBalance.Cmp(mintAmount),
		"target balance must be unchanged after the precompile was disabled (want=%s got=%s)", mintAmount, postDisableBalance)

	// (1) Disable upgrade fired inside `StartExecutingBlock`; precompile
	// is no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(nativeminter.ContractAddress),
		"nativeminter must be disabled after activation")
	// (2) Latest reflects the disable; finalized still observes the
	// pre-disable admin role (settlement lags by Tau).
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Allow the disable activation to settle so finalized catches up.
	sut.clock.Advance(saeparams.Tau + time.Second)
	settleTx := sut.send(ctx, t, wallet, adminIdx, transferTx(nonAdmin, common.Big1))
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable activation
	// block. The upgrade fires in `StartExecutingBlock`, so a mint from
	// the now-promoted manager (`nonAdmin`) in this block sees the
	// precompile already enabled with the new roles and succeeds.
	sut.clock.Set(reenableTime)
	managerMint := sendMintTx(ctx, t, sut, wallet, nonAdminIdx, target, mintAmount)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, managerMint.Hash(), block.Transactions()[0].Hash())
	sut.requireReceiptStatus(ctx, t, managerMint, types.ReceiptStatusSuccessful)

	// Manager mint credited `target` by another `mintAmount`.
	postReenableBalance, err := sut.EthClient().BalanceAt(ctx, target, nil)
	require.NoError(t, err)
	want := new(big.Int).Mul(mintAmount, big.NewInt(2))
	require.Zerof(t, postReenableBalance.Cmp(want),
		"target balance must reflect both successful mints (want=%s got=%s)", want, postReenableBalance)

	// (1) Re-enable upgrade fired; precompile is enabled per chain config
	// at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(nativeminter.ContractAddress),
		"nativeminter must be re-enabled after activation")
	// (2) Latest reflects the re-enable (admin + manager promotion);
	// finalized is still pre-re-enable (no roles).
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))
}
