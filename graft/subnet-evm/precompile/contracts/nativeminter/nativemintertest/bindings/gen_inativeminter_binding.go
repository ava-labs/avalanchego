// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

import (
	"errors"
	"math/big"
	"strings"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts/abi"
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

// INativeMinterMetaData contains all meta data concerning the INativeMinter contract.
var INativeMinterMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"NativeCoinMinted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldRole\",\"type\":\"uint256\"}],\"name\":\"RoleSet\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"mintNativeCoin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"readAllowList\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setNone\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// INativeMinterABI is the input ABI used to generate the binding from.
// Deprecated: Use INativeMinterMetaData.ABI instead.
var INativeMinterABI = INativeMinterMetaData.ABI

// INativeMinter is an auto generated Go binding around an Ethereum contract.
type INativeMinter struct {
	INativeMinterCaller     // Read-only binding to the contract
	INativeMinterTransactor // Write-only binding to the contract
	INativeMinterFilterer   // Log filterer for contract events
}

// INativeMinterCaller is an auto generated read-only Go binding around an Ethereum contract.
type INativeMinterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// INativeMinterTransactor is an auto generated write-only Go binding around an Ethereum contract.
type INativeMinterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// INativeMinterFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type INativeMinterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// INativeMinterSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type INativeMinterSession struct {
	Contract     *INativeMinter    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// INativeMinterCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type INativeMinterCallerSession struct {
	Contract *INativeMinterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// INativeMinterTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type INativeMinterTransactorSession struct {
	Contract     *INativeMinterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// INativeMinterRaw is an auto generated low-level Go binding around an Ethereum contract.
type INativeMinterRaw struct {
	Contract *INativeMinter // Generic contract binding to access the raw methods on
}

// INativeMinterCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type INativeMinterCallerRaw struct {
	Contract *INativeMinterCaller // Generic read-only contract binding to access the raw methods on
}

// INativeMinterTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type INativeMinterTransactorRaw struct {
	Contract *INativeMinterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewINativeMinter creates a new instance of INativeMinter, bound to a specific deployed contract.
