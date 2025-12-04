// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

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

// ExampleRewardManagerMetaData contains all meta data concerning the ExampleRewardManager contract.
var ExampleRewardManagerMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"OwnableInvalidOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"OwnableUnauthorizedAccount\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"allowFeeRecipients\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"areFeeRecipientsAllowed\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"currentRewardAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"disableRewards\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setRewardAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405273020000000000000000000000000000000000000460015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550348015610063575f5ffd5b50335f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff16036100d5575f6040517f1e4fbdf70000000000000000000000000000000000000000000000000000000081526004016100cc91906101ea565b60405180910390fd5b6100e4816100ea60201b60201c565b50610203565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6101d4826101ab565b9050919050565b6101e4816101ca565b82525050565b5f6020820190506101fd5f8301846101db565b92915050565b6107bb806102105f395ff3fe608060405234801561000f575f5ffd5b5060043610610086575f3560e01c8063bc17862811610059578063bc178628146100d8578063e915608b146100e2578063f2fde38b14610100578063f6542b2e1461011c57610086565b80630329099f1461008a5780635e00e67914610094578063715018a6146100b05780638da5cb5b146100ba575b5f5ffd5b61009261013a565b005b6100ae60048036038101906100a9919061066b565b6101c0565b005b6100b8610252565b005b6100c2610265565b6040516100cf91906106a5565b60405180910390f35b6100e061028c565b005b6100ea610312565b6040516100f791906106a5565b60405180910390f35b61011a6004803603810190610115919061066b565b6103a6565b005b61012461042a565b60405161013191906106d8565b60405180910390f35b6101426104be565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630329099f6040518163ffffffff1660e01b81526004015f604051808303815f87803b1580156101a8575f5ffd5b505af11580156101ba573d5f5f3e3d5ffd5b50505050565b6101c86104be565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16635e00e679826040518263ffffffff1660e01b815260040161022291906106a5565b5f604051808303815f87803b158015610239575f5ffd5b505af115801561024b573d5f5f3e3d5ffd5b5050505050565b61025a6104be565b6102635f610545565b565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b6102946104be565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663bc1786286040518163ffffffff1660e01b81526004015f604051808303815f87803b1580156102fa575f5ffd5b505af115801561030c573d5f5f3e3d5ffd5b50505050565b5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663e915608b6040518163ffffffff1660e01b8152600401602060405180830381865afa15801561037d573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103a19190610705565b905090565b6103ae6104be565b5f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff160361041e575f6040517f1e4fbdf700000000000000000000000000000000000000000000000000000000815260040161041591906106a5565b60405180910390fd5b61042781610545565b50565b5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663f6542b2e6040518163ffffffff1660e01b8152600401602060405180830381865afa158015610495573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906104b9919061075a565b905090565b6104c6610606565b73ffffffffffffffffffffffffffffffffffffffff166104e4610265565b73ffffffffffffffffffffffffffffffffffffffff161461054357610507610606565b6040517f118cdaa700000000000000000000000000000000000000000000000000000000815260040161053a91906106a5565b60405180910390fd5b565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b5f33905090565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61063a82610611565b9050919050565b61064a81610630565b8114610654575f5ffd5b50565b5f8135905061066581610641565b92915050565b5f602082840312156106805761067f61060d565b5b5f61068d84828501610657565b91505092915050565b61069f81610630565b82525050565b5f6020820190506106b85f830184610696565b92915050565b5f8115159050919050565b6106d2816106be565b82525050565b5f6020820190506106eb5f8301846106c9565b92915050565b5f815190506106ff81610641565b92915050565b5f6020828403121561071a5761071961060d565b5b5f610727848285016106f1565b91505092915050565b610739816106be565b8114610743575f5ffd5b50565b5f8151905061075481610730565b92915050565b5f6020828403121561076f5761076e61060d565b5b5f61077c84828501610746565b9150509291505056fea2646970667358221220f61773f415e121a56de4f02b984aa72fbdd2d94a3071a4718f845950cd2a511864736f6c634300081e0033",
}

// ExampleRewardManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use ExampleRewardManagerMetaData.ABI instead.
var ExampleRewardManagerABI = ExampleRewardManagerMetaData.ABI

// ExampleRewardManagerBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ExampleRewardManagerMetaData.Bin instead.
var ExampleRewardManagerBin = ExampleRewardManagerMetaData.Bin

