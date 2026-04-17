// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bazelsolc

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

// HelloWorldMetaData contains all meta data concerning the HelloWorld contract.
var HelloWorldMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"name\":\"Hello\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"who\",\"type\":\"string\"}],\"name\":\"greet\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"ead710c4": "greet(string)",
	},
	Bin: "0x608060405234801561000f575f80fd5b506102b18061001d5f395ff3fe608060405234801561000f575f80fd5b5060043610610029575f3560e01c8063ead710c41461002d575b5f80fd5b610047600480360381019061004291906101d0565b610049565b005b7fe762c7c6ad44bf64dd9f998228fe1bf5218e470864dcfff5544c541c5b6c649d816040516100789190610291565b60405180910390a150565b5f604051905090565b5f80fd5b5f80fd5b5f80fd5b5f80fd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6100e28261009c565b810181811067ffffffffffffffff82111715610101576101006100ac565b5b80604052505050565b5f610113610083565b905061011f82826100d9565b919050565b5f67ffffffffffffffff82111561013e5761013d6100ac565b5b6101478261009c565b9050602081019050919050565b828183375f83830152505050565b5f61017461016f84610124565b61010a565b9050828152602081018484840111156101905761018f610098565b5b61019b848285610154565b509392505050565b5f82601f8301126101b7576101b6610094565b5b81356101c7848260208601610162565b91505092915050565b5f602082840312156101e5576101e461008c565b5b5f82013567ffffffffffffffff81111561020257610201610090565b5b61020e848285016101a3565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b8381101561024e578082015181840152602081019050610233565b5f8484015250505050565b5f61026382610217565b61026d8185610221565b935061027d818560208601610231565b6102868161009c565b840191505092915050565b5f6020820190508181035f8301526102a98184610259565b90509291505056",
}

// HelloWorldABI is the input ABI used to generate the binding from.
// Deprecated: Use HelloWorldMetaData.ABI instead.
var HelloWorldABI = HelloWorldMetaData.ABI

// Deprecated: Use HelloWorldMetaData.Sigs instead.
// HelloWorldFuncSigs maps the 4-byte function signature to its string representation.
var HelloWorldFuncSigs = HelloWorldMetaData.Sigs

// HelloWorldBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use HelloWorldMetaData.Bin instead.
var HelloWorldBin = HelloWorldMetaData.Bin

// DeployHelloWorld deploys a new Ethereum contract, binding an instance of HelloWorld to it.
func DeployHelloWorld(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *HelloWorld, error) {
	parsed, err := HelloWorldMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(HelloWorldBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &HelloWorld{HelloWorldCaller: HelloWorldCaller{contract: contract}, HelloWorldTransactor: HelloWorldTransactor{contract: contract}, HelloWorldFilterer: HelloWorldFilterer{contract: contract}}, nil
}

// HelloWorld is an auto generated Go binding around an Ethereum contract.
type HelloWorld struct {
	HelloWorldCaller     // Read-only binding to the contract
	HelloWorldTransactor // Write-only binding to the contract
	HelloWorldFilterer   // Log filterer for contract events
}

// HelloWorldCaller is an auto generated read-only Go binding around an Ethereum contract.
type HelloWorldCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// HelloWorldTransactor is an auto generated write-only Go binding around an Ethereum contract.
type HelloWorldTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// HelloWorldFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type HelloWorldFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// HelloWorldSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type HelloWorldSession struct {
	Contract     *HelloWorld       // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// HelloWorldCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type HelloWorldCallerSession struct {
	Contract *HelloWorldCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts     // Call options to use throughout this session
}

// HelloWorldTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type HelloWorldTransactorSession struct {
	Contract     *HelloWorldTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// HelloWorldRaw is an auto generated low-level Go binding around an Ethereum contract.
type HelloWorldRaw struct {
	Contract *HelloWorld // Generic contract binding to access the raw methods on
}

// HelloWorldCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type HelloWorldCallerRaw struct {
	Contract *HelloWorldCaller // Generic read-only contract binding to access the raw methods on
}

// HelloWorldTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type HelloWorldTransactorRaw struct {
	Contract *HelloWorldTransactor // Generic write-only contract binding to access the raw methods on
}

