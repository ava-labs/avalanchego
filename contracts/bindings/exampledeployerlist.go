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

// ExampleDeployerListMetaData contains all meta data concerning the ExampleDeployerList contract.
var ExampleDeployerListMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"OwnableInvalidOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"OwnableUnauthorizedAccount\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"deployContract\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isAdmin\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isEnabled\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isManager\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"revoke\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b50730200000000000000000000000000000000000000335f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1603610096575f6040517f1e4fbdf700000000000000000000000000000000000000000000000000000000815260040161008d91906101ec565b60405180910390fd5b6100a5816100ec60201b60201c565b508060015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050610205565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6101d6826101ad565b9050919050565b6101e6816101cc565b82525050565b5f6020820190506101ff5f8301846101dd565b92915050565b610b64806102125f395ff3fe608060405234801561000f575f5ffd5b50600436106100a7575f3560e01c806374a8f1031161006f57806374a8f103146101275780638da5cb5b146101435780639015d37114610161578063d0ebdbe714610191578063f2fde38b146101ad578063f3ae2415146101c9576100a7565b80630aaf7043146100ab57806324d7806c146100c75780636cd5c39b146100f7578063704b6c0214610101578063715018a61461011d575b5f5ffd5b6100c560048036038101906100c0919061097a565b6101f9565b005b6100e160048036038101906100dc919061097a565b61020d565b6040516100ee91906109bf565b60405180910390f35b6100ff6102b6565b005b61011b6004803603810190610116919061097a565b6102df565b005b6101256102f3565b005b610141600480360381019061013c919061097a565b610306565b005b61014b61031a565b60405161015891906109e7565b60405180910390f35b61017b6004803603810190610176919061097a565b610341565b60405161018891906109bf565b60405180910390f35b6101ab60048036038101906101a6919061097a565b6103ea565b005b6101c760048036038101906101c2919061097a565b6103fe565b005b6101e360048036038101906101de919061097a565b610482565b6040516101f091906109bf565b60405180910390f35b61020161052b565b61020a816105b2565b50565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b815260040161026991906109e7565b602060405180830381865afa158015610284573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102a89190610a33565b905060028114915050919050565b6040516102c290610910565b604051809103905ff0801580156102db573d5f5f3e3d5ffd5b5050565b6102e761052b565b6102f08161063c565b50565b6102fb61052b565b6103045f6106c6565b565b61030e61052b565b61031781610787565b50565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b815260040161039d91906109e7565b602060405180830381865afa1580156103b8573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103dc9190610a33565b90505f811415915050919050565b6103f261052b565b6103fb8161087f565b50565b61040661052b565b5f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1603610476575f6040517f1e4fbdf700000000000000000000000000000000000000000000000000000000815260040161046d91906109e7565b60405180910390fd5b61047f816106c6565b50565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016104de91906109e7565b602060405180830381865afa1580156104f9573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061051d9190610a33565b905060038114915050919050565b610533610909565b73ffffffffffffffffffffffffffffffffffffffff1661055161031a565b73ffffffffffffffffffffffffffffffffffffffff16146105b057610574610909565b6040517f118cdaa70000000000000000000000000000000000000000000000000000000081526004016105a791906109e7565b60405180910390fd5b565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630aaf7043826040518263ffffffff1660e01b815260040161060c91906109e7565b5f604051808303815f87803b158015610623575f5ffd5b505af1158015610635573d5f5f3e3d5ffd5b5050505050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663704b6c02826040518263ffffffff1660e01b815260040161069691906109e7565b5f604051808303815f87803b1580156106ad575f5ffd5b505af11580156106bf573d5f5f3e3d5ffd5b5050505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b8073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff16036107f5576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107ec90610ab8565b60405180910390fd5b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638c6bfb3b826040518263ffffffff1660e01b815260040161084f91906109e7565b5f604051808303815f87803b158015610866575f5ffd5b505af1158015610878573d5f5f3e3d5ffd5b5050505050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663d0ebdbe7826040518263ffffffff1660e01b81526004016108d991906109e7565b5f604051808303815f87803b1580156108f0575f5ffd5b505af1158015610902573d5f5f3e3d5ffd5b5050505050565b5f33905090565b605880610ad783390190565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61094982610920565b9050919050565b6109598161093f565b8114610963575f5ffd5b50565b5f8135905061097481610950565b92915050565b5f6020828403121561098f5761098e61091c565b5b5f61099c84828501610966565b91505092915050565b5f8115159050919050565b6109b9816109a5565b82525050565b5f6020820190506109d25f8301846109b0565b92915050565b6109e18161093f565b82525050565b5f6020820190506109fa5f8301846109d8565b92915050565b5f819050919050565b610a1281610a00565b8114610a1c575f5ffd5b50565b5f81519050610a2d81610a09565b92915050565b5f60208284031215610a4857610a4761091c565b5b5f610a5584828501610a1f565b91505092915050565b5f82825260208201905092915050565b7f63616e6e6f74207265766f6b65206f776e20726f6c65000000000000000000005f82015250565b5f610aa2601683610a5e565b9150610aad82610a6e565b602082019050919050565b5f6020820190508181035f830152610acf81610a96565b905091905056fe6080604052348015600e575f5ffd5b50603e80601a5f395ff3fe60806040525f5ffdfea26469706673582212204ec2f544eb3d0b4a6bcccfa4c97034822a41b9ca5dfa8d5e912e6b754703f83e64736f6c634300081e0033a264697066735822122074e0033a5890b8801c03002726986741e86eab5032987b4e0f9870b018b50f4764736f6c634300081e0033",
}

