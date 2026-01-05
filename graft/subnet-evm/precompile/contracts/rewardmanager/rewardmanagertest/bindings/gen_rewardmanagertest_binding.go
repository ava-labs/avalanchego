// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
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

// RewardManagerTestMetaData contains all meta data concerning the RewardManagerTest contract.
var RewardManagerTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"rewardManagerPrecompile\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"allowFeeRecipients\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"areFeeRecipientsAllowed\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"currentRewardAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"disableRewards\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setRewardAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b5060405161063a38038061063a833981810160405281019061003191906100d4565b805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506100ff565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a38261007a565b9050919050565b6100b381610099565b81146100bd575f5ffd5b50565b5f815190506100ce816100aa565b92915050565b5f602082840312156100e9576100e8610076565b5b5f6100f6848285016100c0565b91505092915050565b61052e8061010c5f395ff3fe60806040526004361061004d575f3560e01c80630329099f146100585780635e00e6791461006e578063bc17862814610096578063e915608b146100ac578063f6542b2e146100d657610054565b3661005457005b5f5ffd5b348015610063575f5ffd5b5061006c610100565b005b348015610079575f5ffd5b50610094600480360381019061008f9190610407565b61017d565b005b3480156100a1575f5ffd5b506100aa610206565b005b3480156100b7575f5ffd5b506100c0610283565b6040516100cd9190610441565b60405180910390f35b3480156100e1575f5ffd5b506100ea610316565b6040516100f79190610474565b60405180910390f35b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630329099f6040518163ffffffff1660e01b81526004015f604051808303815f87803b158015610165575f5ffd5b505af1158015610177573d5f5f3e3d5ffd5b50505050565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16635e00e679826040518263ffffffff1660e01b81526004016101d69190610441565b5f604051808303815f87803b1580156101ed575f5ffd5b505af11580156101ff573d5f5f3e3d5ffd5b5050505050565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663bc1786286040518163ffffffff1660e01b81526004015f604051808303815f87803b15801561026b575f5ffd5b505af115801561027d573d5f5f3e3d5ffd5b50505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663e915608b6040518163ffffffff1660e01b8152600401602060405180830381865afa1580156102ed573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061031191906104a1565b905090565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663f6542b2e6040518163ffffffff1660e01b8152600401602060405180830381865afa158015610380573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103a491906104f6565b905090565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6103d6826103ad565b9050919050565b6103e6816103cc565b81146103f0575f5ffd5b50565b5f81359050610401816103dd565b92915050565b5f6020828403121561041c5761041b6103a9565b5b5f610429848285016103f3565b91505092915050565b61043b816103cc565b82525050565b5f6020820190506104545f830184610432565b92915050565b5f8115159050919050565b61046e8161045a565b82525050565b5f6020820190506104875f830184610465565b92915050565b5f8151905061049b816103dd565b92915050565b5f602082840312156104b6576104b56103a9565b5b5f6104c38482850161048d565b91505092915050565b6104d58161045a565b81146104df575f5ffd5b50565b5f815190506104f0816104cc565b92915050565b5f6020828403121561050b5761050a6103a9565b5b5f610518848285016104e2565b9150509291505056fea164736f6c634300081c000a",
}

// RewardManagerTestABI is the input ABI used to generate the binding from.
// Deprecated: Use RewardManagerTestMetaData.ABI instead.
var RewardManagerTestABI = RewardManagerTestMetaData.ABI

// RewardManagerTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use RewardManagerTestMetaData.Bin instead.
var RewardManagerTestBin = RewardManagerTestMetaData.Bin

// DeployRewardManagerTest deploys a new Ethereum contract, binding an instance of RewardManagerTest to it.
func DeployRewardManagerTest(auth *bind.TransactOpts, backend bind.ContractBackend, rewardManagerPrecompile common.Address) (common.Address, *types.Transaction, *RewardManagerTest, error) {
	parsed, err := RewardManagerTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(RewardManagerTestBin), backend, rewardManagerPrecompile)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &RewardManagerTest{RewardManagerTestCaller: RewardManagerTestCaller{contract: contract}, RewardManagerTestTransactor: RewardManagerTestTransactor{contract: contract}, RewardManagerTestFilterer: RewardManagerTestFilterer{contract: contract}}, nil
}

// RewardManagerTest is an auto generated Go binding around an Ethereum contract.
type RewardManagerTest struct {
	RewardManagerTestCaller     // Read-only binding to the contract
	RewardManagerTestTransactor // Write-only binding to the contract
	RewardManagerTestFilterer   // Log filterer for contract events
}

// RewardManagerTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type RewardManagerTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// RewardManagerTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type RewardManagerTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// RewardManagerTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type RewardManagerTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// RewardManagerTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type RewardManagerTestSession struct {
	Contract     *RewardManagerTest // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// RewardManagerTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type RewardManagerTestCallerSession struct {
	Contract *RewardManagerTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// RewardManagerTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type RewardManagerTestTransactorSession struct {
	Contract     *RewardManagerTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// RewardManagerTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type RewardManagerTestRaw struct {
	Contract *RewardManagerTest // Generic contract binding to access the raw methods on
}

// RewardManagerTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type RewardManagerTestCallerRaw struct {
	Contract *RewardManagerTestCaller // Generic read-only contract binding to access the raw methods on
}

// RewardManagerTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type RewardManagerTestTransactorRaw struct {
	Contract *RewardManagerTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewRewardManagerTest creates a new instance of RewardManagerTest, bound to a specific deployed contract.
