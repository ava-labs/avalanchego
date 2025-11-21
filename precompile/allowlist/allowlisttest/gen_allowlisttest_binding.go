// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package allowlisttest

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
	Bin: "0x608060405234801561001057600080fd5b50604051610bee380380610bee833981810160405281019061003291906100dd565b80806000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550505061010a565b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006100aa8261007f565b9050919050565b6100ba8161009f565b81146100c557600080fd5b50565b6000815190506100d7816100b1565b92915050565b6000602082840312156100f3576100f261007a565b5b6000610101848285016100c8565b91505092915050565b610ad5806101196000396000f3fe608060405234801561001057600080fd5b50600436106100885760003560e01c806374a8f1031161005b57806374a8f103146100ff5780639015d3711461011b578063d0ebdbe71461014b578063f3ae24151461016757610088565b80630aaf70431461008d57806324d7806c146100a95780636cd5c39b146100d9578063704b6c02146100e3575b600080fd5b6100a760048036038101906100a2919061086a565b610197565b005b6100c360048036038101906100be919061086a565b6101fb565b6040516100d091906108b2565b60405180910390f35b6100e16102a6565b005b6100fd60048036038101906100f8919061086a565b6102d2565b005b6101196004803603810190610114919061086a565b610336565b005b6101356004803603810190610130919061086a565b61039a565b60405161014291906108b2565b60405180910390f35b6101656004803603810190610160919061086a565b610446565b005b610181600480360381019061017c919061086a565b6104aa565b60405161018e91906108b2565b60405180910390f35b6101a0336101fb565b806101b057506101af336104aa565b5b6101ef576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101e69061092a565b60405180910390fd5b6101f881610555565b50565b60008060008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016102579190610959565b602060405180830381865afa158015610274573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061029891906109aa565b905060028114915050919050565b6040516102b2906107fb565b604051809103906000f0801580156102ce573d6000803e3d6000fd5b5050565b6102db336101fb565b806102eb57506102ea336104aa565b5b61032a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016103219061092a565b60405180910390fd5b610333816105e3565b50565b61033f336101fb565b8061034f575061034e336104aa565b5b61038e576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016103859061092a565b60405180910390fd5b61039781610671565b50565b60008060008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016103f69190610959565b602060405180830381865afa158015610413573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061043791906109aa565b90506000811415915050919050565b61044f336101fb565b8061045f575061045e336104aa565b5b61049e576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016104959061092a565b60405180910390fd5b6104a78161076d565b50565b60008060008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016105069190610959565b602060405180830381865afa158015610523573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061054791906109aa565b905060038114915050919050565b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630aaf7043826040518263ffffffff1660e01b81526004016105ae9190610959565b600060405180830381600087803b1580156105c857600080fd5b505af11580156105dc573d6000803e3d6000fd5b5050505050565b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663704b6c02826040518263ffffffff1660e01b815260040161063c9190610959565b600060405180830381600087803b15801561065657600080fd5b505af115801561066a573d6000803e3d6000fd5b5050505050565b8073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff16036106df576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016106d690610a23565b60405180910390fd5b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638c6bfb3b826040518263ffffffff1660e01b81526004016107389190610959565b600060405180830381600087803b15801561075257600080fd5b505af1158015610766573d6000803e3d6000fd5b5050505050565b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663d0ebdbe7826040518263ffffffff1660e01b81526004016107c69190610959565b600060405180830381600087803b1580156107e057600080fd5b505af11580156107f4573d6000803e3d6000fd5b5050505050565b605c80610a4483390190565b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006108378261080c565b9050919050565b6108478161082c565b811461085257600080fd5b50565b6000813590506108648161083e565b92915050565b6000602082840312156108805761087f610807565b5b600061088e84828501610855565b91505092915050565b60008115159050919050565b6108ac81610897565b82525050565b60006020820190506108c760008301846108a3565b92915050565b600082825260208201905092915050565b7f63616e6e6f74206d6f6469667920616c6c6f77206c6973740000000000000000600082015250565b60006109146018836108cd565b915061091f826108de565b602082019050919050565b6000602082019050818103600083015261094381610907565b9050919050565b6109538161082c565b82525050565b600060208201905061096e600083018461094a565b92915050565b6000819050919050565b61098781610974565b811461099257600080fd5b50565b6000815190506109a48161097e565b92915050565b6000602082840312156109c0576109bf610807565b5b60006109ce84828501610995565b91505092915050565b7f63616e6e6f74207265766f6b65206f776e20726f6c6500000000000000000000600082015250565b6000610a0d6016836108cd565b9150610a18826109d7565b602082019050919050565b60006020820190508181036000830152610a3c81610a00565b905091905056fe6080604052348015600f57600080fd5b50603f80601d6000396000f3fe6080604052600080fdfea26469706673582212201b63ada7abb6e91d2d1e12f296495b8869914cb13f5e997f27ba66970aa940d664736f6c634300081e0033a2646970667358221220025e7ac7049d4fa024ad2b0dc09cafc87f270b781d61bf6c11a711f4ddd0ef2164736f6c634300081e0033",
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
