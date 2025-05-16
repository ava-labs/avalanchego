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

// DummyMetaData contains all meta data concerning the Dummy contract.
var DummyMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"key\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"DataWritten\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newValue\",\"type\":\"uint256\"}],\"name\":\"ValueUpdated\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"data\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"key\",\"type\":\"uint256\"}],\"name\":\"readData\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newValue\",\"type\":\"uint256\"}],\"name\":\"updateValue\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"value\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"key\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"val\",\"type\":\"uint256\"}],\"name\":\"writeData\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x6080604052348015600e575f5ffd5b50602a5f819055503360015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555061036a806100635f395ff3fe608060405234801561000f575f5ffd5b5060043610610060575f3560e01c806337ebbc03146100645780633fa4f24514610094578063573c0bd3146100b25780638da5cb5b146100ce578063c71ba63b146100ec578063f0ba844014610108575b5f5ffd5b61007e60048036038101906100799190610224565b610138565b60405161008b919061025e565b60405180910390f35b61009c610152565b6040516100a9919061025e565b60405180910390f35b6100cc60048036038101906100c79190610224565b610157565b005b6100d6610160565b6040516100e391906102b6565b60405180910390f35b610106600480360381019061010191906102cf565b610185565b005b610122600480360381019061011d9190610224565b6101d8565b60405161012f919061025e565b60405180910390f35b5f60025f8381526020019081526020015f20549050919050565b5f5481565b805f8190555050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b8060025f8481526020019081526020015f20819055507f36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c28182826040516101cc92919061030d565b60405180910390a15050565b6002602052805f5260405f205f915090505481565b5f5ffd5b5f819050919050565b610203816101f1565b811461020d575f5ffd5b50565b5f8135905061021e816101fa565b92915050565b5f60208284031215610239576102386101ed565b5b5f61024684828501610210565b91505092915050565b610258816101f1565b82525050565b5f6020820190506102715f83018461024f565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102a082610277565b9050919050565b6102b081610296565b82525050565b5f6020820190506102c95f8301846102a7565b92915050565b5f5f604083850312156102e5576102e46101ed565b5b5f6102f285828601610210565b925050602061030385828601610210565b9150509250929050565b5f6040820190506103205f83018561024f565b61032d602083018461024f565b939250505056fea26469706673582212201893623673c7d322a3501952be3510a3b2582a332e23a9ee7765420a7e2c8f7464736f6c634300081d0033",
}

// DummyABI is the input ABI used to generate the binding from.
// Deprecated: Use DummyMetaData.ABI instead.
var DummyABI = DummyMetaData.ABI

// DummyBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use DummyMetaData.Bin instead.
var DummyBin = DummyMetaData.Bin

// DeployDummy deploys a new Ethereum contract, binding an instance of Dummy to it.
func DeployDummy(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *Dummy, error) {
	parsed, err := DummyMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(DummyBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &Dummy{DummyCaller: DummyCaller{contract: contract}, DummyTransactor: DummyTransactor{contract: contract}, DummyFilterer: DummyFilterer{contract: contract}}, nil
}

// Dummy is an auto generated Go binding around an Ethereum contract.
type Dummy struct {
	DummyCaller     // Read-only binding to the contract
	DummyTransactor // Write-only binding to the contract
	DummyFilterer   // Log filterer for contract events
}

// DummyCaller is an auto generated read-only Go binding around an Ethereum contract.
type DummyCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DummyTransactor is an auto generated write-only Go binding around an Ethereum contract.
type DummyTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DummyFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type DummyFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DummySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type DummySession struct {
	Contract     *Dummy            // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// DummyCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type DummyCallerSession struct {
	Contract *DummyCaller  // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts // Call options to use throughout this session
}

// DummyTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type DummyTransactorSession struct {
	Contract     *DummyTransactor  // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// DummyRaw is an auto generated low-level Go binding around an Ethereum contract.
type DummyRaw struct {
	Contract *Dummy // Generic contract binding to access the raw methods on
}

// DummyCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type DummyCallerRaw struct {
	Contract *DummyCaller // Generic read-only contract binding to access the raw methods on
}

// DummyTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type DummyTransactorRaw struct {
	Contract *DummyTransactor // Generic write-only contract binding to access the raw methods on
}

// NewDummy creates a new instance of Dummy, bound to a specific deployed contract.
func NewDummy(address common.Address, backend bind.ContractBackend) (*Dummy, error) {
	contract, err := bindDummy(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Dummy{DummyCaller: DummyCaller{contract: contract}, DummyTransactor: DummyTransactor{contract: contract}, DummyFilterer: DummyFilterer{contract: contract}}, nil
}

// NewDummyCaller creates a new read-only instance of Dummy, bound to a specific deployed contract.
func NewDummyCaller(address common.Address, caller bind.ContractCaller) (*DummyCaller, error) {
	contract, err := bindDummy(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &DummyCaller{contract: contract}, nil
}

// NewDummyTransactor creates a new write-only instance of Dummy, bound to a specific deployed contract.
func NewDummyTransactor(address common.Address, transactor bind.ContractTransactor) (*DummyTransactor, error) {
	contract, err := bindDummy(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &DummyTransactor{contract: contract}, nil
}

// NewDummyFilterer creates a new log filterer instance of Dummy, bound to a specific deployed contract.
func NewDummyFilterer(address common.Address, filterer bind.ContractFilterer) (*DummyFilterer, error) {
	contract, err := bindDummy(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &DummyFilterer{contract: contract}, nil
}

// bindDummy binds a generic wrapper to an already deployed contract.
func bindDummy(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := DummyMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Dummy *DummyRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Dummy.Contract.DummyCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Dummy *DummyRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Dummy.Contract.DummyTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Dummy *DummyRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Dummy.Contract.DummyTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Dummy *DummyCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Dummy.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Dummy *DummyTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Dummy.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Dummy *DummyTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Dummy.Contract.contract.Transact(opts, method, params...)
}

// Data is a free data retrieval call binding the contract method 0xf0ba8440.
//
// Solidity: function data(uint256 ) view returns(uint256)
func (_Dummy *DummyCaller) Data(opts *bind.CallOpts, arg0 *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _Dummy.contract.Call(opts, &out, "data", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Data is a free data retrieval call binding the contract method 0xf0ba8440.
//
// Solidity: function data(uint256 ) view returns(uint256)
func (_Dummy *DummySession) Data(arg0 *big.Int) (*big.Int, error) {
	return _Dummy.Contract.Data(&_Dummy.CallOpts, arg0)
}

// Data is a free data retrieval call binding the contract method 0xf0ba8440.
//
// Solidity: function data(uint256 ) view returns(uint256)
func (_Dummy *DummyCallerSession) Data(arg0 *big.Int) (*big.Int, error) {
	return _Dummy.Contract.Data(&_Dummy.CallOpts, arg0)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Dummy *DummyCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Dummy.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Dummy *DummySession) Owner() (common.Address, error) {
	return _Dummy.Contract.Owner(&_Dummy.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Dummy *DummyCallerSession) Owner() (common.Address, error) {
	return _Dummy.Contract.Owner(&_Dummy.CallOpts)
}

// ReadData is a free data retrieval call binding the contract method 0x37ebbc03.
//
// Solidity: function readData(uint256 key) view returns(uint256)
func (_Dummy *DummyCaller) ReadData(opts *bind.CallOpts, key *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _Dummy.contract.Call(opts, &out, "readData", key)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReadData is a free data retrieval call binding the contract method 0x37ebbc03.
//
// Solidity: function readData(uint256 key) view returns(uint256)
func (_Dummy *DummySession) ReadData(key *big.Int) (*big.Int, error) {
	return _Dummy.Contract.ReadData(&_Dummy.CallOpts, key)
}

// ReadData is a free data retrieval call binding the contract method 0x37ebbc03.
//
// Solidity: function readData(uint256 key) view returns(uint256)
func (_Dummy *DummyCallerSession) ReadData(key *big.Int) (*big.Int, error) {
	return _Dummy.Contract.ReadData(&_Dummy.CallOpts, key)
}

// Value is a free data retrieval call binding the contract method 0x3fa4f245.
//
// Solidity: function value() view returns(uint256)
func (_Dummy *DummyCaller) Value(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Dummy.contract.Call(opts, &out, "value")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Value is a free data retrieval call binding the contract method 0x3fa4f245.
//
// Solidity: function value() view returns(uint256)
func (_Dummy *DummySession) Value() (*big.Int, error) {
	return _Dummy.Contract.Value(&_Dummy.CallOpts)
}

// Value is a free data retrieval call binding the contract method 0x3fa4f245.
//
// Solidity: function value() view returns(uint256)
func (_Dummy *DummyCallerSession) Value() (*big.Int, error) {
	return _Dummy.Contract.Value(&_Dummy.CallOpts)
}

// UpdateValue is a paid mutator transaction binding the contract method 0x573c0bd3.
//
// Solidity: function updateValue(uint256 newValue) returns()
func (_Dummy *DummyTransactor) UpdateValue(opts *bind.TransactOpts, newValue *big.Int) (*types.Transaction, error) {
	return _Dummy.contract.Transact(opts, "updateValue", newValue)
}

// UpdateValue is a paid mutator transaction binding the contract method 0x573c0bd3.
//
// Solidity: function updateValue(uint256 newValue) returns()
func (_Dummy *DummySession) UpdateValue(newValue *big.Int) (*types.Transaction, error) {
	return _Dummy.Contract.UpdateValue(&_Dummy.TransactOpts, newValue)
}

// UpdateValue is a paid mutator transaction binding the contract method 0x573c0bd3.
//
// Solidity: function updateValue(uint256 newValue) returns()
func (_Dummy *DummyTransactorSession) UpdateValue(newValue *big.Int) (*types.Transaction, error) {
	return _Dummy.Contract.UpdateValue(&_Dummy.TransactOpts, newValue)
}

// WriteData is a paid mutator transaction binding the contract method 0xc71ba63b.
//
// Solidity: function writeData(uint256 key, uint256 val) returns()
func (_Dummy *DummyTransactor) WriteData(opts *bind.TransactOpts, key *big.Int, val *big.Int) (*types.Transaction, error) {
	return _Dummy.contract.Transact(opts, "writeData", key, val)
}

// WriteData is a paid mutator transaction binding the contract method 0xc71ba63b.
//
// Solidity: function writeData(uint256 key, uint256 val) returns()
func (_Dummy *DummySession) WriteData(key *big.Int, val *big.Int) (*types.Transaction, error) {
	return _Dummy.Contract.WriteData(&_Dummy.TransactOpts, key, val)
}

// WriteData is a paid mutator transaction binding the contract method 0xc71ba63b.
//
// Solidity: function writeData(uint256 key, uint256 val) returns()
func (_Dummy *DummyTransactorSession) WriteData(key *big.Int, val *big.Int) (*types.Transaction, error) {
	return _Dummy.Contract.WriteData(&_Dummy.TransactOpts, key, val)
}

// DummyDataWrittenIterator is returned from FilterDataWritten and is used to iterate over the raw logs and unpacked data for DataWritten events raised by the Dummy contract.
type DummyDataWrittenIterator struct {
	Event *DummyDataWritten // Event containing the contract specifics and raw log

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
func (it *DummyDataWrittenIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(DummyDataWritten)
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
		it.Event = new(DummyDataWritten)
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
func (it *DummyDataWrittenIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *DummyDataWrittenIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// DummyDataWritten represents a DataWritten event raised by the Dummy contract.
type DummyDataWritten struct {
	Key   *big.Int
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterDataWritten is a free log retrieval operation binding the contract event 0x36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c281.
//
// Solidity: event DataWritten(uint256 key, uint256 value)
func (_Dummy *DummyFilterer) FilterDataWritten(opts *bind.FilterOpts) (*DummyDataWrittenIterator, error) {

	logs, sub, err := _Dummy.contract.FilterLogs(opts, "DataWritten")
	if err != nil {
		return nil, err
	}
	return &DummyDataWrittenIterator{contract: _Dummy.contract, event: "DataWritten", logs: logs, sub: sub}, nil
}

// WatchDataWritten is a free log subscription operation binding the contract event 0x36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c281.
//
// Solidity: event DataWritten(uint256 key, uint256 value)
func (_Dummy *DummyFilterer) WatchDataWritten(opts *bind.WatchOpts, sink chan<- *DummyDataWritten) (event.Subscription, error) {

	logs, sub, err := _Dummy.contract.WatchLogs(opts, "DataWritten")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(DummyDataWritten)
				if err := _Dummy.contract.UnpackLog(event, "DataWritten", log); err != nil {
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

// ParseDataWritten is a log parse operation binding the contract event 0x36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c281.
//
// Solidity: event DataWritten(uint256 key, uint256 value)
func (_Dummy *DummyFilterer) ParseDataWritten(log types.Log) (*DummyDataWritten, error) {
	event := new(DummyDataWritten)
	if err := _Dummy.contract.UnpackLog(event, "DataWritten", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// DummyValueUpdatedIterator is returned from FilterValueUpdated and is used to iterate over the raw logs and unpacked data for ValueUpdated events raised by the Dummy contract.
type DummyValueUpdatedIterator struct {
	Event *DummyValueUpdated // Event containing the contract specifics and raw log

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
func (it *DummyValueUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(DummyValueUpdated)
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
		it.Event = new(DummyValueUpdated)
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
func (it *DummyValueUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *DummyValueUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// DummyValueUpdated represents a ValueUpdated event raised by the Dummy contract.
type DummyValueUpdated struct {
	NewValue *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterValueUpdated is a free log retrieval operation binding the contract event 0x4273d0736f60e0dedfe745e86718093d8ec8646ebd2a60cd60643eeced565811.
//
// Solidity: event ValueUpdated(uint256 newValue)
func (_Dummy *DummyFilterer) FilterValueUpdated(opts *bind.FilterOpts) (*DummyValueUpdatedIterator, error) {

	logs, sub, err := _Dummy.contract.FilterLogs(opts, "ValueUpdated")
	if err != nil {
		return nil, err
	}
	return &DummyValueUpdatedIterator{contract: _Dummy.contract, event: "ValueUpdated", logs: logs, sub: sub}, nil
}

// WatchValueUpdated is a free log subscription operation binding the contract event 0x4273d0736f60e0dedfe745e86718093d8ec8646ebd2a60cd60643eeced565811.
//
// Solidity: event ValueUpdated(uint256 newValue)
func (_Dummy *DummyFilterer) WatchValueUpdated(opts *bind.WatchOpts, sink chan<- *DummyValueUpdated) (event.Subscription, error) {

	logs, sub, err := _Dummy.contract.WatchLogs(opts, "ValueUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(DummyValueUpdated)
				if err := _Dummy.contract.UnpackLog(event, "ValueUpdated", log); err != nil {
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

// ParseValueUpdated is a log parse operation binding the contract event 0x4273d0736f60e0dedfe745e86718093d8ec8646ebd2a60cd60643eeced565811.
//
// Solidity: event ValueUpdated(uint256 newValue)
func (_Dummy *DummyFilterer) ParseValueUpdated(log types.Log) (*DummyValueUpdated, error) {
	event := new(DummyValueUpdated)
	if err := _Dummy.contract.UnpackLog(event, "ValueUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