// ExampleDeployerListABI is the input ABI used to generate the binding from.
// Deprecated: Use ExampleDeployerListMetaData.ABI instead.
var ExampleDeployerListABI = ExampleDeployerListMetaData.ABI

// ExampleDeployerListBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ExampleDeployerListMetaData.Bin instead.
var ExampleDeployerListBin = ExampleDeployerListMetaData.Bin

// DeployExampleDeployerList deploys a new Ethereum contract, binding an instance of ExampleDeployerList to it.
func DeployExampleDeployerList(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ExampleDeployerList, error) {
	parsed, err := ExampleDeployerListMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ExampleDeployerListBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ExampleDeployerList{ExampleDeployerListCaller: ExampleDeployerListCaller{contract: contract}, ExampleDeployerListTransactor: ExampleDeployerListTransactor{contract: contract}, ExampleDeployerListFilterer: ExampleDeployerListFilterer{contract: contract}}, nil
}

// ExampleDeployerList is an auto generated Go binding around an Ethereum contract.
type ExampleDeployerList struct {
	ExampleDeployerListCaller     // Read-only binding to the contract
	ExampleDeployerListTransactor // Write-only binding to the contract
	ExampleDeployerListFilterer   // Log filterer for contract events
}

// ExampleDeployerListCaller is an auto generated read-only Go binding around an Ethereum contract.
type ExampleDeployerListCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleDeployerListTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ExampleDeployerListTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleDeployerListFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ExampleDeployerListFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleDeployerListSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ExampleDeployerListSession struct {
	Contract     *ExampleDeployerList // Generic contract binding to set the session for
	CallOpts     bind.CallOpts        // Call options to use throughout this session
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// ExampleDeployerListCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ExampleDeployerListCallerSession struct {
	Contract *ExampleDeployerListCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts              // Call options to use throughout this session
}

// ExampleDeployerListTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ExampleDeployerListTransactorSession struct {
	Contract     *ExampleDeployerListTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts              // Transaction auth options to use throughout this session
}

// ExampleDeployerListRaw is an auto generated low-level Go binding around an Ethereum contract.
type ExampleDeployerListRaw struct {
	Contract *ExampleDeployerList // Generic contract binding to access the raw methods on
}

// ExampleDeployerListCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ExampleDeployerListCallerRaw struct {
	Contract *ExampleDeployerListCaller // Generic read-only contract binding to access the raw methods on
}

// ExampleDeployerListTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ExampleDeployerListTransactorRaw struct {
	Contract *ExampleDeployerListTransactor // Generic write-only contract binding to access the raw methods on
}

