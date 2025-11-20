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

// AllowListMetaData contains all meta data concerning the AllowList contract.
var AllowListMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"precompileAddr\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"OwnableInvalidOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"OwnableUnauthorizedAccount\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isAdmin\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isEnabled\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isManager\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"revoke\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b50604051610d39380380610d3983398181016040528101906100319190610217565b335f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff16036100a2575f6040517f1e4fbdf70000000000000000000000000000000000000000000000000000000081526004016100999190610251565b60405180910390fd5b6100b1816100f860201b60201c565b508060015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055505061026a565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6101e6826101bd565b9050919050565b6101f6816101dc565b8114610200575f5ffd5b50565b5f81519050610211816101ed565b92915050565b5f6020828403121561022c5761022b6101b9565b5b5f61023984828501610203565b91505092915050565b61024b816101dc565b82525050565b5f6020820190506102645f830184610242565b92915050565b610ac2806102775f395ff3fe608060405234801561000f575f5ffd5b506004361061009c575f3560e01c80638da5cb5b116100645780638da5cb5b1461012e5780639015d3711461014c578063d0ebdbe71461017c578063f2fde38b14610198578063f3ae2415146101b45761009c565b80630aaf7043146100a057806324d7806c146100bc578063704b6c02146100ec578063715018a61461010857806374a8f10314610112575b5f5ffd5b6100ba60048036038101906100b59190610930565b6101e4565b005b6100d660048036038101906100d19190610930565b6101f8565b6040516100e39190610975565b60405180910390f35b61010660048036038101906101019190610930565b6102a1565b005b6101106102b5565b005b61012c60048036038101906101279190610930565b6102c8565b005b6101366102dc565b604051610143919061099d565b60405180910390f35b61016660048036038101906101619190610930565b610303565b6040516101739190610975565b60405180910390f35b61019660048036038101906101919190610930565b6103ac565b005b6101b260048036038101906101ad9190610930565b6103c0565b005b6101ce60048036038101906101c99190610930565b610444565b6040516101db9190610975565b60405180910390f35b6101ec6104ed565b6101f581610574565b50565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b8152600401610254919061099d565b602060405180830381865afa15801561026f573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061029391906109e9565b905060028114915050919050565b6102a96104ed565b6102b2816105fe565b50565b6102bd6104ed565b6102c65f610688565b565b6102d06104ed565b6102d981610749565b50565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b815260040161035f919061099d565b602060405180830381865afa15801561037a573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061039e91906109e9565b90505f811415915050919050565b6103b46104ed565b6103bd81610841565b50565b6103c86104ed565b5f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1603610438575f6040517f1e4fbdf700000000000000000000000000000000000000000000000000000000815260040161042f919061099d565b60405180910390fd5b61044181610688565b50565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016104a0919061099d565b602060405180830381865afa1580156104bb573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906104df91906109e9565b905060038114915050919050565b6104f56108cb565b73ffffffffffffffffffffffffffffffffffffffff166105136102dc565b73ffffffffffffffffffffffffffffffffffffffff1614610572576105366108cb565b6040517f118cdaa7000000000000000000000000000000000000000000000000000000008152600401610569919061099d565b60405180910390fd5b565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630aaf7043826040518263ffffffff1660e01b81526004016105ce919061099d565b5f604051808303815f87803b1580156105e5575f5ffd5b505af11580156105f7573d5f5f3e3d5ffd5b5050505050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663704b6c02826040518263ffffffff1660e01b8152600401610658919061099d565b5f604051808303815f87803b15801561066f575f5ffd5b505af1158015610681573d5f5f3e3d5ffd5b5050505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b8073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff16036107b7576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107ae90610a6e565b60405180910390fd5b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638c6bfb3b826040518263ffffffff1660e01b8152600401610811919061099d565b5f604051808303815f87803b158015610828575f5ffd5b505af115801561083a573d5f5f3e3d5ffd5b5050505050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663d0ebdbe7826040518263ffffffff1660e01b815260040161089b919061099d565b5f604051808303815f87803b1580156108b2575f5ffd5b505af11580156108c4573d5f5f3e3d5ffd5b5050505050565b5f33905090565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6108ff826108d6565b9050919050565b61090f816108f5565b8114610919575f5ffd5b50565b5f8135905061092a81610906565b92915050565b5f60208284031215610945576109446108d2565b5b5f6109528482850161091c565b91505092915050565b5f8115159050919050565b61096f8161095b565b82525050565b5f6020820190506109885f830184610966565b92915050565b610997816108f5565b82525050565b5f6020820190506109b05f83018461098e565b92915050565b5f819050919050565b6109c8816109b6565b81146109d2575f5ffd5b50565b5f815190506109e3816109bf565b92915050565b5f602082840312156109fe576109fd6108d2565b5b5f610a0b848285016109d5565b91505092915050565b5f82825260208201905092915050565b7f63616e6e6f74207265766f6b65206f776e20726f6c65000000000000000000005f82015250565b5f610a58601683610a14565b9150610a6382610a24565b602082019050919050565b5f6020820190508181035f830152610a8581610a4c565b905091905056fea2646970667358221220ca1811ed5ea5c612a035f49f4669d3792a4e99edce0d414764416eada03edce764736f6c634300081e0033",
}