func NewINativeMinter(address common.Address, backend bind.ContractBackend) (*INativeMinter, error) {
	contract, err := bindINativeMinter(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &INativeMinter{INativeMinterCaller: INativeMinterCaller{contract: contract}, INativeMinterTransactor: INativeMinterTransactor{contract: contract}, INativeMinterFilterer: INativeMinterFilterer{contract: contract}}, nil
}

// NewINativeMinterCaller creates a new read-only instance of INativeMinter, bound to a specific deployed contract.
func NewINativeMinterCaller(address common.Address, caller bind.ContractCaller) (*INativeMinterCaller, error) {
	contract, err := bindINativeMinter(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &INativeMinterCaller{contract: contract}, nil
}

// NewINativeMinterTransactor creates a new write-only instance of INativeMinter, bound to a specific deployed contract.
func NewINativeMinterTransactor(address common.Address, transactor bind.ContractTransactor) (*INativeMinterTransactor, error) {
	contract, err := bindINativeMinter(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &INativeMinterTransactor{contract: contract}, nil
}

// NewINativeMinterFilterer creates a new log filterer instance of INativeMinter, bound to a specific deployed contract.
func NewINativeMinterFilterer(address common.Address, filterer bind.ContractFilterer) (*INativeMinterFilterer, error) {
	contract, err := bindINativeMinter(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &INativeMinterFilterer{contract: contract}, nil
}

// bindINativeMinter binds a generic wrapper to an already deployed contract.
func bindINativeMinter(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := INativeMinterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_INativeMinter *INativeMinterRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _INativeMinter.Contract.INativeMinterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_INativeMinter *INativeMinterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _INativeMinter.Contract.INativeMinterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_INativeMinter *INativeMinterRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _INativeMinter.Contract.INativeMinterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_INativeMinter *INativeMinterCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _INativeMinter.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_INativeMinter *INativeMinterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _INativeMinter.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_INativeMinter *INativeMinterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _INativeMinter.Contract.contract.Transact(opts, method, params...)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_INativeMinter *INativeMinterCaller) ReadAllowList(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _INativeMinter.contract.Call(opts, &out, "readAllowList", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_INativeMinter *INativeMinterSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _INativeMinter.Contract.ReadAllowList(&_INativeMinter.CallOpts, addr)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_INativeMinter *INativeMinterCallerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _INativeMinter.Contract.ReadAllowList(&_INativeMinter.CallOpts, addr)
}

// MintNativeCoin is a paid mutator transaction binding the contract method 0x4f5aaaba.
//
// Solidity: function mintNativeCoin(address addr, uint256 amount) returns()
func (_INativeMinter *INativeMinterTransactor) MintNativeCoin(opts *bind.TransactOpts, addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return _INativeMinter.contract.Transact(opts, "mintNativeCoin", addr, amount)
}

// MintNativeCoin is a paid mutator transaction binding the contract method 0x4f5aaaba.
//
// Solidity: function mintNativeCoin(address addr, uint256 amount) returns()
func (_INativeMinter *INativeMinterSession) MintNativeCoin(addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return _INativeMinter.Contract.MintNativeCoin(&_INativeMinter.TransactOpts, addr, amount)
}

// MintNativeCoin is a paid mutator transaction binding the contract method 0x4f5aaaba.
//
// Solidity: function mintNativeCoin(address addr, uint256 amount) returns()
func (_INativeMinter *INativeMinterTransactorSession) MintNativeCoin(addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return _INativeMinter.Contract.MintNativeCoin(&_INativeMinter.TransactOpts, addr, amount)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_INativeMinter *INativeMinterTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_INativeMinter *INativeMinterSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetAdmin(&_INativeMinter.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_INativeMinter *INativeMinterTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetAdmin(&_INativeMinter.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_INativeMinter *INativeMinterTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_INativeMinter *INativeMinterSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetEnabled(&_INativeMinter.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_INativeMinter *INativeMinterTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetEnabled(&_INativeMinter.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_INativeMinter *INativeMinterTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_INativeMinter *INativeMinterSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetManager(&_INativeMinter.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_INativeMinter *INativeMinterTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetManager(&_INativeMinter.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_INativeMinter *INativeMinterTransactor) SetNone(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.contract.Transact(opts, "setNone", addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_INativeMinter *INativeMinterSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetNone(&_INativeMinter.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_INativeMinter *INativeMinterTransactorSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _INativeMinter.Contract.SetNone(&_INativeMinter.TransactOpts, addr)
}

// INativeMinterNativeCoinMintedIterator is returned from FilterNativeCoinMinted and is used to iterate over the raw logs and unpacked data for NativeCoinMinted events raised by the INativeMinter contract.
type INativeMinterNativeCoinMintedIterator struct {
	Event *INativeMinterNativeCoinMinted // Event containing the contract specifics and raw log

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
func (it *INativeMinterNativeCoinMintedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(INativeMinterNativeCoinMinted)
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
		it.Event = new(INativeMinterNativeCoinMinted)
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
func (it *INativeMinterNativeCoinMintedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *INativeMinterNativeCoinMintedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// INativeMinterNativeCoinMinted represents a NativeCoinMinted event raised by the INativeMinter contract.
type INativeMinterNativeCoinMinted struct {
	Sender    common.Address
	Recipient common.Address
	Amount    *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterNativeCoinMinted is a free log retrieval operation binding the contract event 0x400cd392f3d56fd10bb1dbd5839fdda8298208ddaa97b368faa053e1850930ee.
//
// Solidity: event NativeCoinMinted(address indexed sender, address indexed recipient, uint256 amount)
func (_INativeMinter *INativeMinterFilterer) FilterNativeCoinMinted(opts *bind.FilterOpts, sender []common.Address, recipient []common.Address) (*INativeMinterNativeCoinMintedIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var recipientRule []interface{}
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}

	logs, sub, err := _INativeMinter.contract.FilterLogs(opts, "NativeCoinMinted", senderRule, recipientRule)
	if err != nil {
		return nil, err
	}
	return &INativeMinterNativeCoinMintedIterator{contract: _INativeMinter.contract, event: "NativeCoinMinted", logs: logs, sub: sub}, nil
}

// WatchNativeCoinMinted is a free log subscription operation binding the contract event 0x400cd392f3d56fd10bb1dbd5839fdda8298208ddaa97b368faa053e1850930ee.
//
// Solidity: event NativeCoinMinted(address indexed sender, address indexed recipient, uint256 amount)
func (_INativeMinter *INativeMinterFilterer) WatchNativeCoinMinted(opts *bind.WatchOpts, sink chan<- *INativeMinterNativeCoinMinted, sender []common.Address, recipient []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var recipientRule []interface{}
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}

	logs, sub, err := _INativeMinter.contract.WatchLogs(opts, "NativeCoinMinted", senderRule, recipientRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(INativeMinterNativeCoinMinted)
				if err := _INativeMinter.contract.UnpackLog(event, "NativeCoinMinted", log); err != nil {
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

// ParseNativeCoinMinted is a log parse operation binding the contract event 0x400cd392f3d56fd10bb1dbd5839fdda8298208ddaa97b368faa053e1850930ee.
//
// Solidity: event NativeCoinMinted(address indexed sender, address indexed recipient, uint256 amount)
func (_INativeMinter *INativeMinterFilterer) ParseNativeCoinMinted(log types.Log) (*INativeMinterNativeCoinMinted, error) {
	event := new(INativeMinterNativeCoinMinted)
	if err := _INativeMinter.contract.UnpackLog(event, "NativeCoinMinted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// INativeMinterRoleSetIterator is returned from FilterRoleSet and is used to iterate over the raw logs and unpacked data for RoleSet events raised by the INativeMinter contract.
type INativeMinterRoleSetIterator struct {
	Event *INativeMinterRoleSet // Event containing the contract specifics and raw log

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
func (it *INativeMinterRoleSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(INativeMinterRoleSet)
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
		it.Event = new(INativeMinterRoleSet)
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
func (it *INativeMinterRoleSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *INativeMinterRoleSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// INativeMinterRoleSet represents a RoleSet event raised by the INativeMinter contract.
type INativeMinterRoleSet struct {
	Role    *big.Int
	Account common.Address
	Sender  common.Address
	OldRole *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleSet is a free log retrieval operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_INativeMinter *INativeMinterFilterer) FilterRoleSet(opts *bind.FilterOpts, role []*big.Int, account []common.Address, sender []common.Address) (*INativeMinterRoleSetIterator, error) {

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

	logs, sub, err := _INativeMinter.contract.FilterLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &INativeMinterRoleSetIterator{contract: _INativeMinter.contract, event: "RoleSet", logs: logs, sub: sub}, nil
}

// WatchRoleSet is a free log subscription operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_INativeMinter *INativeMinterFilterer) WatchRoleSet(opts *bind.WatchOpts, sink chan<- *INativeMinterRoleSet, role []*big.Int, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _INativeMinter.contract.WatchLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(INativeMinterRoleSet)
				if err := _INativeMinter.contract.UnpackLog(event, "RoleSet", log); err != nil {
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
func (_INativeMinter *INativeMinterFilterer) ParseRoleSet(log types.Log) (*INativeMinterRoleSet, error) {
	event := new(INativeMinterRoleSet)
	if err := _INativeMinter.contract.UnpackLog(event, "RoleSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