// DeployExampleRewardManager deploys a new Ethereum contract, binding an instance of ExampleRewardManager to it.
func DeployExampleRewardManager(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ExampleRewardManager, error) {
	parsed, err := ExampleRewardManagerMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ExampleRewardManagerBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ExampleRewardManager{ExampleRewardManagerCaller: ExampleRewardManagerCaller{contract: contract}, ExampleRewardManagerTransactor: ExampleRewardManagerTransactor{contract: contract}, ExampleRewardManagerFilterer: ExampleRewardManagerFilterer{contract: contract}}, nil
}

// ExampleRewardManager is an auto generated Go binding around an Ethereum contract.
type ExampleRewardManager struct {
	ExampleRewardManagerCaller     // Read-only binding to the contract
	ExampleRewardManagerTransactor // Write-only binding to the contract
	ExampleRewardManagerFilterer   // Log filterer for contract events
}

// ExampleRewardManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type ExampleRewardManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleRewardManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ExampleRewardManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleRewardManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ExampleRewardManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleRewardManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ExampleRewardManagerSession struct {
	Contract     *ExampleRewardManager // Generic contract binding to set the session for
	CallOpts     bind.CallOpts         // Call options to use throughout this session
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// ExampleRewardManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ExampleRewardManagerCallerSession struct {
	Contract *ExampleRewardManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts               // Call options to use throughout this session
}

// ExampleRewardManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ExampleRewardManagerTransactorSession struct {
	Contract     *ExampleRewardManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts               // Transaction auth options to use throughout this session
}

// ExampleRewardManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type ExampleRewardManagerRaw struct {
	Contract *ExampleRewardManager // Generic contract binding to access the raw methods on
}

// ExampleRewardManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ExampleRewardManagerCallerRaw struct {
	Contract *ExampleRewardManagerCaller // Generic read-only contract binding to access the raw methods on
}

// ExampleRewardManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ExampleRewardManagerTransactorRaw struct {
	Contract *ExampleRewardManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewExampleRewardManager creates a new instance of ExampleRewardManager, bound to a specific deployed contract.
func NewExampleRewardManager(address common.Address, backend bind.ContractBackend) (*ExampleRewardManager, error) {
	contract, err := bindExampleRewardManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ExampleRewardManager{ExampleRewardManagerCaller: ExampleRewardManagerCaller{contract: contract}, ExampleRewardManagerTransactor: ExampleRewardManagerTransactor{contract: contract}, ExampleRewardManagerFilterer: ExampleRewardManagerFilterer{contract: contract}}, nil
}

// NewExampleRewardManagerCaller creates a new read-only instance of ExampleRewardManager, bound to a specific deployed contract.
func NewExampleRewardManagerCaller(address common.Address, caller bind.ContractCaller) (*ExampleRewardManagerCaller, error) {
	contract, err := bindExampleRewardManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleRewardManagerCaller{contract: contract}, nil
}

// NewExampleRewardManagerTransactor creates a new write-only instance of ExampleRewardManager, bound to a specific deployed contract.
func NewExampleRewardManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*ExampleRewardManagerTransactor, error) {
	contract, err := bindExampleRewardManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleRewardManagerTransactor{contract: contract}, nil
}

// NewExampleRewardManagerFilterer creates a new log filterer instance of ExampleRewardManager, bound to a specific deployed contract.
func NewExampleRewardManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*ExampleRewardManagerFilterer, error) {
	contract, err := bindExampleRewardManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ExampleRewardManagerFilterer{contract: contract}, nil
}

