// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
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

// IRewardManagerMetaData contains all meta data concerning the IRewardManager contract.
var IRewardManagerMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"FeeRecipientsAllowed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"oldRewardAddress\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newRewardAddress\",\"type\":\"address\"}],\"name\":\"RewardAddressChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RewardsDisabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldRole\",\"type\":\"uint256\"}],\"name\":\"RoleSet\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"allowFeeRecipients\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"areFeeRecipientsAllowed\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"isAllowed\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"currentRewardAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"rewardAddress\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"disableRewards\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"readAllowList\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setNone\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setRewardAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// IRewardManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use IRewardManagerMetaData.ABI instead.
var IRewardManagerABI = IRewardManagerMetaData.ABI

// IRewardManager is an auto generated Go binding around an Ethereum contract.
type IRewardManager struct {
	IRewardManagerCaller     // Read-only binding to the contract
	IRewardManagerTransactor // Write-only binding to the contract
	IRewardManagerFilterer   // Log filterer for contract events
}

// IRewardManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type IRewardManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IRewardManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type IRewardManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IRewardManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type IRewardManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IRewardManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type IRewardManagerSession struct {
	Contract     *IRewardManager   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// IRewardManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type IRewardManagerCallerSession struct {
	Contract *IRewardManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// IRewardManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type IRewardManagerTransactorSession struct {
	Contract     *IRewardManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// IRewardManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type IRewardManagerRaw struct {
	Contract *IRewardManager // Generic contract binding to access the raw methods on
}

// IRewardManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type IRewardManagerCallerRaw struct {
	Contract *IRewardManagerCaller // Generic read-only contract binding to access the raw methods on
}

// IRewardManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type IRewardManagerTransactorRaw struct {
	Contract *IRewardManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewIRewardManager creates a new instance of IRewardManager, bound to a specific deployed contract.
func NewIRewardManager(address common.Address, backend bind.ContractBackend) (*IRewardManager, error) {
	contract, err := bindIRewardManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &IRewardManager{IRewardManagerCaller: IRewardManagerCaller{contract: contract}, IRewardManagerTransactor: IRewardManagerTransactor{contract: contract}, IRewardManagerFilterer: IRewardManagerFilterer{contract: contract}}, nil
}

// NewIRewardManagerCaller creates a new read-only instance of IRewardManager, bound to a specific deployed contract.
func NewIRewardManagerCaller(address common.Address, caller bind.ContractCaller) (*IRewardManagerCaller, error) {
	contract, err := bindIRewardManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerCaller{contract: contract}, nil
}

// NewIRewardManagerTransactor creates a new write-only instance of IRewardManager, bound to a specific deployed contract.
func NewIRewardManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*IRewardManagerTransactor, error) {
	contract, err := bindIRewardManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerTransactor{contract: contract}, nil
}

// NewIRewardManagerFilterer creates a new log filterer instance of IRewardManager, bound to a specific deployed contract.
func NewIRewardManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*IRewardManagerFilterer, error) {
	contract, err := bindIRewardManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerFilterer{contract: contract}, nil
}

// bindIRewardManager binds a generic wrapper to an already deployed contract.
func bindIRewardManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := IRewardManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IRewardManager *IRewardManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IRewardManager.Contract.IRewardManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IRewardManager *IRewardManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IRewardManager.Contract.IRewardManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IRewardManager *IRewardManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IRewardManager.Contract.IRewardManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IRewardManager *IRewardManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IRewardManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IRewardManager *IRewardManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IRewardManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IRewardManager *IRewardManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IRewardManager.Contract.contract.Transact(opts, method, params...)
}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool isAllowed)
func (_IRewardManager *IRewardManagerCaller) AreFeeRecipientsAllowed(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _IRewardManager.contract.Call(opts, &out, "areFeeRecipientsAllowed")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool isAllowed)
func (_IRewardManager *IRewardManagerSession) AreFeeRecipientsAllowed() (bool, error) {
	return _IRewardManager.Contract.AreFeeRecipientsAllowed(&_IRewardManager.CallOpts)
}

