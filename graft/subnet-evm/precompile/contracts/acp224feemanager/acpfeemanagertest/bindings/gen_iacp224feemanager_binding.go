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

// IACP224FeeManagerFeeConfig is an auto generated low-level Go binding around an user-defined struct.
type IACP224FeeManagerFeeConfig struct {
	TargetGas         *big.Int
	MinGasPrice       *big.Int
	MaxCapacityFactor *big.Int
	TimeToDouble      *big.Int
}

// IACP224FeeManagerMetaData contains all meta data concerning the IACP224FeeManager contract.
var IACP224FeeManagerMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minGasPrice\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxCapacityFactor\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"timeToDouble\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structIACP224FeeManager.FeeConfig\",\"name\":\"oldFeeConfig\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minGasPrice\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxCapacityFactor\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"timeToDouble\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structIACP224FeeManager.FeeConfig\",\"name\":\"newFeeConfig\",\"type\":\"tuple\"}],\"name\":\"FeeConfigUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldRole\",\"type\":\"uint256\"}],\"name\":\"RoleSet\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"getFeeConfig\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minGasPrice\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxCapacityFactor\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"timeToDouble\",\"type\":\"uint256\"}],\"internalType\":\"structIACP224FeeManager.FeeConfig\",\"name\":\"config\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getFeeConfigLastChangedAt\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"readAllowList\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minGasPrice\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxCapacityFactor\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"timeToDouble\",\"type\":\"uint256\"}],\"internalType\":\"structIACP224FeeManager.FeeConfig\",\"name\":\"config\",\"type\":\"tuple\"}],\"name\":\"setFeeConfig\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setNone\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// IACP224FeeManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use IACP224FeeManagerMetaData.ABI instead.
var IACP224FeeManagerABI = IACP224FeeManagerMetaData.ABI

// IACP224FeeManager is an auto generated Go binding around an Ethereum contract.
type IACP224FeeManager struct {
	IACP224FeeManagerCaller     // Read-only binding to the contract
	IACP224FeeManagerTransactor // Write-only binding to the contract
	IACP224FeeManagerFilterer   // Log filterer for contract events
}

// IACP224FeeManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type IACP224FeeManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IACP224FeeManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type IACP224FeeManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IACP224FeeManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type IACP224FeeManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IACP224FeeManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type IACP224FeeManagerSession struct {
	Contract     *IACP224FeeManager // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// IACP224FeeManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type IACP224FeeManagerCallerSession struct {
	Contract *IACP224FeeManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// IACP224FeeManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type IACP224FeeManagerTransactorSession struct {
	Contract     *IACP224FeeManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// IACP224FeeManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type IACP224FeeManagerRaw struct {
	Contract *IACP224FeeManager // Generic contract binding to access the raw methods on
}

// IACP224FeeManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type IACP224FeeManagerCallerRaw struct {
	Contract *IACP224FeeManagerCaller // Generic read-only contract binding to access the raw methods on
}

// IACP224FeeManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type IACP224FeeManagerTransactorRaw struct {
	Contract *IACP224FeeManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewIACP224FeeManager creates a new instance of IACP224FeeManager, bound to a specific deployed contract.
func NewIACP224FeeManager(address common.Address, backend bind.ContractBackend) (*IACP224FeeManager, error) {
	contract, err := bindIACP224FeeManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &IACP224FeeManager{IACP224FeeManagerCaller: IACP224FeeManagerCaller{contract: contract}, IACP224FeeManagerTransactor: IACP224FeeManagerTransactor{contract: contract}, IACP224FeeManagerFilterer: IACP224FeeManagerFilterer{contract: contract}}, nil
}

// NewIACP224FeeManagerCaller creates a new read-only instance of IACP224FeeManager, bound to a specific deployed contract.
func NewIACP224FeeManagerCaller(address common.Address, caller bind.ContractCaller) (*IACP224FeeManagerCaller, error) {
	contract, err := bindIACP224FeeManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &IACP224FeeManagerCaller{contract: contract}, nil
}

// NewIACP224FeeManagerTransactor creates a new write-only instance of IACP224FeeManager, bound to a specific deployed contract.
func NewIACP224FeeManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*IACP224FeeManagerTransactor, error) {
	contract, err := bindIACP224FeeManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &IACP224FeeManagerTransactor{contract: contract}, nil
}

// NewIACP224FeeManagerFilterer creates a new log filterer instance of IACP224FeeManager, bound to a specific deployed contract.
func NewIACP224FeeManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*IACP224FeeManagerFilterer, error) {
	contract, err := bindIACP224FeeManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &IACP224FeeManagerFilterer{contract: contract}, nil
}

