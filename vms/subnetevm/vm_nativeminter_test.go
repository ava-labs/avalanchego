// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// nativeMinterCallGas is the gas budget supplied to `mintNativeCoin` calls.
// 100k comfortably covers intrinsic gas (~21k) + MintGasCost (30k) +
// NativeCoinMintedEventGasCost (~2.4k) post-Durango.
const nativeMinterCallGas = 100_000

// sendMintTx packs a `mintNativeCoin(target, amount)` calldata for the
// nativeminter precompile and broadcasts it from account `from`.
func sendMintTx(t *testing.T, sut *SUT, from int, target common.Address, amount *big.Int) *types.Transaction {
	t.Helper()

	data, err := nativeminter.PackMintNativeCoin(target, amount)
	require.NoError(t, err)
	return sut.sendCallTx(t, from, nativeminter.ContractAddress, data, nativeMinterCallGas)
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
//     timestamp (via `BeforeExecutingBlock` -> `core.ApplyUpgrades`).
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

	now := postHeliconStartTime(t)
	disableTime := now.Add(saeparams.Tau)
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	// `target` is intentionally NOT one of the funded keychain accounts so
	// that mint effects on its balance are unambiguous.
	target := common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
	mintAmount := big.NewInt(1_000_000_000)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				nativeminter.ConfigKey: nativeminter.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					nil, // no initial mint
				),
			}
		}),
		withUpgradeConfig(func(addresses []common.Address) []byte {
			return mustMarshalJSON(t, &extras.UpgradeConfig{
				PrecompileUpgrades: []extras.PrecompileUpgrade{
					{
						Config: nativeminter.NewDisableConfig(utils.PointerTo(uint64(disableTime.Unix()))),
					},
					{
						// Re-enable promotes `nonAdminIdx` to manager so we
						// can observe the new config land in state.
						Config: nativeminter.NewConfig(
							utils.PointerTo(uint64(reenableTime.Unix())),
							[]common.Address{addresses[adminIdx]},
							nil,
							[]common.Address{addresses[nonAdminIdx]},
							nil,
						),
					},
				},
			})
		}),
	)

	addresses := sut.ethWallet.Addresses()
	admin := addresses[adminIdx]
	nonAdmin := addresses[nonAdminIdx]

	// Step 0: at genesis, nativeminter is enabled with `admin` as the only
	// allow-listed minter. Latest and finalized agree (no upgrades yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(nativeminter.ContractAddress),
		"nativeminter must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Both mint txs are admitted at RPC. They are both included in the
	// next block, but the non-admin's call reverts because the precompile
	// returns ErrCannotMint, producing a status=failed receipt with no
	// balance change. The admin's call succeeds and credits `target` by
	// exactly `mintAmount`.
	preBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zero(t, preBalance.Sign(), "target balance must be 0 before any mint")

	adminMint := sendMintTx(t, sut, adminIdx, target, mintAmount)
	nonAdminMint := sendMintTx(t, sut, nonAdminIdx, target, mintAmount)
	block := sut.buildAndAcceptBlock(t)
	requireBlockContainsTxs(t, block, adminMint.Hash(), nonAdminMint.Hash())
	sut.requireTxSucceeded(t, adminMint)
	sut.requireTxFailed(t, nonAdminMint)

	postBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postBalance.Cmp(mintAmount),
		"target balance must reflect exactly one successful mint (want=%s got=%s)", mintAmount, postBalance)

	// Step 1: advance to `disableTime` and produce the activation block.
	// `BeforeExecutingBlock` runs the disable upgrade BEFORE tx execution
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
	sut.setTime(t, disableTime)
	postDisableMint := sendMintTx(t, sut, adminIdx, target, mintAmount)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, postDisableMint.Hash(), block.Transactions()[0].Hash())

	postDisableBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postDisableBalance.Cmp(mintAmount),
		"target balance must be unchanged after the precompile was disabled (want=%s got=%s)", mintAmount, postDisableBalance)

	// (1) Disable upgrade fired inside `BeforeExecutingBlock`; precompile
	// is no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(nativeminter.ContractAddress),
		"nativeminter must be disabled after activation")
	// (2) Latest reflects the disable; finalized still observes the
	// pre-disable admin role (settlement lags by Tau).
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Allow the disable activation to settle so finalized catches up.
	sut.advanceTime(t, saeparams.Tau+time.Second)
	settleTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable activation
	// block. The upgrade fires in `BeforeExecutingBlock`, so a mint from
	// the now-promoted manager (`nonAdmin`) in this block sees the
	// precompile already enabled with the new roles and succeeds.
	sut.setTime(t, reenableTime)
	managerMint := sendMintTx(t, sut, nonAdminIdx, target, mintAmount)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, managerMint.Hash(), block.Transactions()[0].Hash())
	sut.requireTxSucceeded(t, managerMint)

	// Manager mint credited `target` by another `mintAmount`.
	postReenableBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
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
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, nativeminter.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))
}
