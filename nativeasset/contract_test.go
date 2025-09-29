// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeasset_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Force import core to register the VM hooks.
	// This allows testing the precompiles by exercising the EVM.
	_ "github.com/ava-labs/coreth/core"

	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params"

	ethtypes "github.com/ava-labs/libevm/core/types"
	ethparams "github.com/ava-labs/libevm/params"

	. "github.com/ava-labs/coreth/nativeasset"
)

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func CanTransfer(db vm.StateDB, addr common.Address, amount *uint256.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}

func TestPackNativeAssetCallInput(t *testing.T) {
	addr := common.BytesToAddress([]byte("hello"))
	assetID := common.BytesToHash([]byte("ScoobyCoin"))
	assetAmount := big.NewInt(50)
	callData := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	input := PackNativeAssetCallInput(addr, assetID, assetAmount, callData)

	unpackedAddr, unpackedAssetID, unpackedAssetAmount, unpackedCallData, err := UnpackNativeAssetCallInput(input)
	require.NoError(t, err)
	require.Equal(t, addr, unpackedAddr, "address")
	require.Equal(t, assetID, unpackedAssetID, "assetID")
	require.Equal(t, assetAmount, unpackedAssetAmount, "assetAmount")
	require.Equal(t, callData, unpackedCallData, "callData")
}