// NewHelloWorld creates a new instance of HelloWorld, bound to a specific deployed contract.
func NewHelloWorld(address common.Address, backend bind.ContractBackend) (*HelloWorld, error) {
	contract, err := bindHelloWorld(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &HelloWorld{HelloWorldCaller: HelloWorldCaller{contract: contract}, HelloWorldTransactor: HelloWorldTransactor{contract: contract}, HelloWorldFilterer: HelloWorldFilterer{contract: contract}}, nil
}

// NewHelloWorldCaller creates a new read-only instance of HelloWorld, bound to a specific deployed contract.
func NewHelloWorldCaller(address common.Address, caller bind.ContractCaller) (*HelloWorldCaller, error) {
	contract, err := bindHelloWorld(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &HelloWorldCaller{contract: contract}, nil
}

// NewHelloWorldTransactor creates a new write-only instance of HelloWorld, bound to a specific deployed contract.
func NewHelloWorldTransactor(address common.Address, transactor bind.ContractTransactor) (*HelloWorldTransactor, error) {
	contract, err := bindHelloWorld(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &HelloWorldTransactor{contract: contract}, nil
}

// NewHelloWorldFilterer creates a new log filterer instance of HelloWorld, bound to a specific deployed contract.
func NewHelloWorldFilterer(address common.Address, filterer bind.ContractFilterer) (*HelloWorldFilterer, error) {
	contract, err := bindHelloWorld(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &HelloWorldFilterer{contract: contract}, nil
}

// bindHelloWorld binds a generic wrapper to an already deployed contract.
func bindHelloWorld(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := HelloWorldMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_HelloWorld *HelloWorldRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _HelloWorld.Contract.HelloWorldCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_HelloWorld *HelloWorldRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _HelloWorld.Contract.HelloWorldTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_HelloWorld *HelloWorldRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _HelloWorld.Contract.HelloWorldTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_HelloWorld *HelloWorldCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _HelloWorld.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_HelloWorld *HelloWorldTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _HelloWorld.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_HelloWorld *HelloWorldTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _HelloWorld.Contract.contract.Transact(opts, method, params...)
}

// Greet is a paid mutator transaction binding the contract method 0xead710c4.
//
// Solidity: function greet(string who) returns()
func (_HelloWorld *HelloWorldTransactor) Greet(opts *bind.TransactOpts, who string) (*types.Transaction, error) {
	return _HelloWorld.contract.Transact(opts, "greet", who)
}

// Greet is a paid mutator transaction binding the contract method 0xead710c4.
//
// Solidity: function greet(string who) returns()
func (_HelloWorld *HelloWorldSession) Greet(who string) (*types.Transaction, error) {
	return _HelloWorld.Contract.Greet(&_HelloWorld.TransactOpts, who)
}

// Greet is a paid mutator transaction binding the contract method 0xead710c4.
//
// Solidity: function greet(string who) returns()
func (_HelloWorld *HelloWorldTransactorSession) Greet(who string) (*types.Transaction, error) {
	return _HelloWorld.Contract.Greet(&_HelloWorld.TransactOpts, who)
}

// HelloWorldHelloIterator is returned from FilterHello and is used to iterate over the raw logs and unpacked data for Hello events raised by the HelloWorld contract.
type HelloWorldHelloIterator struct {
	Event *HelloWorldHello // Event containing the contract specifics and raw log

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
func (it *HelloWorldHelloIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(HelloWorldHello)
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
		it.Event = new(HelloWorldHello)
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
func (it *HelloWorldHelloIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *HelloWorldHelloIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// HelloWorldHello represents a Hello event raised by the HelloWorld contract.
type HelloWorldHello struct {
	Arg0 string
	Raw  types.Log // Blockchain specific contextual infos
}

// FilterHello is a free log retrieval operation binding the contract event 0xe762c7c6ad44bf64dd9f998228fe1bf5218e470864dcfff5544c541c5b6c649d.
//
// Solidity: event Hello(string arg0)
func (_HelloWorld *HelloWorldFilterer) FilterHello(opts *bind.FilterOpts) (*HelloWorldHelloIterator, error) {

	logs, sub, err := _HelloWorld.contract.FilterLogs(opts, "Hello")
	if err != nil {
		return nil, err
	}
	return &HelloWorldHelloIterator{contract: _HelloWorld.contract, event: "Hello", logs: logs, sub: sub}, nil
}

// WatchHello is a free log subscription operation binding the contract event 0xe762c7c6ad44bf64dd9f998228fe1bf5218e470864dcfff5544c541c5b6c649d.
//
// Solidity: event Hello(string arg0)
func (_HelloWorld *HelloWorldFilterer) WatchHello(opts *bind.WatchOpts, sink chan<- *HelloWorldHello) (event.Subscription, error) {

	logs, sub, err := _HelloWorld.contract.WatchLogs(opts, "Hello")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(HelloWorldHello)
				if err := _HelloWorld.contract.UnpackLog(event, "Hello", log); err != nil {
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

// ParseHello is a log parse operation binding the contract event 0xe762c7c6ad44bf64dd9f998228fe1bf5218e470864dcfff5544c541c5b6c649d.
//
// Solidity: event Hello(string arg0)
func (_HelloWorld *HelloWorldFilterer) ParseHello(log types.Log) (*HelloWorldHello, error) {
	event := new(HelloWorldHello)
	if err := _HelloWorld.contract.UnpackLog(event, "Hello", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
