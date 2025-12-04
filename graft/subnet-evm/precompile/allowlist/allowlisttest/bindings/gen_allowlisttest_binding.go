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

// AllowListTestMetaData contains all meta data concerning the AllowListTest contract.
var AllowListTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"precompileAddr\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"deployContract\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isAdmin\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isEnabled\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isManager\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"revoke\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b50604051610ba1380380610ba1833981810160405281019061003191906100d6565b80805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055505050610101565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a58261007c565b9050919050565b6100b58161009b565b81146100bf575f5ffd5b50565b5f815190506100d0816100ac565b92915050565b5f602082840312156100eb576100ea610078565b5b5f6100f8848285016100c2565b91505092915050565b610a938061010e5f395ff3fe608060405234801561000f575f5ffd5b5060043610610086575f3560e01c806374a8f1031161005957806374a8f103146100fc5780639015d37114610118578063d0ebdbe714610148578063f3ae24151461016457610086565b80630aaf70431461008a57806324d7806c146100a65780636cd5c39b146100d6578063704b6c02146100e0575b5f5ffd5b6100a4600480360381019061009f9190610841565b610194565b005b6100c060048036038101906100bb9190610841565b6101f8565b6040516100cd9190610886565b60405180910390f35b6100de6102a0565b005b6100fa60048036038101906100f59190610841565b6102c9565b005b61011660048036038101906101119190610841565b61032d565b005b610132600480360381019061012d9190610841565b610391565b60405161013f9190610886565b60405180910390f35b610162600480360381019061015d9190610841565b610439565b005b61017e60048036038101906101799190610841565b61049d565b60405161018b9190610886565b60405180910390f35b61019d336101f8565b806101ad57506101ac3361049d565b5b6101ec576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101e3906108f9565b60405180910390fd5b6101f581610545565b50565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016102539190610926565b602060405180830381865afa15801561026e573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102929190610972565b905060028114915050919050565b6040516102ac906107d7565b604051809103905ff0801580156102c5573d5f5f3e3d5ffd5b5050565b6102d2336101f8565b806102e257506102e13361049d565b5b610321576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610318906108f9565b60405180910390fd5b61032a816105ce565b50565b610336336101f8565b8061034657506103453361049d565b5b610385576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161037c906108f9565b60405180910390fd5b61038e81610657565b50565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016103ec9190610926565b602060405180830381865afa158015610407573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061042b9190610972565b90505f811415915050919050565b610442336101f8565b8061045257506104513361049d565b5b610491576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610488906108f9565b60405180910390fd5b61049a8161074e565b50565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016104f89190610926565b602060405180830381865afa158015610513573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906105379190610972565b905060038114915050919050565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630aaf7043826040518263ffffffff1660e01b815260040161059e9190610926565b5f604051808303815f87803b1580156105b5575f5ffd5b505af11580156105c7573d5f5f3e3d5ffd5b5050505050565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663704b6c02826040518263ffffffff1660e01b81526004016106279190610926565b5f604051808303815f87803b15801561063e575f5ffd5b505af1158015610650573d5f5f3e3d5ffd5b5050505050565b8073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff16036106c5576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016106bc906109e7565b60405180910390fd5b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638c6bfb3b826040518263ffffffff1660e01b815260040161071e9190610926565b5f604051808303815f87803b158015610735575f5ffd5b505af1158015610747573d5f5f3e3d5ffd5b5050505050565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663d0ebdbe7826040518263ffffffff1660e01b81526004016107a79190610926565b5f604051808303815f87803b1580156107be575f5ffd5b505af11580156107d0573d5f5f3e3d5ffd5b5050505050565b605880610a0683390190565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f610810826107e7565b9050919050565b61082081610806565b811461082a575f5ffd5b50565b5f8135905061083b81610817565b92915050565b5f60208284031215610856576108556107e3565b5b5f6108638482850161082d565b91505092915050565b5f8115159050919050565b6108808161086c565b82525050565b5f6020820190506108995f830184610877565b92915050565b5f82825260208201905092915050565b7f63616e6e6f74206d6f6469667920616c6c6f77206c69737400000000000000005f82015250565b5f6108e360188361089f565b91506108ee826108af565b602082019050919050565b5f6020820190508181035f830152610910816108d7565b9050919050565b61092081610806565b82525050565b5f6020820190506109395f830184610917565b92915050565b5f819050919050565b6109518161093f565b811461095b575f5ffd5b50565b5f8151905061096c81610948565b92915050565b5f60208284031215610987576109866107e3565b5b5f6109948482850161095e565b91505092915050565b7f63616e6e6f74207265766f6b65206f776e20726f6c65000000000000000000005f82015250565b5f6109d160168361089f565b91506109dc8261099d565b602082019050919050565b5f6020820190508181035f8301526109fe816109c5565b905091905056fe6080604052348015600e575f5ffd5b50603e80601a5f395ff3fe60806040525f5ffdfea2646970667358221220a6f35569427acc58ad5367cdacdafc7cbf00ed36744b876fba3bd8433689c4a564736f6c634300081e0033a2646970667358221220b9610a9e66cd5a3a7c0ccc575d7228dff9f3846ffc78cf4eccd34dd152f9297664736f6c634300081e0033",
}