func TestStatefulPrecompile(t *testing.T) {
	vmCtx := vm.BlockContext{
		BlockNumber: big.NewInt(0),
		Time:        0,
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		Header: &ethtypes.Header{
			Number: big.NewInt(0),
		},
	}

	type statefulContractTest struct {
		setupStateDB         func() *state.StateDB
		from                 common.Address
		precompileAddr       common.Address
		input                []byte
		value                *uint256.Int
		gasInput             uint64
		expectedGasRemaining uint64
		expectedErr          error
		expectedResult       []byte
		name                 string
		stateDBCheck         func(*testing.T, *state.StateDB)
	}

	userAddr1 := common.BytesToAddress([]byte("user1"))
	userAddr2 := common.BytesToAddress([]byte("user2"))
	assetID := common.BytesToHash([]byte("ScoobyCoin"))
	zeroBytes := make([]byte, 32)
	big0 := uint256.NewInt(0)
	bigHundred := big.NewInt(100)
	u256Hundred := uint256.NewInt(100)
	oneHundredBytes := make([]byte, 32)
	bigFifty := big.NewInt(50)
	fiftyBytes := make([]byte, 32)
	bigFifty.FillBytes(fiftyBytes)
	bigHundred.FillBytes(oneHundredBytes)

	tests := []statefulContractTest{
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				// Create account
				statedb.CreateAccount(userAddr1)
				// Set balance to pay for gas fee
				statedb.SetBalance(userAddr1, u256Hundred)
				// Set MultiCoin balance
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                big0,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: 0,
			expectedErr:          nil,
			expectedResult:       zeroBytes,
			name:                 "native asset balance: uninitialized multicoin balance returns 0",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				// Create account
				statedb.CreateAccount(userAddr1)
				// Set balance to pay for gas fee
				statedb.SetBalance(userAddr1, u256Hundred)
				// Initialize multicoin balance and set it back to 0
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.SubBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                big0,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: 0,
			expectedErr:          nil,
			expectedResult:       zeroBytes,
			name:                 "native asset balance: initialized multicoin balance returns 0",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				// Create account
				statedb.CreateAccount(userAddr1)
				// Set balance to pay for gas fee
				statedb.SetBalance(userAddr1, u256Hundred)
				// Initialize multicoin balance to 100
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                big0,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: 0,
			expectedErr:          nil,
			expectedResult:       oneHundredBytes,
			name:                 "native asset balance: returns correct non-zero multicoin balance",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                nil,
			value:                big0,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: 0,
			expectedErr:          vm.ErrExecutionReverted,
			expectedResult:       nil,
			name:                 "native asset balance: invalid input data reverts",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                big0,
			gasInput:             params.AssetBalanceApricot - 1,
			expectedGasRemaining: 0,
			expectedErr:          vm.ErrOutOfGas,
			expectedResult:       nil,
			name:                 "native asset balance: insufficient gas errors",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                u256Hundred,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: params.AssetBalanceApricot,
			expectedErr:          vm.ErrInsufficientBalance,
			expectedResult:       nil,
			name:                 "native asset balance: non-zero value with insufficient funds reverts before running pre-compile",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                big0,
			gasInput:             params.AssetCallApricot + ethparams.CallNewAccountGas + 123,
			expectedGasRemaining: 123,
			expectedErr:          nil,
			expectedResult:       nil,
			name:                 "native asset call: multicoin transfer",
			stateDBCheck: func(t *testing.T, statedb *state.StateDB) {
				user1Balance := statedb.GetBalance(userAddr1)
				user2Balance := statedb.GetBalance(userAddr2)
				wrappedStateDB := extstate.New(statedb)
				user1AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr2, assetID)

				expectedBalance := big.NewInt(50)
				require.Equal(t, u256Hundred, user1Balance, "user 1 balance")
				require.Equal(t, big0, user2Balance, "user 2 balance")
				require.Equal(t, expectedBalance, user1AssetBalance, "user 1 asset balance")
				require.Equal(t, expectedBalance, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(49),
			gasInput:             params.AssetCallApricot + ethparams.CallNewAccountGas,
			expectedGasRemaining: 0,
			expectedErr:          nil,
			expectedResult:       nil,
			name:                 "native asset call: multicoin transfer with non-zero value",
			stateDBCheck: func(t *testing.T, statedb *state.StateDB) {
				user1Balance := statedb.GetBalance(userAddr1)
				user2Balance := statedb.GetBalance(userAddr2)
				nativeAssetCallAddrBalance := statedb.GetBalance(NativeAssetCallAddr)
				wrappedStateDB := extstate.New(statedb)
				user1AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr2, assetID)
				expectedBalance := big.NewInt(50)

				require.Equal(t, uint256.NewInt(51), user1Balance, "user 1 balance")
				require.Equal(t, big0, user2Balance, "user 2 balance")
				require.Equal(t, uint256.NewInt(49), nativeAssetCallAddrBalance, "native asset call addr balance")
				require.Equal(t, expectedBalance, user1AssetBalance, "user 1 asset balance")
				require.Equal(t, expectedBalance, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, big.NewInt(50))
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(51), nil),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot,
			expectedGasRemaining: 0,
			expectedErr:          vm.ErrInsufficientBalance,
			expectedResult:       nil,
			name:                 "native asset call: insufficient multicoin funds",
			stateDBCheck: func(t *testing.T, statedb *state.StateDB) {
				user1Balance := statedb.GetBalance(userAddr1)
				user2Balance := statedb.GetBalance(userAddr2)
				wrappedStateDB := extstate.New(statedb)
				user1AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr2, assetID)

				require.Equal(t, bigHundred, user1Balance, "user 1 balance")
				require.Equal(t, big0, user2Balance, "user 2 balance")
				require.Equal(t, big.NewInt(51), user1AssetBalance, "user 1 asset balance")
				require.Equal(t, big0, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, uint256.NewInt(50))
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, big.NewInt(50))
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(51),
			gasInput:             params.AssetCallApricot,
			expectedGasRemaining: params.AssetCallApricot,
			expectedErr:          vm.ErrInsufficientBalance,
			expectedResult:       nil,
			name:                 "native asset call: insufficient funds",
			stateDBCheck: func(t *testing.T, statedb *state.StateDB) {
				user1Balance := statedb.GetBalance(userAddr1)
				user2Balance := statedb.GetBalance(userAddr2)
				wrappedStateDB := extstate.New(statedb)
				user1AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr2, assetID)

				require.Equal(t, big.NewInt(50), user1Balance, "user 1 balance")
				require.Equal(t, big0, user2Balance, "user 2 balance")
				require.Equal(t, big.NewInt(50), user1AssetBalance, "user 1 asset balance")
				require.Equal(t, big0, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot - 1,
			expectedGasRemaining: 0,
			expectedErr:          vm.ErrOutOfGas,
			expectedResult:       nil,
			name:                 "native asset call: insufficient gas for native asset call",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot + ethparams.CallNewAccountGas - 1,
			expectedGasRemaining: 0,
			expectedErr:          vm.ErrOutOfGas,
			expectedResult:       nil,
			name:                 "native asset call: insufficient gas to create new account",
			stateDBCheck: func(t *testing.T, statedb *state.StateDB) {
				user1Balance := statedb.GetBalance(userAddr1)
				user2Balance := statedb.GetBalance(userAddr2)
				wrappedStateDB := extstate.New(statedb)
				user1AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := wrappedStateDB.GetBalanceMultiCoin(userAddr2, assetID)

				require.Equal(t, bigHundred, user1Balance, "user 1 balance")
				require.Equal(t, big0, user2Balance, "user 2 balance")
				require.Equal(t, bigHundred, user1AssetBalance, "user 1 asset balance")
				require.Equal(t, big0, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                make([]byte, 24),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot + ethparams.CallNewAccountGas,
			expectedGasRemaining: ethparams.CallNewAccountGas,
			expectedErr:          vm.ErrExecutionReverted,
			expectedResult:       nil,
			name:                 "native asset call: invalid input",
		},
		{
			setupStateDB: func() *state.StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				require.NoError(t, err)
				statedb.SetBalance(userAddr1, u256Hundred)
				wrappedStateDB := extstate.New(statedb)
				wrappedStateDB.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				wrappedStateDB.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       GenesisContractAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                big0,
			gasInput:             params.AssetCallApricot + ethparams.CallNewAccountGas,
			expectedGasRemaining: params.AssetCallApricot + ethparams.CallNewAccountGas,
			expectedErr:          vm.ErrExecutionReverted,
			expectedResult:       nil,
			name:                 "deprecated contract",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDB := test.setupStateDB()
			// Create EVM with BlockNumber and Time initialized to 0 to enable Apricot Rules.
			evm := vm.NewEVM(vmCtx, vm.TxContext{}, stateDB, params.TestApricotPhase5Config, vm.Config{}) // Use ApricotPhase5Config because these precompiles are deprecated in ApricotPhase6.
			ret, gasRemaining, err := evm.Call(vm.AccountRef(test.from), test.precompileAddr, test.input, test.gasInput, test.value)
			// Place gas remaining check before error check, so that it is not skipped when there is an error
			assert.Equalf(t, test.expectedGasRemaining, gasRemaining, "unexpected gas remaining (%d of %d)", gasRemaining, test.gasInput)

			if test.expectedErr != nil {
				assert.Equal(t, test.expectedErr, err, "expected error to match")
				return
			}
			if assert.NoError(t, err, "EVM Call produced unexpected error") {
				assert.Equal(t, test.expectedResult, ret, "unexpected return value")
				if test.stateDBCheck != nil {
					test.stateDBCheck(t, stateDB)
				}
			}
		})
	}
}