// AllowListABI is the input ABI used to generate the binding from.
// Deprecated: Use AllowListMetaData.ABI instead.
var AllowListABI = AllowListMetaData.ABI

// AllowListBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use AllowListMetaData.Bin instead.
var AllowListBin = AllowListMetaData.Bin

// DeployAllowList deploys a new Ethereum contract, binding an instance of AllowList to it.
func DeployAllowList(auth *bind.TransactOpts, backend bind.ContractBackend, precompileAddr common.Address) (common.Address, *types.Transaction, *AllowList, error) {
	parsed, err := AllowListMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(AllowListBin), backend, precompileAddr)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &AllowList{AllowListCaller: AllowListCaller{contract: contract}, AllowListTransactor: AllowListTransactor{contract: contract}, AllowListFilterer: AllowListFilterer{contract: contract}}, nil
}

// AllowList is an auto generated Go binding around an Ethereum contract.
type AllowList struct {
	AllowListCaller     // Read-only binding to the contract
	AllowListTransactor // Write-only binding to the contract
	AllowListFilterer   // Log filterer for contract events
}

// AllowListCaller is an auto generated read-only Go binding around an Ethereum contract.
type AllowListCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// AllowListTransactor is an auto generated write-only Go binding around an Ethereum contract.
type AllowListTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// AllowListFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type AllowListFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// AllowListSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type AllowListSession struct {
	Contract     *AllowList        // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// AllowListCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type AllowListCallerSession struct {
	Contract *AllowListCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts    // Call options to use throughout this session
}

// AllowListTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type AllowListTransactorSession struct {
	Contract     *AllowListTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// AllowListRaw is an auto generated low-level Go binding around an Ethereum contract.
type AllowListRaw struct {
	Contract *AllowList // Generic contract binding to access the raw methods on
}

// AllowListCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type AllowListCallerRaw struct {
	Contract *AllowListCaller // Generic read-only contract binding to access the raw methods on
}

// AllowListTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type AllowListTransactorRaw struct {
	Contract *AllowListTransactor // Generic write-only contract binding to access the raw methods on
}