// AreFeeRecipientsAllowed is a free data retrieval call binding the contract method 0xf6542b2e.
//
// Solidity: function areFeeRecipientsAllowed() view returns(bool isAllowed)
func (_IRewardManager *IRewardManagerCallerSession) AreFeeRecipientsAllowed() (bool, error) {
	return _IRewardManager.Contract.AreFeeRecipientsAllowed(&_IRewardManager.CallOpts)
}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address rewardAddress)
func (_IRewardManager *IRewardManagerCaller) CurrentRewardAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _IRewardManager.contract.Call(opts, &out, "currentRewardAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address rewardAddress)
func (_IRewardManager *IRewardManagerSession) CurrentRewardAddress() (common.Address, error) {
	return _IRewardManager.Contract.CurrentRewardAddress(&_IRewardManager.CallOpts)
}

// CurrentRewardAddress is a free data retrieval call binding the contract method 0xe915608b.
//
// Solidity: function currentRewardAddress() view returns(address rewardAddress)
func (_IRewardManager *IRewardManagerCallerSession) CurrentRewardAddress() (common.Address, error) {
	return _IRewardManager.Contract.CurrentRewardAddress(&_IRewardManager.CallOpts)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IRewardManager *IRewardManagerCaller) ReadAllowList(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _IRewardManager.contract.Call(opts, &out, "readAllowList", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IRewardManager *IRewardManagerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IRewardManager.Contract.ReadAllowList(&_IRewardManager.CallOpts, addr)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IRewardManager *IRewardManagerCallerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IRewardManager.Contract.ReadAllowList(&_IRewardManager.CallOpts, addr)
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_IRewardManager *IRewardManagerTransactor) AllowFeeRecipients(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "allowFeeRecipients")
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_IRewardManager *IRewardManagerSession) AllowFeeRecipients() (*types.Transaction, error) {
	return _IRewardManager.Contract.AllowFeeRecipients(&_IRewardManager.TransactOpts)
}

// AllowFeeRecipients is a paid mutator transaction binding the contract method 0x0329099f.
//
// Solidity: function allowFeeRecipients() returns()
func (_IRewardManager *IRewardManagerTransactorSession) AllowFeeRecipients() (*types.Transaction, error) {
	return _IRewardManager.Contract.AllowFeeRecipients(&_IRewardManager.TransactOpts)
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_IRewardManager *IRewardManagerTransactor) DisableRewards(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "disableRewards")
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_IRewardManager *IRewardManagerSession) DisableRewards() (*types.Transaction, error) {
	return _IRewardManager.Contract.DisableRewards(&_IRewardManager.TransactOpts)
}