func NewRewardManagerTest(address common.Address, backend bind.ContractBackend) (*RewardManagerTest, error) {
	contract, err := bindRewardManagerTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &RewardManagerTest{RewardManagerTestCaller: RewardManagerTestCaller{contract: contract}, RewardManagerTestTransactor: RewardManagerTestTransactor{contract: contract}, RewardManagerTestFilterer: RewardManagerTestFilterer{contract: contract}}, nil
}

// NewRewardManagerTestCaller creates a new read-only instance of RewardManagerTest, bound to a specific deployed contract.
func NewRewardManagerTestCaller(address common.Address, caller bind.ContractCaller) (*RewardManagerTestCaller, error) {
	contract, err := bindRewardManagerTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &RewardManagerTestCaller{contract: contract}, nil
}

// NewRewardManagerTestTransactor creates a new write-only instance of RewardManagerTest, bound to a specific deployed contract.
func NewRewardManagerTestTransactor(address common.Address, transactor bind.ContractTransactor) (*RewardManagerTestTransactor, error) {
	contract, err := bindRewardManagerTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &RewardManagerTestTransactor{contract: contract}, nil
}

// NewRewardManagerTestFilterer creates a new log filterer instance of RewardManagerTest, bound to a specific deployed contract.
func NewRewardManagerTestFilterer(address common.Address, filterer bind.ContractFilterer) (*RewardManagerTestFilterer, error) {
	contract, err := bindRewardManagerTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &RewardManagerTestFilterer{contract: contract}, nil
}

// bindRewardManagerTest binds a generic wrapper to an already deployed contract.
func bindRewardManagerTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := RewardManagerTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_RewardManagerTest *RewardManagerTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _RewardManagerTest.Contract.RewardManagerTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_RewardManagerTest *RewardManagerTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RewardManagerTest.Contract.RewardManagerTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_RewardManagerTest *RewardManagerTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _RewardManagerTest.Contract.RewardManagerTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_RewardManagerTest *RewardManagerTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _RewardManagerTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_RewardManagerTest *RewardManagerTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RewardManagerTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_RewardManagerTest *RewardManagerTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _RewardManagerTest.Contract.contract.Transact(opts, method, params...)
}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool)
func (_RewardManagerTest *RewardManagerTestCaller) AreFeeRecipientsAllowed(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _RewardManagerTest.contract.Call(opts, &out, "areFeeRecipientsAllowed")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool)
func (_RewardManagerTest *RewardManagerTestSession) AreFeeRecipientsAllowed() (bool, error) {
	return _RewardManagerTest.Contract.AreFeeRecipientsAllowed(&_RewardManagerTest.CallOpts)
}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool)
func (_RewardManagerTest *RewardManagerTestCallerSession) AreFeeRecipientsAllowed() (bool, error) {
	return _RewardManagerTest.Contract.AreFeeRecipientsAllowed(&_RewardManagerTest.CallOpts)
}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address)
func (_RewardManagerTest *RewardManagerTestCaller) CurrentRewardAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _RewardManagerTest.contract.Call(opts, &out, "currentRewardAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address)
func (_RewardManagerTest *RewardManagerTestSession) CurrentRewardAddress() (common.Address, error) {
	return _RewardManagerTest.Contract.CurrentRewardAddress(&_RewardManagerTest.CallOpts)
}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address)
func (_RewardManagerTest *RewardManagerTestCallerSession) CurrentRewardAddress() (common.Address, error) {
	return _RewardManagerTest.Contract.CurrentRewardAddress(&_RewardManagerTest.CallOpts)
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_RewardManagerTest *RewardManagerTestTransactor) AllowFeeRecipients(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RewardManagerTest.contract.Transact(opts, "allowFeeRecipients")
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_RewardManagerTest *RewardManagerTestSession) AllowFeeRecipients() (*types.Transaction, error) {
	return _RewardManagerTest.Contract.AllowFeeRecipients(&_RewardManagerTest.TransactOpts)
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_RewardManagerTest *RewardManagerTestTransactorSession) AllowFeeRecipients() (*types.Transaction, error) {
	return _RewardManagerTest.Contract.AllowFeeRecipients(&_RewardManagerTest.TransactOpts)
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_RewardManagerTest *RewardManagerTestTransactor) DisableRewards(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RewardManagerTest.contract.Transact(opts, "disableRewards")
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_RewardManagerTest *RewardManagerTestSession) DisableRewards() (*types.Transaction, error) {
	return _RewardManagerTest.Contract.DisableRewards(&_RewardManagerTest.TransactOpts)
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_RewardManagerTest *RewardManagerTestTransactorSession) DisableRewards() (*types.Transaction, error) {
	return _RewardManagerTest.Contract.DisableRewards(&_RewardManagerTest.TransactOpts)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_RewardManagerTest *RewardManagerTestTransactor) SetRewardAddress(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _RewardManagerTest.contract.Transact(opts, "setRewardAddress", addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_RewardManagerTest *RewardManagerTestSession) SetRewardAddress(addr common.Address) (*types.Transaction, error) {
	return _RewardManagerTest.Contract.SetRewardAddress(&_RewardManagerTest.TransactOpts, addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_RewardManagerTest *RewardManagerTestTransactorSession) SetRewardAddress(addr common.Address) (*types.Transaction, error) {
	return _RewardManagerTest.Contract.SetRewardAddress(&_RewardManagerTest.TransactOpts, addr)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_RewardManagerTest *RewardManagerTestTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RewardManagerTest.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_RewardManagerTest *RewardManagerTestSession) Receive() (*types.Transaction, error) {
	return _RewardManagerTest.Contract.Receive(&_RewardManagerTest.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_RewardManagerTest *RewardManagerTestTransactorSession) Receive() (*types.Transaction, error) {
	return _RewardManagerTest.Contract.Receive(&_RewardManagerTest.TransactOpts)
}
