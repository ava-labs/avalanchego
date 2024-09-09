// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
)

func TestPrecompiledContractSpendsGas(t *testing.T) {
	unwrapped := &sha256hash{}

	input := []byte{'J', 'E', 'T', 'S'}
	requiredGas := unwrapped.RequiredGas(input)
	_, remainingGas, err := RunPrecompiledContract(unwrapped, input, requiredGas)
	if err != nil {
		t.Fatalf("Unexpectedly failed to run precompiled contract: %s", err)
	}

	if remainingGas != 0 {
		t.Fatalf("Found more remaining gas than expected: %d", remainingGas)
	}
}

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func CanTransfer(db StateDB, addr common.Address, amount *uint256.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

func CanTransferMC(db StateDB, addr common.Address, to common.Address, coinID common.Hash, amount *big.Int) bool {
	log.Info("CanTransferMC", "address", addr, "to", to, "coinID", coinID, "amount", amount)
	return db.GetBalanceMultiCoin(addr, coinID).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func Transfer(db StateDB, sender, recipient common.Address, amount *uint256.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func TransferMultiCoin(db StateDB, sender, recipient common.Address, coinID common.Hash, amount *big.Int) {
	db.SubBalanceMultiCoin(sender, coinID, amount)
	db.AddBalanceMultiCoin(recipient, coinID, amount)
}

func TestPackNativeAssetCallInput(t *testing.T) {
	addr := common.BytesToAddress([]byte("hello"))
	assetID := common.BytesToHash([]byte("ScoobyCoin"))
	assetAmount := big.NewInt(50)
	callData := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	input := PackNativeAssetCallInput(addr, assetID, assetAmount, callData)

	unpackedAddr, unpackedAssetID, unpackedAssetAmount, unpackedCallData, err := UnpackNativeAssetCallInput(input)
	assert.NoError(t, err)
	assert.Equal(t, addr, unpackedAddr, "address")
	assert.Equal(t, assetID, unpackedAssetID, "assetID")
	assert.Equal(t, assetAmount, unpackedAssetAmount, "assetAmount")
	assert.Equal(t, callData, unpackedCallData, "callData")
}

func TestStatefulPrecompile(t *testing.T) {
	vmCtx := BlockContext{
		BlockNumber:       big.NewInt(0),
		Time:              0,
		CanTransfer:       CanTransfer,
		CanTransferMC:     CanTransferMC,
		Transfer:          Transfer,
		TransferMultiCoin: TransferMultiCoin,
	}

	type statefulContractTest struct {
		setupStateDB         func() StateDB
		from                 common.Address
		precompileAddr       common.Address
		input                []byte
		value                *uint256.Int
		gasInput             uint64
		expectedGasRemaining uint64
		expectedErr          error
		expectedResult       []byte
		name                 string
		stateDBCheck         func(*testing.T, StateDB)
	}

	userAddr1 := common.BytesToAddress([]byte("user1"))
	userAddr2 := common.BytesToAddress([]byte("user2"))
	assetID := common.BytesToHash([]byte("ScoobyCoin"))
	zeroBytes := make([]byte, 32)
	big0.FillBytes(zeroBytes)
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
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
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
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				// Create account
				statedb.CreateAccount(userAddr1)
				// Set balance to pay for gas fee
				statedb.SetBalance(userAddr1, u256Hundred)
				// Initialize multicoin balance and set it back to 0
				statedb.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.SubBalanceMultiCoin(userAddr1, assetID, bigHundred)
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
			name:                 "native asset balance: initialized multicoin balance returns 0",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				// Create account
				statedb.CreateAccount(userAddr1)
				// Set balance to pay for gas fee
				statedb.SetBalance(userAddr1, u256Hundred)
				// Initialize multicoin balance to 100
				statedb.AddBalanceMultiCoin(userAddr1, assetID, bigHundred)
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
			expectedResult:       oneHundredBytes,
			name:                 "native asset balance: returns correct non-zero multicoin balance",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                nil,
			value:                big0,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: 0,
			expectedErr:          vmerrs.ErrExecutionReverted,
			expectedResult:       nil,
			name:                 "native asset balance: invalid input data reverts",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                big0,
			gasInput:             params.AssetBalanceApricot - 1,
			expectedGasRemaining: 0,
			expectedErr:          vmerrs.ErrOutOfGas,
			expectedResult:       nil,
			name:                 "native asset balance: insufficient gas errors",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetBalanceAddr,
			input:                PackNativeAssetBalanceInput(userAddr1, assetID),
			value:                u256Hundred,
			gasInput:             params.AssetBalanceApricot,
			expectedGasRemaining: params.AssetBalanceApricot,
			expectedErr:          vmerrs.ErrInsufficientBalance,
			expectedResult:       nil,
			name:                 "native asset balance: non-zero value with insufficient funds reverts before running pre-compile",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                big0,
			gasInput:             params.AssetCallApricot + params.CallNewAccountGas,
			expectedGasRemaining: 0,
			expectedErr:          nil,
			expectedResult:       nil,
			name:                 "native asset call: multicoin transfer",
			stateDBCheck: func(t *testing.T, stateDB StateDB) {
				user1Balance := stateDB.GetBalance(userAddr1)
				user2Balance := stateDB.GetBalance(userAddr2)
				user1AssetBalance := stateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := stateDB.GetBalanceMultiCoin(userAddr2, assetID)

				expectedBalance := big.NewInt(50)
				assert.Equal(t, u256Hundred, user1Balance, "user 1 balance")
				assert.Equal(t, big0, user2Balance, "user 2 balance")
				assert.Equal(t, expectedBalance, user1AssetBalance, "user 1 asset balance")
				assert.Equal(t, expectedBalance, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(49),
			gasInput:             params.AssetCallApricot + params.CallNewAccountGas,
			expectedGasRemaining: 0,
			expectedErr:          nil,
			expectedResult:       nil,
			name:                 "native asset call: multicoin transfer with non-zero value",
			stateDBCheck: func(t *testing.T, stateDB StateDB) {
				user1Balance := stateDB.GetBalance(userAddr1)
				user2Balance := stateDB.GetBalance(userAddr2)
				nativeAssetCallAddrBalance := stateDB.GetBalance(NativeAssetCallAddr)
				user1AssetBalance := stateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := stateDB.GetBalanceMultiCoin(userAddr2, assetID)
				expectedBalance := big.NewInt(50)

				assert.Equal(t, uint256.NewInt(51), user1Balance, "user 1 balance")
				assert.Equal(t, big0, user2Balance, "user 2 balance")
				assert.Equal(t, uint256.NewInt(49), nativeAssetCallAddrBalance, "native asset call addr balance")
				assert.Equal(t, expectedBalance, user1AssetBalance, "user 1 asset balance")
				assert.Equal(t, expectedBalance, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, big.NewInt(50))
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(51), nil),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot,
			expectedGasRemaining: 0,
			expectedErr:          vmerrs.ErrInsufficientBalance,
			expectedResult:       nil,
			name:                 "native asset call: insufficient multicoin funds",
			stateDBCheck: func(t *testing.T, stateDB StateDB) {
				user1Balance := stateDB.GetBalance(userAddr1)
				user2Balance := stateDB.GetBalance(userAddr2)
				user1AssetBalance := stateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := stateDB.GetBalanceMultiCoin(userAddr2, assetID)

				assert.Equal(t, bigHundred, user1Balance, "user 1 balance")
				assert.Equal(t, big0, user2Balance, "user 2 balance")
				assert.Equal(t, big.NewInt(51), user1AssetBalance, "user 1 asset balance")
				assert.Equal(t, big0, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, uint256.NewInt(50))
				statedb.SetBalanceMultiCoin(userAddr1, assetID, big.NewInt(50))
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(51),
			gasInput:             params.AssetCallApricot,
			expectedGasRemaining: params.AssetCallApricot,
			expectedErr:          vmerrs.ErrInsufficientBalance,
			expectedResult:       nil,
			name:                 "native asset call: insufficient funds",
			stateDBCheck: func(t *testing.T, stateDB StateDB) {
				user1Balance := stateDB.GetBalance(userAddr1)
				user2Balance := stateDB.GetBalance(userAddr2)
				user1AssetBalance := stateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := stateDB.GetBalanceMultiCoin(userAddr2, assetID)

				assert.Equal(t, big.NewInt(50), user1Balance, "user 1 balance")
				assert.Equal(t, big0, user2Balance, "user 2 balance")
				assert.Equal(t, big.NewInt(50), user1AssetBalance, "user 1 asset balance")
				assert.Equal(t, big0, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot - 1,
			expectedGasRemaining: 0,
			expectedErr:          vmerrs.ErrOutOfGas,
			expectedResult:       nil,
			name:                 "native asset call: insufficient gas for native asset call",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot + params.CallNewAccountGas - 1,
			expectedGasRemaining: 0,
			expectedErr:          vmerrs.ErrOutOfGas,
			expectedResult:       nil,
			name:                 "native asset call: insufficient gas to create new account",
			stateDBCheck: func(t *testing.T, stateDB StateDB) {
				user1Balance := stateDB.GetBalance(userAddr1)
				user2Balance := stateDB.GetBalance(userAddr2)
				user1AssetBalance := stateDB.GetBalanceMultiCoin(userAddr1, assetID)
				user2AssetBalance := stateDB.GetBalanceMultiCoin(userAddr2, assetID)

				assert.Equal(t, bigHundred, user1Balance, "user 1 balance")
				assert.Equal(t, big0, user2Balance, "user 2 balance")
				assert.Equal(t, bigHundred, user1AssetBalance, "user 1 asset balance")
				assert.Equal(t, big0, user2AssetBalance, "user 2 asset balance")
			},
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       NativeAssetCallAddr,
			input:                make([]byte, 24),
			value:                uint256.NewInt(50),
			gasInput:             params.AssetCallApricot + params.CallNewAccountGas,
			expectedGasRemaining: params.CallNewAccountGas,
			expectedErr:          vmerrs.ErrExecutionReverted,
			expectedResult:       nil,
			name:                 "native asset call: invalid input",
		},
		{
			setupStateDB: func() StateDB {
				statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
				if err != nil {
					t.Fatal(err)
				}
				statedb.SetBalance(userAddr1, u256Hundred)
				statedb.SetBalanceMultiCoin(userAddr1, assetID, bigHundred)
				statedb.Finalise(true)
				return statedb
			},
			from:                 userAddr1,
			precompileAddr:       genesisContractAddr,
			input:                PackNativeAssetCallInput(userAddr2, assetID, big.NewInt(50), nil),
			value:                big0,
			gasInput:             params.AssetCallApricot + params.CallNewAccountGas,
			expectedGasRemaining: params.AssetCallApricot + params.CallNewAccountGas,
			expectedErr:          vmerrs.ErrExecutionReverted,
			expectedResult:       nil,
			name:                 "deprecated contract",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDB := test.setupStateDB()
			// Create EVM with BlockNumber and Time initialized to 0 to enable Apricot Rules.
			evm := NewEVM(vmCtx, TxContext{}, stateDB, params.TestApricotPhase5Config, Config{}) // Use ApricotPhase5Config because these precompiles are deprecated in ApricotPhase6.
			ret, gasRemaining, err := evm.Call(AccountRef(test.from), test.precompileAddr, test.input, test.gasInput, test.value)
			// Place gas remaining check before error check, so that it is not skipped when there is an error
			assert.Equal(t, test.expectedGasRemaining, gasRemaining, "unexpected gas remaining")

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