// NewAllowList creates a new instance of AllowList, bound to a specific deployed contract.
func NewAllowList(address common.Address, backend bind.ContractBackend) (*AllowList, error) {
	contract, err := bindAllowList(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &AllowList{AllowListCaller: AllowListCaller{contract: contract}, AllowListTransactor: AllowListTransactor{contract: contract}, AllowListFilterer: AllowListFilterer{contract: contract}}, nil
}

// NewAllowListCaller creates a new read-only instance of AllowList, bound to a specific deployed contract.
func NewAllowListCaller(address common.Address, caller bind.ContractCaller) (*AllowListCaller, error) {
	contract, err := bindAllowList(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &AllowListCaller{contract: contract}, nil
}

// NewAllowListTransactor creates a new write-only instance of AllowList, bound to a specific deployed contract.
func NewAllowListTransactor(address common.Address, transactor bind.ContractTransactor) (*AllowListTransactor, error) {
	contract, err := bindAllowList(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &AllowListTransactor{contract: contract}, nil
}

// NewAllowListFilterer creates a new log filterer instance of AllowList, bound to a specific deployed contract.
func NewAllowListFilterer(address common.Address, filterer bind.ContractFilterer) (*AllowListFilterer, error) {
	contract, err := bindAllowList(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &AllowListFilterer{contract: contract}, nil
}

// bindAllowList binds a generic wrapper to an already deployed contract.
func bindAllowList(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := AllowListMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_AllowList *AllowListRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _AllowList.Contract.AllowListCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_AllowList *AllowListRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _AllowList.Contract.AllowListTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_AllowList *AllowListRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _AllowList.Contract.AllowListTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_AllowList *AllowListCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _AllowList.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_AllowList *AllowListTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _AllowList.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_AllowList *AllowListTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _AllowList.Contract.contract.Transact(opts, method, params...)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_AllowList *AllowListCaller) IsAdmin(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _AllowList.contract.Call(opts, &out, "isAdmin", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_AllowList *AllowListSession) IsAdmin(addr common.Address) (bool, error) {
	return _AllowList.Contract.IsAdmin(&_AllowList.CallOpts, addr)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_AllowList *AllowListCallerSession) IsAdmin(addr common.Address) (bool, error) {
	return _AllowList.Contract.IsAdmin(&_AllowList.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_AllowList *AllowListCaller) IsEnabled(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _AllowList.contract.Call(opts, &out, "isEnabled", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_AllowList *AllowListSession) IsEnabled(addr common.Address) (bool, error) {
	return _AllowList.Contract.IsEnabled(&_AllowList.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_AllowList *AllowListCallerSession) IsEnabled(addr common.Address) (bool, error) {
	return _AllowList.Contract.IsEnabled(&_AllowList.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_AllowList *AllowListCaller) IsManager(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _AllowList.contract.Call(opts, &out, "isManager", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_AllowList *AllowListSession) IsManager(addr common.Address) (bool, error) {
	return _AllowList.Contract.IsManager(&_AllowList.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_AllowList *AllowListCallerSession) IsManager(addr common.Address) (bool, error) {
	return _AllowList.Contract.IsManager(&_AllowList.CallOpts, addr)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_AllowList *AllowListCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _AllowList.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_AllowList *AllowListSession) Owner() (common.Address, error) {
	return _AllowList.Contract.Owner(&_AllowList.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_AllowList *AllowListCallerSession) Owner() (common.Address, error) {
	return _AllowList.Contract.Owner(&_AllowList.CallOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_AllowList *AllowListTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _AllowList.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_AllowList *AllowListSession) RenounceOwnership() (*types.Transaction, error) {
	return _AllowList.Contract.RenounceOwnership(&_AllowList.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_AllowList *AllowListTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _AllowList.Contract.RenounceOwnership(&_AllowList.TransactOpts)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_AllowList *AllowListTransactor) Revoke(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowList.contract.Transact(opts, "revoke", addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_AllowList *AllowListSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.Revoke(&_AllowList.TransactOpts, addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_AllowList *AllowListTransactorSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.Revoke(&_AllowList.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_AllowList *AllowListTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowList.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_AllowList *AllowListSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.SetAdmin(&_AllowList.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_AllowList *AllowListTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.SetAdmin(&_AllowList.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_AllowList *AllowListTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowList.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_AllowList *AllowListSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.SetEnabled(&_AllowList.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_AllowList *AllowListTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.SetEnabled(&_AllowList.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_AllowList *AllowListTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowList.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_AllowList *AllowListSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.SetManager(&_AllowList.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_AllowList *AllowListTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.SetManager(&_AllowList.TransactOpts, addr)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_AllowList *AllowListTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _AllowList.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_AllowList *AllowListSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.TransferOwnership(&_AllowList.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_AllowList *AllowListTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _AllowList.Contract.TransferOwnership(&_AllowList.TransactOpts, newOwner)
}

// AllowListOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the AllowList contract.
type AllowListOwnershipTransferredIterator struct {
	Event *AllowListOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *AllowListOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(AllowListOwnershipTransferred)
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
		it.Event = new(AllowListOwnershipTransferred)
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
func (it *AllowListOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *AllowListOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// AllowListOwnershipTransferred represents a OwnershipTransferred event raised by the AllowList contract.
type AllowListOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_AllowList *AllowListFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*AllowListOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _AllowList.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &AllowListOwnershipTransferredIterator{contract: _AllowList.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_AllowList *AllowListFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *AllowListOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _AllowList.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(AllowListOwnershipTransferred)
				if err := _AllowList.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_AllowList *AllowListFilterer) ParseOwnershipTransferred(log types.Log) (*AllowListOwnershipTransferred, error) {
	event := new(AllowListOwnershipTransferred)
	if err := _AllowList.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
