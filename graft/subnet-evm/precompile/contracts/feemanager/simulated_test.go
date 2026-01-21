// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/txpool"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
	feemanagerbindings "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager/feemanagertest/bindings"
)

var (
	adminKey, _        = crypto.GenerateKey()
	unprivilegedKey, _ = crypto.GenerateKey()

	adminAddress        = crypto.PubkeyToAddress(adminKey.PublicKey)
	unprivilegedAddress = crypto.PubkeyToAddress(unprivilegedKey.PublicKey)
)

var (
	genesisFeeConfig = commontype.FeeConfig{
		GasLimit:                 big.NewInt(20_000_000),
		TargetBlockRate:          2,
		MinBaseFee:               big.NewInt(1_000_000_000),
		TargetGas:                big.NewInt(100_000_000),
		BaseFeeChangeDenominator: big.NewInt(48),
		MinBlockGasCost:          big.NewInt(0),
		MaxBlockGasCost:          big.NewInt(10_000_000),
		BlockGasCostStep:         big.NewInt(500_000),
	}

	cchainFeeConfig = commontype.FeeConfig{
		GasLimit:                 big.NewInt(8_000_000),
		TargetBlockRate:          2,
		MinBaseFee:               big.NewInt(25_000_000_000),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(36),
		MinBlockGasCost:          big.NewInt(0),
		MaxBlockGasCost:          big.NewInt(1_000_000),
		BlockGasCostStep:         big.NewInt(100_000),
	}
)

func TestMain(m *testing.M) {
	// Ensure libevm extras are registered for tests.
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	m.Run()
}