// bindIACP224FeeManager binds a generic wrapper to an already deployed contract.
func bindIACP224FeeManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := IACP224FeeManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IACP224FeeManager *IACP224FeeManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IACP224FeeManager.Contract.IACP224FeeManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IACP224FeeManager *IACP224FeeManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.IACP224FeeManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IACP224FeeManager *IACP224FeeManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.IACP224FeeManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IACP224FeeManager *IACP224FeeManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IACP224FeeManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IACP224FeeManager *IACP224FeeManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IACP224FeeManager *IACP224FeeManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.contract.Transact(opts, method, params...)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns((uint256,uint256,uint256,uint256) config)
func (_IACP224FeeManager *IACP224FeeManagerCaller) GetFeeConfig(opts *bind.CallOpts) (IACP224FeeManagerFeeConfig, error) {
	var out []interface{}
	err := _IACP224FeeManager.contract.Call(opts, &out, "getFeeConfig")

	if err != nil {
		return *new(IACP224FeeManagerFeeConfig), err
	}

	out0 := *abi.ConvertType(out[0], new(IACP224FeeManagerFeeConfig)).(*IACP224FeeManagerFeeConfig)

	return out0, err

}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns((uint256,uint256,uint256,uint256) config)
func (_IACP224FeeManager *IACP224FeeManagerSession) GetFeeConfig() (IACP224FeeManagerFeeConfig, error) {
	return _IACP224FeeManager.Contract.GetFeeConfig(&_IACP224FeeManager.CallOpts)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns((uint256,uint256,uint256,uint256) config)
func (_IACP224FeeManager *IACP224FeeManagerCallerSession) GetFeeConfig() (IACP224FeeManagerFeeConfig, error) {
	return _IACP224FeeManager.Contract.GetFeeConfig(&_IACP224FeeManager.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256 blockNumber)
func (_IACP224FeeManager *IACP224FeeManagerCaller) GetFeeConfigLastChangedAt(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _IACP224FeeManager.contract.Call(opts, &out, "getFeeConfigLastChangedAt")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256 blockNumber)
func (_IACP224FeeManager *IACP224FeeManagerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _IACP224FeeManager.Contract.GetFeeConfigLastChangedAt(&_IACP224FeeManager.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256 blockNumber)
func (_IACP224FeeManager *IACP224FeeManagerCallerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _IACP224FeeManager.Contract.GetFeeConfigLastChangedAt(&_IACP224FeeManager.CallOpts)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IACP224FeeManager *IACP224FeeManagerCaller) ReadAllowList(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _IACP224FeeManager.contract.Call(opts, &out, "readAllowList", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IACP224FeeManager *IACP224FeeManagerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IACP224FeeManager.Contract.ReadAllowList(&_IACP224FeeManager.CallOpts, addr)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IACP224FeeManager *IACP224FeeManagerCallerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IACP224FeeManager.Contract.ReadAllowList(&_IACP224FeeManager.CallOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetAdmin(&_IACP224FeeManager.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetAdmin(&_IACP224FeeManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetEnabled(&_IACP224FeeManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetEnabled(&_IACP224FeeManager.TransactOpts, addr)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0xa9eddf99.
//
// Solidity: function setFeeConfig((uint256,uint256,uint256,uint256) config) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactor) SetFeeConfig(opts *bind.TransactOpts, config IACP224FeeManagerFeeConfig) (*types.Transaction, error) {
	return _IACP224FeeManager.contract.Transact(opts, "setFeeConfig", config)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0xa9eddf99.
//
// Solidity: function setFeeConfig((uint256,uint256,uint256,uint256) config) returns()
func (_IACP224FeeManager *IACP224FeeManagerSession) SetFeeConfig(config IACP224FeeManagerFeeConfig) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetFeeConfig(&_IACP224FeeManager.TransactOpts, config)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0xa9eddf99.
//
// Solidity: function setFeeConfig((uint256,uint256,uint256,uint256) config) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactorSession) SetFeeConfig(config IACP224FeeManagerFeeConfig) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetFeeConfig(&_IACP224FeeManager.TransactOpts, config)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetManager(&_IACP224FeeManager.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetManager(&_IACP224FeeManager.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactor) SetNone(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.contract.Transact(opts, "setNone", addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetNone(&_IACP224FeeManager.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IACP224FeeManager *IACP224FeeManagerTransactorSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IACP224FeeManager.Contract.SetNone(&_IACP224FeeManager.TransactOpts, addr)
}

// IACP224FeeManagerFeeConfigUpdatedIterator is returned from FilterFeeConfigUpdated and is used to iterate over the raw logs and unpacked data for FeeConfigUpdated events raised by the IACP224FeeManager contract.
type IACP224FeeManagerFeeConfigUpdatedIterator struct {
	Event *IACP224FeeManagerFeeConfigUpdated // Event containing the contract specifics and raw log

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
func (it *IACP224FeeManagerFeeConfigUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IACP224FeeManagerFeeConfigUpdated)
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
		it.Event = new(IACP224FeeManagerFeeConfigUpdated)
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
func (it *IACP224FeeManagerFeeConfigUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IACP224FeeManagerFeeConfigUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IACP224FeeManagerFeeConfigUpdated represents a FeeConfigUpdated event raised by the IACP224FeeManager contract.
type IACP224FeeManagerFeeConfigUpdated struct {
	Sender       common.Address
	OldFeeConfig IACP224FeeManagerFeeConfig
	NewFeeConfig IACP224FeeManagerFeeConfig
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterFeeConfigUpdated is a free log retrieval operation binding the contract event 0x7c8f55342e5773f33fd3d86417b12680af9a76fa95ed879aa56cd61c9fff25f6.
//
// Solidity: event FeeConfigUpdated(address indexed sender, (uint256,uint256,uint256,uint256) oldFeeConfig, (uint256,uint256,uint256,uint256) newFeeConfig)
func (_IACP224FeeManager *IACP224FeeManagerFilterer) FilterFeeConfigUpdated(opts *bind.FilterOpts, sender []common.Address) (*IACP224FeeManagerFeeConfigUpdatedIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IACP224FeeManager.contract.FilterLogs(opts, "FeeConfigUpdated", senderRule)
	if err != nil {
		return nil, err
	}
	return &IACP224FeeManagerFeeConfigUpdatedIterator{contract: _IACP224FeeManager.contract, event: "FeeConfigUpdated", logs: logs, sub: sub}, nil
}

// WatchFeeConfigUpdated is a free log subscription operation binding the contract event 0x7c8f55342e5773f33fd3d86417b12680af9a76fa95ed879aa56cd61c9fff25f6.
//
// Solidity: event FeeConfigUpdated(address indexed sender, (uint256,uint256,uint256,uint256) oldFeeConfig, (uint256,uint256,uint256,uint256) newFeeConfig)
func (_IACP224FeeManager *IACP224FeeManagerFilterer) WatchFeeConfigUpdated(opts *bind.WatchOpts, sink chan<- *IACP224FeeManagerFeeConfigUpdated, sender []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IACP224FeeManager.contract.WatchLogs(opts, "FeeConfigUpdated", senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IACP224FeeManagerFeeConfigUpdated)
				if err := _IACP224FeeManager.contract.UnpackLog(event, "FeeConfigUpdated", log); err != nil {
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

// ParseFeeConfigUpdated is a log parse operation binding the contract event 0x7c8f55342e5773f33fd3d86417b12680af9a76fa95ed879aa56cd61c9fff25f6.
//
// Solidity: event FeeConfigUpdated(address indexed sender, (uint256,uint256,uint256,uint256) oldFeeConfig, (uint256,uint256,uint256,uint256) newFeeConfig)
func (_IACP224FeeManager *IACP224FeeManagerFilterer) ParseFeeConfigUpdated(log types.Log) (*IACP224FeeManagerFeeConfigUpdated, error) {
	event := new(IACP224FeeManagerFeeConfigUpdated)
	if err := _IACP224FeeManager.contract.UnpackLog(event, "FeeConfigUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// IACP224FeeManagerRoleSetIterator is returned from FilterRoleSet and is used to iterate over the raw logs and unpacked data for RoleSet events raised by the IACP224FeeManager contract.
type IACP224FeeManagerRoleSetIterator struct {
	Event *IACP224FeeManagerRoleSet // Event containing the contract specifics and raw log

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
func (it *IACP224FeeManagerRoleSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IACP224FeeManagerRoleSet)
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
		it.Event = new(IACP224FeeManagerRoleSet)
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
func (it *IACP224FeeManagerRoleSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IACP224FeeManagerRoleSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IACP224FeeManagerRoleSet represents a RoleSet event raised by the IACP224FeeManager contract.
type IACP224FeeManagerRoleSet struct {
	Role    *big.Int
	Account common.Address
	Sender  common.Address
	OldRole *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleSet is a free log retrieval operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IACP224FeeManager *IACP224FeeManagerFilterer) FilterRoleSet(opts *bind.FilterOpts, role []*big.Int, account []common.Address, sender []common.Address) (*IACP224FeeManagerRoleSetIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IACP224FeeManager.contract.FilterLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &IACP224FeeManagerRoleSetIterator{contract: _IACP224FeeManager.contract, event: "RoleSet", logs: logs, sub: sub}, nil
}

// WatchRoleSet is a free log subscription operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IACP224FeeManager *IACP224FeeManagerFilterer) WatchRoleSet(opts *bind.WatchOpts, sink chan<- *IACP224FeeManagerRoleSet, role []*big.Int, account []common.Address, sender []common.Address) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IACP224FeeManager.contract.WatchLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IACP224FeeManagerRoleSet)
				if err := _IACP224FeeManager.contract.UnpackLog(event, "RoleSet", log); err != nil {
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

// ParseRoleSet is a log parse operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IACP224FeeManager *IACP224FeeManagerFilterer) ParseRoleSet(log types.Log) (*IACP224FeeManagerRoleSet, error) {
	event := new(IACP224FeeManagerRoleSet)
	if err := _IACP224FeeManager.contract.UnpackLog(event, "RoleSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
