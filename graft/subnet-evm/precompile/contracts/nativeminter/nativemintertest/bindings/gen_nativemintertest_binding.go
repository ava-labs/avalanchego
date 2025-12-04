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

// NativeMinterTestMetaData contains all meta data concerning the NativeMinterTest contract.
var NativeMinterTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"nativeMinterPrecompile\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"mintNativeCoin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b50604051610336380380610336833981810160405281019061003191906100d4565b805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506100ff565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a38261007a565b9050919050565b6100b381610099565b81146100bd575f5ffd5b50565b5f815190506100ce816100aa565b92915050565b5f602082840312156100e9576100e8610076565b5b5f6100f6848285016100c0565b91505092915050565b61022a8061010c5f395ff3fe608060405260043610610021575f3560e01c80634f5aaaba1461002c57610028565b3661002857005b5f5ffd5b348015610037575f5ffd5b50610052600480360381019061004d9190610171565b610054565b005b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16634f5aaaba83836040518363ffffffff1660e01b81526004016100af9291906101cd565b5f604051808303815f87803b1580156100c6575f5ffd5b505af11580156100d8573d5f5f3e3d5ffd5b505050505050565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61010d826100e4565b9050919050565b61011d81610103565b8114610127575f5ffd5b50565b5f8135905061013881610114565b92915050565b5f819050919050565b6101508161013e565b811461015a575f5ffd5b50565b5f8135905061016b81610147565b92915050565b5f5f60408385031215610187576101866100e0565b5b5f6101948582860161012a565b92505060206101a58582860161015d565b9150509250929050565b6101b881610103565b82525050565b6101c78161013e565b82525050565b5f6040820190506101e05f8301856101af565b6101ed60208301846101be565b939250505056fea264697066735822122022a495ab74b25db78e2d373ef8499eee646e53fb4eb515ac693575252359c1ae64736f6c634300081e0033",
}

// NativeMinterTestABI is the input ABI used to generate the binding from.
// Deprecated: Use NativeMinterTestMetaData.ABI instead.
var NativeMinterTestABI = NativeMinterTestMetaData.ABI

// NativeMinterTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use NativeMinterTestMetaData.Bin instead.
var NativeMinterTestBin = NativeMinterTestMetaData.Bin

// DeployNativeMinterTest deploys a new Ethereum contract, binding an instance of NativeMinterTest to it.
func DeployNativeMinterTest(auth *bind.TransactOpts, backend bind.ContractBackend, nativeMinterPrecompile common.Address) (common.Address, *types.Transaction, *NativeMinterTest, error) {
	parsed, err := NativeMinterTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(NativeMinterTestBin), backend, nativeMinterPrecompile)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &NativeMinterTest{NativeMinterTestCaller: NativeMinterTestCaller{contract: contract}, NativeMinterTestTransactor: NativeMinterTestTransactor{contract: contract}, NativeMinterTestFilterer: NativeMinterTestFilterer{contract: contract}}, nil
}

// NativeMinterTest is an auto generated Go binding around an Ethereum contract.
type NativeMinterTest struct {
	NativeMinterTestCaller     // Read-only binding to the contract
	NativeMinterTestTransactor // Write-only binding to the contract
	NativeMinterTestFilterer   // Log filterer for contract events
}

// NativeMinterTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type NativeMinterTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// NativeMinterTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type NativeMinterTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// NativeMinterTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type NativeMinterTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// NativeMinterTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type NativeMinterTestSession struct {
	Contract     *NativeMinterTest // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// NativeMinterTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type NativeMinterTestCallerSession struct {
	Contract *NativeMinterTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// NativeMinterTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type NativeMinterTestTransactorSession struct {
	Contract     *NativeMinterTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// NativeMinterTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type NativeMinterTestRaw struct {
	Contract *NativeMinterTest // Generic contract binding to access the raw methods on
}

// NativeMinterTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type NativeMinterTestCallerRaw struct {
	Contract *NativeMinterTestCaller // Generic read-only contract binding to access the raw methods on
}

// NativeMinterTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type NativeMinterTestTransactorRaw struct {
	Contract *NativeMinterTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewNativeMinterTest creates a new instance of NativeMinterTest, bound to a specific deployed contract.
func NewNativeMinterTest(address common.Address, backend bind.ContractBackend) (*NativeMinterTest, error) {
	contract, err := bindNativeMinterTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &NativeMinterTest{NativeMinterTestCaller: NativeMinterTestCaller{contract: contract}, NativeMinterTestTransactor: NativeMinterTestTransactor{contract: contract}, NativeMinterTestFilterer: NativeMinterTestFilterer{contract: contract}}, nil
}

// NewNativeMinterTestCaller creates a new read-only instance of NativeMinterTest, bound to a specific deployed contract.
func NewNativeMinterTestCaller(address common.Address, caller bind.ContractCaller) (*NativeMinterTestCaller, error) {
	contract, err := bindNativeMinterTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &NativeMinterTestCaller{contract: contract}, nil
}

// NewNativeMinterTestTransactor creates a new write-only instance of NativeMinterTest, bound to a specific deployed contract.
func NewNativeMinterTestTransactor(address common.Address, transactor bind.ContractTransactor) (*NativeMinterTestTransactor, error) {
	contract, err := bindNativeMinterTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &NativeMinterTestTransactor{contract: contract}, nil
}

// NewNativeMinterTestFilterer creates a new log filterer instance of NativeMinterTest, bound to a specific deployed contract.
func NewNativeMinterTestFilterer(address common.Address, filterer bind.ContractFilterer) (*NativeMinterTestFilterer, error) {
	contract, err := bindNativeMinterTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &NativeMinterTestFilterer{contract: contract}, nil
}

// bindNativeMinterTest binds a generic wrapper to an already deployed contract.
func bindNativeMinterTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := NativeMinterTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_NativeMinterTest *NativeMinterTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _NativeMinterTest.Contract.NativeMinterTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_NativeMinterTest *NativeMinterTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _NativeMinterTest.Contract.NativeMinterTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_NativeMinterTest *NativeMinterTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _NativeMinterTest.Contract.NativeMinterTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_NativeMinterTest *NativeMinterTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _NativeMinterTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_NativeMinterTest *NativeMinterTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _NativeMinterTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_NativeMinterTest *NativeMinterTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _NativeMinterTest.Contract.contract.Transact(opts, method, params...)
}

// MintNativeCoin is a paid mutator transaction binding the contract method 0x4f5aaaba.
//
// Solidity: function mintNativeCoin(address addr, uint256 amount) returns()
func (_NativeMinterTest *NativeMinterTestTransactor) MintNativeCoin(opts *bind.TransactOpts, addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return _NativeMinterTest.contract.Transact(opts, "mintNativeCoin", addr, amount)
}

// MintNativeCoin is a paid mutator transaction binding the contract method 0x4f5aaaba.
//
// Solidity: function mintNativeCoin(address addr, uint256 amount) returns()
func (_NativeMinterTest *NativeMinterTestSession) MintNativeCoin(addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return _NativeMinterTest.Contract.MintNativeCoin(&_NativeMinterTest.TransactOpts, addr, amount)
}

// MintNativeCoin is a paid mutator transaction binding the contract method 0x4f5aaaba.
//
// Solidity: function mintNativeCoin(address addr, uint256 amount) returns()
func (_NativeMinterTest *NativeMinterTestTransactorSession) MintNativeCoin(addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return _NativeMinterTest.Contract.MintNativeCoin(&_NativeMinterTest.TransactOpts, addr, amount)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_NativeMinterTest *NativeMinterTestTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _NativeMinterTest.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_NativeMinterTest *NativeMinterTestSession) Receive() (*types.Transaction, error) {
	return _NativeMinterTest.Contract.Receive(&_NativeMinterTest.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_NativeMinterTest *NativeMinterTestTransactorSession) Receive() (*types.Transaction, error) {
	return _NativeMinterTest.Contract.Receive(&_NativeMinterTest.TransactOpts)
}