func deployFeeManagerTest(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *feemanagerbindings.FeeManagerTest) {
	t.Helper()
	addr, tx, contract, err := feemanagerbindings.DeployFeeManagerTest(auth, b.Client(), feemanager.ContractAddress)
	require.NoError(t, err)
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

// feeConfigSetter is satisfied by both FeeManagerTest and IFeeManager bindings.
// This allows us to use the same helper for both.
type feeConfigSetter interface {
	SetFeeConfig(opts *bind.TransactOpts, gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error)
}

// trySetFeeConfig attempts to set fee config using a FeeConfig struct.
// Returns the transaction and any error from the call.
func trySetFeeConfig(contract feeConfigSetter, auth *bind.TransactOpts, config commontype.FeeConfig) (*types.Transaction, error) {
	return contract.SetFeeConfig(
		auth,
		config.GasLimit,
		new(big.Int).SetUint64(config.TargetBlockRate),
		config.MinBaseFee,
		config.TargetGas,
		config.BaseFeeChangeDenominator,
		config.MinBlockGasCost,
		config.MaxBlockGasCost,
		config.BlockGasCostStep,
	)
}

// setFeeConfig sets fee config using a FeeConfig struct instead of individual parameters.
// Returns the block number where the config was changed.
func setFeeConfig(t *testing.T, b *sim.Backend, contract feeConfigSetter, auth *bind.TransactOpts, config commontype.FeeConfig) uint64 {
	t.Helper()
	tx, err := trySetFeeConfig(contract, auth, config)
	require.NoError(t, err)
	receipt := utilstest.WaitReceiptSuccessful(t, b, tx)
	return receipt.BlockNumber.Uint64()
}

// toCommonFeeConfig converts a bindings FeeConfig to a commontype.FeeConfig
func toCommonFeeConfig(cfg feemanagerbindings.IFeeManagerFeeConfig) commontype.FeeConfig {
	return commontype.FeeConfig{
		GasLimit:                 cfg.GasLimit,
		TargetBlockRate:          cfg.TargetBlockRate.Uint64(),
		MinBaseFee:               cfg.MinBaseFee,
		TargetGas:                cfg.TargetGas,
		BaseFeeChangeDenominator: cfg.BaseFeeChangeDenominator,
		MinBlockGasCost:          cfg.MinBlockGasCost,
		MaxBlockGasCost:          cfg.MaxBlockGasCost,
		BlockGasCostStep:         cfg.BlockGasCostStep,
	}
}

// verifyFeeConfigsMatch verifies that two fee configs match
func verifyFeeConfigsMatch(t *testing.T, expected, actual commontype.FeeConfig) {
	t.Helper()
	require.Zero(t, expected.GasLimit.Cmp(actual.GasLimit), "gasLimit mismatch")
	require.Zero(t, new(big.Int).SetUint64(expected.TargetBlockRate).Cmp(new(big.Int).SetUint64(actual.TargetBlockRate)), "targetBlockRate mismatch")
	require.Zero(t, expected.MinBaseFee.Cmp(actual.MinBaseFee), "minBaseFee mismatch")
	require.Zero(t, expected.TargetGas.Cmp(actual.TargetGas), "targetGas mismatch")
	require.Zero(t, expected.BaseFeeChangeDenominator.Cmp(actual.BaseFeeChangeDenominator), "baseFeeChangeDenominator mismatch")
	require.Zero(t, expected.MinBlockGasCost.Cmp(actual.MinBlockGasCost), "minBlockGasCost mismatch")
	require.Zero(t, expected.MaxBlockGasCost.Cmp(actual.MaxBlockGasCost), "maxBlockGasCost mismatch")
	require.Zero(t, expected.BlockGasCostStep.Cmp(actual.BlockGasCostStep), "blockGasCostStep mismatch")
}

func TestFeeManager(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, feeManagerIntf *feemanagerbindings.IFeeManager)
	}

	testCases := []testCase{
		{
			name: "should verify admin has admin role",
			test: func(t *testing.T, _ *sim.Backend, feeManager *feemanagerbindings.IFeeManager) {
				allowlisttest.VerifyRole(t, feeManager, adminAddress, allowlist.AdminRole)
			},
		},
		{
			name: "should verify new contract has no role",
			test: func(t *testing.T, backend *sim.Backend, feeManager *feemanagerbindings.IFeeManager) {
				testContractAddr, _ := deployFeeManagerTest(t, backend, admin)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "contract should not be able to change fee without enabled",
			test: func(t *testing.T, backend *sim.Backend, feeManager *feemanagerbindings.IFeeManager) {
				testContractAddr, testContract := deployFeeManagerTest(t, backend, admin)

				allowlisttest.VerifyRole(t, feeManager, adminAddress, allowlist.AdminRole)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.NoRole)

				// Try to change fee config from contract without being enabled - should fail during gas estimation
				_, err := trySetFeeConfig(testContract, admin, cchainFeeConfig)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "contract should be added to manager list",
			test: func(t *testing.T, backend *sim.Backend, feeManager *feemanagerbindings.IFeeManager) {
				testContractAddr, _ := deployFeeManagerTest(t, backend, admin)
				allowlisttest.VerifyRole(t, feeManager, adminAddress, allowlist.AdminRole)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.NoRole)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "admin should be able to change fees",
			test: func(t *testing.T, backend *sim.Backend, feeManager *feemanagerbindings.IFeeManager) {
				testContractAddr, testContract := deployFeeManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

				got, err := testContract.GetFeeConfig(nil)
				require.NoError(t, err)
				verifyFeeConfigsMatch(t, genesisFeeConfig, toCommonFeeConfig(got))

				blockNum := setFeeConfig(t, backend, testContract, admin, cchainFeeConfig)

				got, err = testContract.GetFeeConfig(nil)
				require.NoError(t, err)
				verifyFeeConfigsMatch(t, cchainFeeConfig, toCommonFeeConfig(got))

				lastChangedAt, err := testContract.GetFeeConfigLastChangedAt(nil)
				require.NoError(t, err)
				require.Equal(t, blockNum, lastChangedAt.Uint64())
			},
		},
		{
			name: "should reject a transaction below the minimum fee",
			test: func(t *testing.T, backend *sim.Backend, feeManager *feemanagerbindings.IFeeManager) {
				testContractAddr, testContract := deployFeeManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

				// Get current min base fee
				config, err := testContract.GetFeeConfig(nil)
				require.NoError(t, err)
				originalMinBaseFee := config.MinBaseFee

				// Raise min fee by one
				raisedConfig := genesisFeeConfig
				raisedConfig.MinBaseFee = new(big.Int).Add(originalMinBaseFee, big.NewInt(1))
				_ = setFeeConfig(t, backend, testContract, admin, raisedConfig)

				// Try to send a transaction with the old (now too low) fee - should fail
				lowFeeAuth := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)
				lowFeeAuth.GasLimit = 100000
				lowFeeAuth.GasFeeCap = originalMinBaseFee
				lowFeeAuth.GasTipCap = big.NewInt(1)

				lowFeeConfig := genesisFeeConfig
				lowFeeConfig.MinBaseFee = originalMinBaseFee
				_, err = trySetFeeConfig(testContract, lowFeeAuth, lowFeeConfig)
				require.ErrorContains(t, err, txpool.ErrUnderpriced.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
	}

	precompileCfg := feemanager.NewConfig(utils.PointerTo[uint64](0), []common.Address{adminAddress}, nil, nil, &genesisFeeConfig)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, []common.Address{adminAddress, unprivilegedAddress})
			defer backend.Close()

			feeManager, err := feemanagerbindings.NewIFeeManager(feemanager.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, feeManager)
		})
	}
}

func TestIFeeManager_Events(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)

	precompileCfg := feemanager.NewConfig(utils.PointerTo[uint64](0), []common.Address{adminAddress}, nil, nil, &genesisFeeConfig)
	backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, []common.Address{adminAddress, unprivilegedAddress})
	defer backend.Close()

	feeManager, err := feemanagerbindings.NewIFeeManager(feemanager.ContractAddress, backend.Client())
	require.NoError(t, err)

	t.Run("should emit FeeConfigChanged event", func(t *testing.T) {
		require := require.New(t)

		// Change fee config from genesis to C-Chain
		_ = setFeeConfig(t, backend, feeManager, admin, cchainFeeConfig)

		// Filter for FeeConfigChanged events
		iter, err := feeManager.FilterFeeConfigChanged(nil, []common.Address{adminAddress})
		require.NoError(err)
		defer iter.Close()

		// Verify event fields match expected values
		require.True(iter.Next(), "expected to find FeeConfigChanged event")
		event := iter.Event
		require.Equal(adminAddress, event.Sender)

		verifyFeeConfigsMatch(t, genesisFeeConfig, toCommonFeeConfig(event.OldFeeConfig))
		verifyFeeConfigsMatch(t, cchainFeeConfig, toCommonFeeConfig(event.NewFeeConfig))

		require.False(iter.Next(), "expected no more FeeConfigChanged events")
		require.NoError(iter.Error())
	})
}