// NewExampleDeployerList creates a new instance of ExampleDeployerList, bound to a specific deployed contract.
func NewExampleDeployerList(address common.Address, backend bind.ContractBackend) (*ExampleDeployerList, error) {
	contract, err := bindExampleDeployerList(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ExampleDeployerList{ExampleDeployerListCaller: ExampleDeployerListCaller{contract: contract}, ExampleDeployerListTransactor: ExampleDeployerListTransactor{contract: contract}, ExampleDeployerListFilterer: ExampleDeployerListFilterer{contract: contract}}, nil
}

// NewExampleDeployerListCaller creates a new read-only instance of ExampleDeployerList, bound to a specific deployed contract.
func NewExampleDeployerListCaller(address common.Address, caller bind.ContractCaller) (*ExampleDeployerListCaller, error) {
	contract, err := bindExampleDeployerList(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleDeployerListCaller{contract: contract}, nil
}

// NewExampleDeployerListTransactor creates a new write-only instance of ExampleDeployerList, bound to a specific deployed contract.
func NewExampleDeployerListTransactor(address common.Address, transactor bind.ContractTransactor) (*ExampleDeployerListTransactor, error) {
	contract, err := bindExampleDeployerList(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleDeployerListTransactor{contract: contract}, nil
}

// NewExampleDeployerListFilterer creates a new log filterer instance of ExampleDeployerList, bound to a specific deployed contract.
func NewExampleDeployerListFilterer(address common.Address, filterer bind.ContractFilterer) (*ExampleDeployerListFilterer, error) {
	contract, err := bindExampleDeployerList(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ExampleDeployerListFilterer{contract: contract}, nil
}

// bindExampleDeployerList binds a generic wrapper to an already deployed contract.
func bindExampleDeployerList(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ExampleDeployerListMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleDeployerList *ExampleDeployerListRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleDeployerList.Contract.ExampleDeployerListCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleDeployerList *ExampleDeployerListRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.ExampleDeployerListTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleDeployerList *ExampleDeployerListRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.ExampleDeployerListTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleDeployerList *ExampleDeployerListCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleDeployerList.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleDeployerList *ExampleDeployerListTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleDeployerList *ExampleDeployerListTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.contract.Transact(opts, method, params...)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListCaller) IsAdmin(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ExampleDeployerList.contract.Call(opts, &out, "isAdmin", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListSession) IsAdmin(addr common.Address) (bool, error) {
	return _ExampleDeployerList.Contract.IsAdmin(&_ExampleDeployerList.CallOpts, addr)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListCallerSession) IsAdmin(addr common.Address) (bool, error) {
	return _ExampleDeployerList.Contract.IsAdmin(&_ExampleDeployerList.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListCaller) IsEnabled(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ExampleDeployerList.contract.Call(opts, &out, "isEnabled", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListSession) IsEnabled(addr common.Address) (bool, error) {
	return _ExampleDeployerList.Contract.IsEnabled(&_ExampleDeployerList.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListCallerSession) IsEnabled(addr common.Address) (bool, error) {
	return _ExampleDeployerList.Contract.IsEnabled(&_ExampleDeployerList.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListCaller) IsManager(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ExampleDeployerList.contract.Call(opts, &out, "isManager", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListSession) IsManager(addr common.Address) (bool, error) {
	return _ExampleDeployerList.Contract.IsManager(&_ExampleDeployerList.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ExampleDeployerList *ExampleDeployerListCallerSession) IsManager(addr common.Address) (bool, error) {
	return _ExampleDeployerList.Contract.IsManager(&_ExampleDeployerList.CallOpts, addr)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleDeployerList *ExampleDeployerListCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ExampleDeployerList.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleDeployerList *ExampleDeployerListSession) Owner() (common.Address, error) {
	return _ExampleDeployerList.Contract.Owner(&_ExampleDeployerList.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleDeployerList *ExampleDeployerListCallerSession) Owner() (common.Address, error) {
	return _ExampleDeployerList.Contract.Owner(&_ExampleDeployerList.CallOpts)
}

// DeployContract is a paid mutator transaction binding the contract method 0x6cd5c39b.
//
// Solidity: function deployContract() returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) DeployContract(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "deployContract")
}

// DeployContract is a paid mutator transaction binding the contract method 0x6cd5c39b.
//
// Solidity: function deployContract() returns()
func (_ExampleDeployerList *ExampleDeployerListSession) DeployContract() (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.DeployContract(&_ExampleDeployerList.TransactOpts)
}

// DeployContract is a paid mutator transaction binding the contract method 0x6cd5c39b.
//
// Solidity: function deployContract() returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) DeployContract() (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.DeployContract(&_ExampleDeployerList.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleDeployerList *ExampleDeployerListSession) RenounceOwnership() (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.RenounceOwnership(&_ExampleDeployerList.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.RenounceOwnership(&_ExampleDeployerList.TransactOpts)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) Revoke(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "revoke", addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.Revoke(&_ExampleDeployerList.TransactOpts, addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.Revoke(&_ExampleDeployerList.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.SetAdmin(&_ExampleDeployerList.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.SetAdmin(&_ExampleDeployerList.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.SetEnabled(&_ExampleDeployerList.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.SetEnabled(&_ExampleDeployerList.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.SetManager(&_ExampleDeployerList.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.SetManager(&_ExampleDeployerList.TransactOpts, addr)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleDeployerList *ExampleDeployerListSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.TransferOwnership(&_ExampleDeployerList.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleDeployerList *ExampleDeployerListTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ExampleDeployerList.Contract.TransferOwnership(&_ExampleDeployerList.TransactOpts, newOwner)
}

// ExampleDeployerListOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the ExampleDeployerList contract.
type ExampleDeployerListOwnershipTransferredIterator struct {
	Event *ExampleDeployerListOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *ExampleDeployerListOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ExampleDeployerListOwnershipTransferred)
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
		it.Event = new(ExampleDeployerListOwnershipTransferred)
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
func (it *ExampleDeployerListOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ExampleDeployerListOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ExampleDeployerListOwnershipTransferred represents a OwnershipTransferred event raised by the ExampleDeployerList contract.
type ExampleDeployerListOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleDeployerList *ExampleDeployerListFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ExampleDeployerListOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ExampleDeployerList.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ExampleDeployerListOwnershipTransferredIterator{contract: _ExampleDeployerList.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleDeployerList *ExampleDeployerListFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *ExampleDeployerListOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ExampleDeployerList.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ExampleDeployerListOwnershipTransferred)
				if err := _ExampleDeployerList.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_ExampleDeployerList *ExampleDeployerListFilterer) ParseOwnershipTransferred(log types.Log) (*ExampleDeployerListOwnershipTransferred, error) {
	event := new(ExampleDeployerListOwnershipTransferred)
	if err := _ExampleDeployerList.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