// AllowListTestABI is the input ABI used to generate the binding from.
// Deprecated: Use AllowListTestMetaData.ABI instead.
var AllowListTestABI = AllowListTestMetaData.ABI

// AllowListTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use AllowListTestMetaData.Bin instead.
var AllowListTestBin = AllowListTestMetaData.Bin

// DeployAllowListTest deploys a new Ethereum contract, binding an instance of AllowListTest to it.
func DeployAllowListTest(auth *bind.TransactOpts, backend bind.ContractBackend, precompileAddr common.Address) (common.Address, *types.Transaction, *AllowListTest, error) {
	parsed, err := AllowListTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(AllowListTestBin), backend, precompileAddr)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &AllowListTest{AllowListTestCaller: AllowListTestCaller{contract: contract}, AllowListTestTransactor: AllowListTestTransactor{contract: contract}, AllowListTestFilterer: AllowListTestFilterer{contract: contract}}, nil
}

// AllowListTest is an auto generated Go binding around an Ethereum contract.
type AllowListTest struct {
	AllowListTestCaller     // Read-only binding to the contract
	AllowListTestTransactor // Write-only binding to the contract
	AllowListTestFilterer   // Log filterer for contract events
}

// AllowListTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type AllowListTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// AllowListTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type AllowListTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// AllowListTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type AllowListTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// AllowListTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type AllowListTestSession struct {
	Contract     *AllowListTest    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// AllowListTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type AllowListTestCallerSession struct {
	Contract *AllowListTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// AllowListTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type AllowListTestTransactorSession struct {
	Contract     *AllowListTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// AllowListTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type AllowListTestRaw struct {
	Contract *AllowListTest // Generic contract binding to access the raw methods on
}

// AllowListTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type AllowListTestCallerRaw struct {
	Contract *AllowListTestCaller // Generic read-only contract binding to access the raw methods on
}

// AllowListTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type AllowListTestTransactorRaw struct {
	Contract *AllowListTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewAllowListTest creates a new instance of AllowListTest, bound to a specific deployed contract.
func NewAllowListTest(address common.Address, backend bind.ContractBackend) (*AllowListTest, error) {
	contract, err := bindAllowListTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &AllowListTest{AllowListTestCaller: AllowListTestCaller{contract: contract}, AllowListTestTransactor: AllowListTestTransactor{contract: contract}, AllowListTestFilterer: AllowListTestFilterer{contract: contract}}, nil
}

// NewAllowListTestCaller creates a new read-only instance of AllowListTest, bound to a specific deployed contract.
func NewAllowListTestCaller(address common.Address, caller bind.ContractCaller) (*AllowListTestCaller, error) {
	contract, err := bindAllowListTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &AllowListTestCaller{contract: contract}, nil
}

// NewAllowListTestTransactor creates a new write-only instance of AllowListTest, bound to a specific deployed contract.
func NewAllowListTestTransactor(address common.Address, transactor bind.ContractTransactor) (*AllowListTestTransactor, error) {
	contract, err := bindAllowListTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &AllowListTestTransactor{contract: contract}, nil
}

