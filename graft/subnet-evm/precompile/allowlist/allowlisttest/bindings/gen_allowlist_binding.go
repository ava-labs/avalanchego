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

// IAllowListMetaData contains all meta data concerning the IAllowList contract.
var IAllowListMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldRole\",\"type\":\"uint256\"}],\"name\":\"RoleSet\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"readAllowList\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setNone\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// IAllowListABI is the input ABI used to generate the binding from.
// Deprecated: Use IAllowListMetaData.ABI instead.
var IAllowListABI = IAllowListMetaData.ABI

// IAllowList is an auto generated Go binding around an Ethereum contract.
type IAllowList struct {
	IAllowListCaller     // Read-only binding to the contract
	IAllowListTransactor // Write-only binding to the contract
	IAllowListFilterer   // Log filterer for contract events
}

// IAllowListCaller is an auto generated read-only Go binding around an Ethereum contract.
type IAllowListCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IAllowListTransactor is an auto generated write-only Go binding around an Ethereum contract.
type IAllowListTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IAllowListFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type IAllowListFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IAllowListSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type IAllowListSession struct {
	Contract     *IAllowList       // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// IAllowListCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type IAllowListCallerSession struct {
	Contract *IAllowListCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts     // Call options to use throughout this session
}

// IAllowListTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type IAllowListTransactorSession struct {
	Contract     *IAllowListTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// IAllowListRaw is an auto generated low-level Go binding around an Ethereum contract.
type IAllowListRaw struct {
	Contract *IAllowList // Generic contract binding to access the raw methods on
}

// IAllowListCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type IAllowListCallerRaw struct {
	Contract *IAllowListCaller // Generic read-only contract binding to access the raw methods on
}

// IAllowListTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type IAllowListTransactorRaw struct {
	Contract *IAllowListTransactor // Generic write-only contract binding to access the raw methods on
}

// NewIAllowList creates a new instance of IAllowList, bound to a specific deployed contract.
func NewIAllowList(address common.Address, backend bind.ContractBackend) (*IAllowList, error) {
	contract, err := bindIAllowList(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &IAllowList{IAllowListCaller: IAllowListCaller{contract: contract}, IAllowListTransactor: IAllowListTransactor{contract: contract}, IAllowListFilterer: IAllowListFilterer{contract: contract}}, nil
}

// NewIAllowListCaller creates a new read-only instance of IAllowList, bound to a specific deployed contract.
func NewIAllowListCaller(address common.Address, caller bind.ContractCaller) (*IAllowListCaller, error) {
	contract, err := bindIAllowList(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &IAllowListCaller{contract: contract}, nil
}

// NewIAllowListTransactor creates a new write-only instance of IAllowList, bound to a specific deployed contract.
func NewIAllowListTransactor(address common.Address, transactor bind.ContractTransactor) (*IAllowListTransactor, error) {
	contract, err := bindIAllowList(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &IAllowListTransactor{contract: contract}, nil
}

// NewIAllowListFilterer creates a new log filterer instance of IAllowList, bound to a specific deployed contract.
func NewIAllowListFilterer(address common.Address, filterer bind.ContractFilterer) (*IAllowListFilterer, error) {
	contract, err := bindIAllowList(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &IAllowListFilterer{contract: contract}, nil
}

// bindIAllowList binds a generic wrapper to an already deployed contract.
func bindIAllowList(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := IAllowListMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IAllowList *IAllowListRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IAllowList.Contract.IAllowListCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IAllowList *IAllowListRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IAllowList.Contract.IAllowListTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IAllowList *IAllowListRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IAllowList.Contract.IAllowListTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IAllowList *IAllowListCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IAllowList.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IAllowList *IAllowListTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IAllowList.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IAllowList *IAllowListTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IAllowList.Contract.contract.Transact(opts, method, params...)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IAllowList *IAllowListCaller) ReadAllowList(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _IAllowList.contract.Call(opts, &out, "readAllowList", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IAllowList *IAllowListSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IAllowList.Contract.ReadAllowList(&_IAllowList.CallOpts, addr)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IAllowList *IAllowListCallerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IAllowList.Contract.ReadAllowList(&_IAllowList.CallOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IAllowList *IAllowListTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IAllowList.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IAllowList *IAllowListSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetAdmin(&_IAllowList.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IAllowList *IAllowListTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetAdmin(&_IAllowList.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IAllowList *IAllowListTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IAllowList.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IAllowList *IAllowListSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetEnabled(&_IAllowList.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IAllowList *IAllowListTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetEnabled(&_IAllowList.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IAllowList *IAllowListTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IAllowList.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IAllowList *IAllowListSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetManager(&_IAllowList.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IAllowList *IAllowListTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetManager(&_IAllowList.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IAllowList *IAllowListTransactor) SetNone(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IAllowList.contract.Transact(opts, "setNone", addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IAllowList *IAllowListSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetNone(&_IAllowList.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IAllowList *IAllowListTransactorSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IAllowList.Contract.SetNone(&_IAllowList.TransactOpts, addr)
}

// IAllowListRoleSetIterator is returned from FilterRoleSet and is used to iterate over the raw logs and unpacked data for RoleSet events raised by the IAllowList contract.
type IAllowListRoleSetIterator struct {
	Event *IAllowListRoleSet // Event containing the contract specifics and raw log

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
func (it *IAllowListRoleSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IAllowListRoleSet)
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
		it.Event = new(IAllowListRoleSet)
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
func (it *IAllowListRoleSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IAllowListRoleSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IAllowListRoleSet represents a RoleSet event raised by the IAllowList contract.
type IAllowListRoleSet struct {
	Role    *big.Int
	Account common.Address
	Sender  common.Address
	OldRole *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleSet is a free log retrieval operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IAllowList *IAllowListFilterer) FilterRoleSet(opts *bind.FilterOpts, role []*big.Int, account []common.Address, sender []common.Address) (*IAllowListRoleSetIterator, error) {

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

	logs, sub, err := _IAllowList.contract.FilterLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &IAllowListRoleSetIterator{contract: _IAllowList.contract, event: "RoleSet", logs: logs, sub: sub}, nil
}

// WatchRoleSet is a free log subscription operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IAllowList *IAllowListFilterer) WatchRoleSet(opts *bind.WatchOpts, sink chan<- *IAllowListRoleSet, role []*big.Int, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _IAllowList.contract.WatchLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IAllowListRoleSet)
				if err := _IAllowList.contract.UnpackLog(event, "RoleSet", log); err != nil {
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
func (_IAllowList *IAllowListFilterer) ParseRoleSet(log types.Log) (*IAllowListRoleSet, error) {
	event := new(IAllowListRoleSet)
	if err := _IAllowList.contract.UnpackLog(event, "RoleSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