// DisableRewards is a paid mutator transaction binding the contract method 0xbc178628.
//
// Solidity: function disableRewards() returns()
func (_IRewardManager *IRewardManagerTransactorSession) DisableRewards() (*types.Transaction, error) {
	return _IRewardManager.Contract.DisableRewards(&_IRewardManager.TransactOpts)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IRewardManager *IRewardManagerTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IRewardManager *IRewardManagerSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetAdmin(&_IRewardManager.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IRewardManager *IRewardManagerTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetAdmin(&_IRewardManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IRewardManager *IRewardManagerTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IRewardManager *IRewardManagerSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetEnabled(&_IRewardManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IRewardManager *IRewardManagerTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetEnabled(&_IRewardManager.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IRewardManager *IRewardManagerTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IRewardManager *IRewardManagerSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetManager(&_IRewardManager.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IRewardManager *IRewardManagerTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetManager(&_IRewardManager.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IRewardManager *IRewardManagerTransactor) SetNone(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "setNone", addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IRewardManager *IRewardManagerSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetNone(&_IRewardManager.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IRewardManager *IRewardManagerTransactorSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetNone(&_IRewardManager.TransactOpts, addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_IRewardManager *IRewardManagerTransactor) SetRewardAddress(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.contract.Transact(opts, "setRewardAddress", addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_IRewardManager *IRewardManagerSession) SetRewardAddress(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetRewardAddress(&_IRewardManager.TransactOpts, addr)
}

// SetRewardAddress is a paid mutator transaction binding the contract method 0x5e00e679.
//
// Solidity: function setRewardAddress(address addr) returns()
func (_IRewardManager *IRewardManagerTransactorSession) SetRewardAddress(addr common.Address) (*types.Transaction, error) {
	return _IRewardManager.Contract.SetRewardAddress(&_IRewardManager.TransactOpts, addr)
}

// IRewardManagerFeeRecipientsAllowedIterator is returned from FilterFeeRecipientsAllowed and is used to iterate over the raw logs and unpacked data for FeeRecipientsAllowed events raised by the IRewardManager contract.
type IRewardManagerFeeRecipientsAllowedIterator struct {
	Event *IRewardManagerFeeRecipientsAllowed // Event containing the contract specifics and raw log

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
func (it *IRewardManagerFeeRecipientsAllowedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IRewardManagerFeeRecipientsAllowed)
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
		it.Event = new(IRewardManagerFeeRecipientsAllowed)
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
func (it *IRewardManagerFeeRecipientsAllowedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IRewardManagerFeeRecipientsAllowedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IRewardManagerFeeRecipientsAllowed represents a FeeRecipientsAllowed event raised by the IRewardManager contract.
type IRewardManagerFeeRecipientsAllowed struct {
	Sender common.Address
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterFeeRecipientsAllowed is a free log retrieval operation binding the contract event 0xabb1949bd129fef9b84601a48aee89d600d90074ca10216a02ce43996be55991.
//
// Solidity: event FeeRecipientsAllowed(address indexed sender)
func (_IRewardManager *IRewardManagerFilterer) FilterFeeRecipientsAllowed(opts *bind.FilterOpts, sender []common.Address) (*IRewardManagerFeeRecipientsAllowedIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IRewardManager.contract.FilterLogs(opts, "FeeRecipientsAllowed", senderRule)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerFeeRecipientsAllowedIterator{contract: _IRewardManager.contract, event: "FeeRecipientsAllowed", logs: logs, sub: sub}, nil
}

// WatchFeeRecipientsAllowed is a free log subscription operation binding the contract event 0xabb1949bd129fef9b84601a48aee89d600d90074ca10216a02ce43996be55991.
//
// Solidity: event FeeRecipientsAllowed(address indexed sender)
func (_IRewardManager *IRewardManagerFilterer) WatchFeeRecipientsAllowed(opts *bind.WatchOpts, sink chan<- *IRewardManagerFeeRecipientsAllowed, sender []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IRewardManager.contract.WatchLogs(opts, "FeeRecipientsAllowed", senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IRewardManagerFeeRecipientsAllowed)
				if err := _IRewardManager.contract.UnpackLog(event, "FeeRecipientsAllowed", log); err != nil {
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

// ParseFeeRecipientsAllowed is a log parse operation binding the contract event 0xabb1949bd129fef9b84601a48aee89d600d90074ca10216a02ce43996be55991.
//
// Solidity: event FeeRecipientsAllowed(address indexed sender)
func (_IRewardManager *IRewardManagerFilterer) ParseFeeRecipientsAllowed(log types.Log) (*IRewardManagerFeeRecipientsAllowed, error) {
	event := new(IRewardManagerFeeRecipientsAllowed)
	if err := _IRewardManager.contract.UnpackLog(event, "FeeRecipientsAllowed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// IRewardManagerRewardAddressChangedIterator is returned from FilterRewardAddressChanged and is used to iterate over the raw logs and unpacked data for RewardAddressChanged events raised by the IRewardManager contract.
type IRewardManagerRewardAddressChangedIterator struct {
	Event *IRewardManagerRewardAddressChanged // Event containing the contract specifics and raw log

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
func (it *IRewardManagerRewardAddressChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IRewardManagerRewardAddressChanged)
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
		it.Event = new(IRewardManagerRewardAddressChanged)
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
func (it *IRewardManagerRewardAddressChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IRewardManagerRewardAddressChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IRewardManagerRewardAddressChanged represents a RewardAddressChanged event raised by the IRewardManager contract.
type IRewardManagerRewardAddressChanged struct {
	Sender           common.Address
	OldRewardAddress common.Address
	NewRewardAddress common.Address
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterRewardAddressChanged is a free log retrieval operation binding the contract event 0xc2a9e07cba6f4920acaa5933bd0406949d5dbef7ee698e786ea23e8708f32a6c.
//
// Solidity: event RewardAddressChanged(address indexed sender, address indexed oldRewardAddress, address indexed newRewardAddress)
func (_IRewardManager *IRewardManagerFilterer) FilterRewardAddressChanged(opts *bind.FilterOpts, sender []common.Address, oldRewardAddress []common.Address, newRewardAddress []common.Address) (*IRewardManagerRewardAddressChangedIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var oldRewardAddressRule []interface{}
	for _, oldRewardAddressItem := range oldRewardAddress {
		oldRewardAddressRule = append(oldRewardAddressRule, oldRewardAddressItem)
	}
	var newRewardAddressRule []interface{}
	for _, newRewardAddressItem := range newRewardAddress {
		newRewardAddressRule = append(newRewardAddressRule, newRewardAddressItem)
	}

	logs, sub, err := _IRewardManager.contract.FilterLogs(opts, "RewardAddressChanged", senderRule, oldRewardAddressRule, newRewardAddressRule)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerRewardAddressChangedIterator{contract: _IRewardManager.contract, event: "RewardAddressChanged", logs: logs, sub: sub}, nil
}

// WatchRewardAddressChanged is a free log subscription operation binding the contract event 0xc2a9e07cba6f4920acaa5933bd0406949d5dbef7ee698e786ea23e8708f32a6c.
//
// Solidity: event RewardAddressChanged(address indexed sender, address indexed oldRewardAddress, address indexed newRewardAddress)
func (_IRewardManager *IRewardManagerFilterer) WatchRewardAddressChanged(opts *bind.WatchOpts, sink chan<- *IRewardManagerRewardAddressChanged, sender []common.Address, oldRewardAddress []common.Address, newRewardAddress []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var oldRewardAddressRule []interface{}
	for _, oldRewardAddressItem := range oldRewardAddress {
		oldRewardAddressRule = append(oldRewardAddressRule, oldRewardAddressItem)
	}
	var newRewardAddressRule []interface{}
	for _, newRewardAddressItem := range newRewardAddress {
		newRewardAddressRule = append(newRewardAddressRule, newRewardAddressItem)
	}

	logs, sub, err := _IRewardManager.contract.WatchLogs(opts, "RewardAddressChanged", senderRule, oldRewardAddressRule, newRewardAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IRewardManagerRewardAddressChanged)
				if err := _IRewardManager.contract.UnpackLog(event, "RewardAddressChanged", log); err != nil {
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

// ParseRewardAddressChanged is a log parse operation binding the contract event 0xc2a9e07cba6f4920acaa5933bd0406949d5dbef7ee698e786ea23e8708f32a6c.
//
// Solidity: event RewardAddressChanged(address indexed sender, address indexed oldRewardAddress, address indexed newRewardAddress)
func (_IRewardManager *IRewardManagerFilterer) ParseRewardAddressChanged(log types.Log) (*IRewardManagerRewardAddressChanged, error) {
	event := new(IRewardManagerRewardAddressChanged)
	if err := _IRewardManager.contract.UnpackLog(event, "RewardAddressChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// IRewardManagerRewardsDisabledIterator is returned from FilterRewardsDisabled and is used to iterate over the raw logs and unpacked data for RewardsDisabled events raised by the IRewardManager contract.
type IRewardManagerRewardsDisabledIterator struct {
	Event *IRewardManagerRewardsDisabled // Event containing the contract specifics and raw log

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
func (it *IRewardManagerRewardsDisabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IRewardManagerRewardsDisabled)
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
		it.Event = new(IRewardManagerRewardsDisabled)
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
func (it *IRewardManagerRewardsDisabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IRewardManagerRewardsDisabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IRewardManagerRewardsDisabled represents a RewardsDisabled event raised by the IRewardManager contract.
type IRewardManagerRewardsDisabled struct {
	Sender common.Address
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterRewardsDisabled is a free log retrieval operation binding the contract event 0xeb121f0335efe8f4b8ebef7793c18c171834696989656a8c345acc558359fabf.
//
// Solidity: event RewardsDisabled(address indexed sender)
func (_IRewardManager *IRewardManagerFilterer) FilterRewardsDisabled(opts *bind.FilterOpts, sender []common.Address) (*IRewardManagerRewardsDisabledIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IRewardManager.contract.FilterLogs(opts, "RewardsDisabled", senderRule)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerRewardsDisabledIterator{contract: _IRewardManager.contract, event: "RewardsDisabled", logs: logs, sub: sub}, nil
}

// WatchRewardsDisabled is a free log subscription operation binding the contract event 0xeb121f0335efe8f4b8ebef7793c18c171834696989656a8c345acc558359fabf.
//
// Solidity: event RewardsDisabled(address indexed sender)
func (_IRewardManager *IRewardManagerFilterer) WatchRewardsDisabled(opts *bind.WatchOpts, sink chan<- *IRewardManagerRewardsDisabled, sender []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IRewardManager.contract.WatchLogs(opts, "RewardsDisabled", senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IRewardManagerRewardsDisabled)
				if err := _IRewardManager.contract.UnpackLog(event, "RewardsDisabled", log); err != nil {
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

// ParseRewardsDisabled is a log parse operation binding the contract event 0xeb121f0335efe8f4b8ebef7793c18c171834696989656a8c345acc558359fabf.
//
// Solidity: event RewardsDisabled(address indexed sender)
func (_IRewardManager *IRewardManagerFilterer) ParseRewardsDisabled(log types.Log) (*IRewardManagerRewardsDisabled, error) {
	event := new(IRewardManagerRewardsDisabled)
	if err := _IRewardManager.contract.UnpackLog(event, "RewardsDisabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// IRewardManagerRoleSetIterator is returned from FilterRoleSet and is used to iterate over the raw logs and unpacked data for RoleSet events raised by the IRewardManager contract.
type IRewardManagerRoleSetIterator struct {
	Event *IRewardManagerRoleSet // Event containing the contract specifics and raw log

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
func (it *IRewardManagerRoleSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IRewardManagerRoleSet)
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
		it.Event = new(IRewardManagerRoleSet)
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
func (it *IRewardManagerRoleSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IRewardManagerRoleSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IRewardManagerRoleSet represents a RoleSet event raised by the IRewardManager contract.
type IRewardManagerRoleSet struct {
	Role    *big.Int
	Account common.Address
	Sender  common.Address
	OldRole *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleSet is a free log retrieval operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IRewardManager *IRewardManagerFilterer) FilterRoleSet(opts *bind.FilterOpts, role []*big.Int, account []common.Address, sender []common.Address) (*IRewardManagerRoleSetIterator, error) {

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

	logs, sub, err := _IRewardManager.contract.FilterLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &IRewardManagerRoleSetIterator{contract: _IRewardManager.contract, event: "RoleSet", logs: logs, sub: sub}, nil
}

// WatchRoleSet is a free log subscription operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IRewardManager *IRewardManagerFilterer) WatchRoleSet(opts *bind.WatchOpts, sink chan<- *IRewardManagerRoleSet, role []*big.Int, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _IRewardManager.contract.WatchLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IRewardManagerRoleSet)
				if err := _IRewardManager.contract.UnpackLog(event, "RoleSet", log); err != nil {
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
func (_IRewardManager *IRewardManagerFilterer) ParseRoleSet(log types.Log) (*IRewardManagerRoleSet, error) {
	event := new(IRewardManagerRoleSet)
	if err := _IRewardManager.contract.UnpackLog(event, "RoleSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