// bindExampleRewardManager binds a generic wrapper to an already deployed contract.
func bindExampleRewardManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ExampleRewardManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleRewardManager *ExampleRewardManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleRewardManager.Contract.ExampleRewardManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleRewardManager *ExampleRewardManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.ExampleRewardManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleRewardManager *ExampleRewardManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.ExampleRewardManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleRewardManager *ExampleRewardManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleRewardManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleRewardManager *ExampleRewardManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleRewardManager *ExampleRewardManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.contract.Transact(opts, method, params...)
}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool)
func (_ExampleRewardManager *ExampleRewardManagerCaller) AreFeeRecipientsAllowed(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _ExampleRewardManager.contract.Call(opts, &out, "areFeeRecipientsAllowed")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool)
func (_ExampleRewardManager *ExampleRewardManagerSession) AreFeeRecipientsAllowed() (bool, error) {
	return _ExampleRewardManager.Contract.AreFeeRecipientsAllowed(&_ExampleRewardManager.CallOpts)
}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool)
func (_ExampleRewardManager *ExampleRewardManagerCallerSession) AreFeeRecipientsAllowed() (bool, error) {
	return _ExampleRewardManager.Contract.AreFeeRecipientsAllowed(&_ExampleRewardManager.CallOpts)
}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address)
func (_ExampleRewardManager *ExampleRewardManagerCaller) CurrentRewardAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ExampleRewardManager.contract.Call(opts, &out, "currentRewardAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address)
func (_ExampleRewardManager *ExampleRewardManagerSession) CurrentRewardAddress() (common.Address, error) {
	return _ExampleRewardManager.Contract.CurrentRewardAddress(&_ExampleRewardManager.CallOpts)
}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address)
func (_ExampleRewardManager *ExampleRewardManagerCallerSession) CurrentRewardAddress() (common.Address, error) {
	return _ExampleRewardManager.Contract.CurrentRewardAddress(&_ExampleRewardManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleRewardManager *ExampleRewardManagerCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ExampleRewardManager.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleRewardManager *ExampleRewardManagerSession) Owner() (common.Address, error) {
	return _ExampleRewardManager.Contract.Owner(&_ExampleRewardManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleRewardManager *ExampleRewardManagerCallerSession) Owner() (common.Address, error) {
	return _ExampleRewardManager.Contract.Owner(&_ExampleRewardManager.CallOpts)
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactor) AllowFeeRecipients(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleRewardManager.contract.Transact(opts, "allowFeeRecipients")
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_ExampleRewardManager *ExampleRewardManagerSession) AllowFeeRecipients() (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.AllowFeeRecipients(&_ExampleRewardManager.TransactOpts)
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactorSession) AllowFeeRecipients() (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.AllowFeeRecipients(&_ExampleRewardManager.TransactOpts)
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactor) DisableRewards(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleRewardManager.contract.Transact(opts, "disableRewards")
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_ExampleRewardManager *ExampleRewardManagerSession) DisableRewards() (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.DisableRewards(&_ExampleRewardManager.TransactOpts)
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactorSession) DisableRewards() (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.DisableRewards(&_ExampleRewardManager.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleRewardManager.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleRewardManager *ExampleRewardManagerSession) RenounceOwnership() (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.RenounceOwnership(&_ExampleRewardManager.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.RenounceOwnership(&_ExampleRewardManager.TransactOpts)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactor) SetRewardAddress(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleRewardManager.contract.Transact(opts, "setRewardAddress", addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_ExampleRewardManager *ExampleRewardManagerSession) SetRewardAddress(addr common.Address) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.SetRewardAddress(&_ExampleRewardManager.TransactOpts, addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactorSession) SetRewardAddress(addr common.Address) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.SetRewardAddress(&_ExampleRewardManager.TransactOpts, addr)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _ExampleRewardManager.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleRewardManager *ExampleRewardManagerSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.TransferOwnership(&_ExampleRewardManager.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleRewardManager *ExampleRewardManagerTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ExampleRewardManager.Contract.TransferOwnership(&_ExampleRewardManager.TransactOpts, newOwner)
}

// ExampleRewardManagerOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the ExampleRewardManager contract.
type ExampleRewardManagerOwnershipTransferredIterator struct {
	Event *ExampleRewardManagerOwnershipTransferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ExampleRewardManagerOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ExampleRewardManagerOwnershipTransferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ExampleRewardManagerOwnershipTransferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ExampleRewardManagerOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ExampleRewardManagerOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ExampleRewardManagerOwnershipTransferred represents a OwnershipTransferred event raised by the ExampleRewardManager contract.
type ExampleRewardManagerOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleRewardManager *ExampleRewardManagerFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ExampleRewardManagerOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ExampleRewardManager.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ExampleRewardManagerOwnershipTransferredIterator{contract: _ExampleRewardManager.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleRewardManager *ExampleRewardManagerFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *ExampleRewardManagerOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ExampleRewardManager.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ExampleRewardManagerOwnershipTransferred)
				if err := _ExampleRewardManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleRewardManager *ExampleRewardManagerFilterer) ParseOwnershipTransferred(log types.Log) (*ExampleRewardManagerOwnershipTransferred, error) {
	event := new(ExampleRewardManagerOwnershipTransferred)
	if err := _ExampleRewardManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