// NewAllowListTestFilterer creates a new log filterer instance of AllowListTest, bound to a specific deployed contract.
func NewAllowListTestFilterer(address common.Address, filterer bind.ContractFilterer) (*AllowListTestFilterer, error) {
	contract, err := bindAllowListTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &AllowListTestFilterer{contract: contract}, nil
}

// bindAllowListTest binds a generic wrapper to an already deployed contract.
func bindAllowListTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := AllowListTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_AllowListTest *AllowListTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _AllowListTest.Contract.AllowListTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_AllowListTest *AllowListTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _AllowListTest.Contract.AllowListTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_AllowListTest *AllowListTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _AllowListTest.Contract.AllowListTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_AllowListTest *AllowListTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _AllowListTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_AllowListTest *AllowListTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _AllowListTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_AllowListTest *AllowListTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _AllowListTest.Contract.contract.Transact(opts, method, params...)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_AllowListTest *AllowListTestCaller) IsAdmin(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _AllowListTest.contract.Call(opts, &out, "isAdmin", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_AllowListTest *AllowListTestSession) IsAdmin(addr common.Address) (bool, error) {
	return _AllowListTest.Contract.IsAdmin(&_AllowListTest.CallOpts, addr)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_AllowListTest *AllowListTestCallerSession) IsAdmin(addr common.Address) (bool, error) {
	return _AllowListTest.Contract.IsAdmin(&_AllowListTest.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_AllowListTest *AllowListTestCaller) IsEnabled(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _AllowListTest.contract.Call(opts, &out, "isEnabled", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_AllowListTest *AllowListTestSession) IsEnabled(addr common.Address) (bool, error) {
	return _AllowListTest.Contract.IsEnabled(&_AllowListTest.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_AllowListTest *AllowListTestCallerSession) IsEnabled(addr common.Address) (bool, error) {
	return _AllowListTest.Contract.IsEnabled(&_AllowListTest.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_AllowListTest *AllowListTestCaller) IsManager(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _AllowListTest.contract.Call(opts, &out, "isManager", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_AllowListTest *AllowListTestSession) IsManager(addr common.Address) (bool, error) {
	return _AllowListTest.Contract.IsManager(&_AllowListTest.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_AllowListTest *AllowListTestCallerSession) IsManager(addr common.Address) (bool, error) {
	return _AllowListTest.Contract.IsManager(&_AllowListTest.CallOpts, addr)
}

// DeployContract is a paid mutator transaction binding the contract method 0x6cd5c39b.
//
// Solidity: function deployContract() returns()
func (_AllowListTest *AllowListTestTransactor) DeployContract(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _AllowListTest.contract.Transact(opts, "deployContract")
}

// DeployContract is a paid mutator transaction binding the contract method 0x6cd5c39b.
//
// Solidity: function deployContract() returns()
func (_AllowListTest *AllowListTestSession) DeployContract() (*types.Transaction, error) {
	return _AllowListTest.Contract.DeployContract(&_AllowListTest.TransactOpts)
}

// DeployContract is a paid mutator transaction binding the contract method 0x6cd5c39b.
//
// Solidity: function deployContract() returns()
func (_AllowListTest *AllowListTestTransactorSession) DeployContract() (*types.Transaction, error) {
	return _AllowListTest.Contract.DeployContract(&_AllowListTest.TransactOpts)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_AllowListTest *AllowListTestTransactor) Revoke(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.contract.Transact(opts, "revoke", addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_AllowListTest *AllowListTestSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.Revoke(&_AllowListTest.TransactOpts, addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_AllowListTest *AllowListTestTransactorSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.Revoke(&_AllowListTest.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_AllowListTest *AllowListTestTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_AllowListTest *AllowListTestSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.SetAdmin(&_AllowListTest.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_AllowListTest *AllowListTestTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.SetAdmin(&_AllowListTest.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_AllowListTest *AllowListTestTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_AllowListTest *AllowListTestSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.SetEnabled(&_AllowListTest.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_AllowListTest *AllowListTestTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.SetEnabled(&_AllowListTest.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_AllowListTest *AllowListTestTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_AllowListTest *AllowListTestSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.SetManager(&_AllowListTest.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_AllowListTest *AllowListTestTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _AllowListTest.Contract.SetManager(&_AllowListTest.TransactOpts, addr)
}
