// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contracts

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// TrieStressTestMetaData contains all meta data concerning the TrieStressTest contract.
var TrieStressTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"writeValues\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x6080604052348015600e575f5ffd5b506101328061001c5f395ff3fe6080604052348015600e575f5ffd5b50600436106026575f3560e01c8063514a19d614602a575b5f5ffd5b60406004803603810190603c919060d6565b6042565b005b5f60603373ffffffffffffffffffffffffffffffffffffffff16901b5f1b90505f5f90505b82811015609f575f82908060018154018082558091505060019003905f5260205f20015f909190919091505580806001019150506067565b505050565b5f5ffd5b5f819050919050565b60b88160a8565b811460c1575f5ffd5b50565b5f8135905060d08160b1565b92915050565b5f6020828403121560e85760e760a4565b5b5f60f38482850160c4565b9150509291505056fea2646970667358221220418357ca24c837df9263e230a8739659bc6fbf70cfd056989d639a46d8b18ef464736f6c634300081e0033",
}

// TrieStressTestABI is the input ABI used to generate the binding from.
// Deprecated: Use TrieStressTestMetaData.ABI instead.
var TrieStressTestABI = TrieStressTestMetaData.ABI

// TrieStressTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use TrieStressTestMetaData.Bin instead.
var TrieStressTestBin = TrieStressTestMetaData.Bin

// DeployTrieStressTest deploys a new Ethereum contract, binding an instance of TrieStressTest to it.
func DeployTrieStressTest(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *TrieStressTest, error) {
	parsed, err := TrieStressTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(TrieStressTestBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &TrieStressTest{TrieStressTestCaller: TrieStressTestCaller{contract: contract}, TrieStressTestTransactor: TrieStressTestTransactor{contract: contract}, TrieStressTestFilterer: TrieStressTestFilterer{contract: contract}}, nil
}

// TrieStressTest is an auto generated Go binding around an Ethereum contract.
type TrieStressTest struct {
	TrieStressTestCaller     // Read-only binding to the contract
	TrieStressTestTransactor // Write-only binding to the contract
	TrieStressTestFilterer   // Log filterer for contract events
}

// TrieStressTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type TrieStressTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TrieStressTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type TrieStressTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TrieStressTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type TrieStressTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TrieStressTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type TrieStressTestSession struct {
	Contract     *TrieStressTest   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// TrieStressTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type TrieStressTestCallerSession struct {
	Contract *TrieStressTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// TrieStressTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type TrieStressTestTransactorSession struct {
	Contract     *TrieStressTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// TrieStressTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type TrieStressTestRaw struct {
	Contract *TrieStressTest // Generic contract binding to access the raw methods on
}

// TrieStressTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type TrieStressTestCallerRaw struct {
	Contract *TrieStressTestCaller // Generic read-only contract binding to access the raw methods on
}

// TrieStressTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type TrieStressTestTransactorRaw struct {
	Contract *TrieStressTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewTrieStressTest creates a new instance of TrieStressTest, bound to a specific deployed contract.
func NewTrieStressTest(address common.Address, backend bind.ContractBackend) (*TrieStressTest, error) {
	contract, err := bindTrieStressTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &TrieStressTest{TrieStressTestCaller: TrieStressTestCaller{contract: contract}, TrieStressTestTransactor: TrieStressTestTransactor{contract: contract}, TrieStressTestFilterer: TrieStressTestFilterer{contract: contract}}, nil
}

// NewTrieStressTestCaller creates a new read-only instance of TrieStressTest, bound to a specific deployed contract.
func NewTrieStressTestCaller(address common.Address, caller bind.ContractCaller) (*TrieStressTestCaller, error) {
	contract, err := bindTrieStressTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &TrieStressTestCaller{contract: contract}, nil
}

// NewTrieStressTestTransactor creates a new write-only instance of TrieStressTest, bound to a specific deployed contract.
func NewTrieStressTestTransactor(address common.Address, transactor bind.ContractTransactor) (*TrieStressTestTransactor, error) {
	contract, err := bindTrieStressTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &TrieStressTestTransactor{contract: contract}, nil
}

// NewTrieStressTestFilterer creates a new log filterer instance of TrieStressTest, bound to a specific deployed contract.
func NewTrieStressTestFilterer(address common.Address, filterer bind.ContractFilterer) (*TrieStressTestFilterer, error) {
	contract, err := bindTrieStressTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &TrieStressTestFilterer{contract: contract}, nil
}

// bindTrieStressTest binds a generic wrapper to an already deployed contract.
func bindTrieStressTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := TrieStressTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_TrieStressTest *TrieStressTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _TrieStressTest.Contract.TrieStressTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_TrieStressTest *TrieStressTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _TrieStressTest.Contract.TrieStressTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_TrieStressTest *TrieStressTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _TrieStressTest.Contract.TrieStressTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_TrieStressTest *TrieStressTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _TrieStressTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_TrieStressTest *TrieStressTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _TrieStressTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_TrieStressTest *TrieStressTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _TrieStressTest.Contract.contract.Transact(opts, method, params...)
}

// WriteValues is a paid mutator transaction binding the contract method 0x514a19d6.
//
// Solidity: function writeValues(uint256 value) returns()
func (_TrieStressTest *TrieStressTestTransactor) WriteValues(opts *bind.TransactOpts, value *big.Int) (*types.Transaction, error) {
	return _TrieStressTest.contract.Transact(opts, "writeValues", value)
}

// WriteValues is a paid mutator transaction binding the contract method 0x514a19d6.
//
// Solidity: function writeValues(uint256 value) returns()
func (_TrieStressTest *TrieStressTestSession) WriteValues(value *big.Int) (*types.Transaction, error) {
	return _TrieStressTest.Contract.WriteValues(&_TrieStressTest.TransactOpts, value)
}

// WriteValues is a paid mutator transaction binding the contract method 0x514a19d6.
//
// Solidity: function writeValues(uint256 value) returns()
func (_TrieStressTest *TrieStressTestTransactorSession) WriteValues(value *big.Int) (*types.Transaction, error) {
	return _TrieStressTest.Contract.WriteValues(&_TrieStressTest.TransactOpts, value)
}
