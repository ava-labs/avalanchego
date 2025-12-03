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

// IFeeManagerFeeConfig is an auto generated low-level Go binding around an user-defined struct.
type IFeeManagerFeeConfig struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}

// IFeeManagerMetaData contains all meta data concerning the IFeeManager contract.
var IFeeManagerMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structIFeeManager.FeeConfig\",\"name\":\"oldFeeConfig\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structIFeeManager.FeeConfig\",\"name\":\"newFeeConfig\",\"type\":\"tuple\"}],\"name\":\"FeeConfigChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldRole\",\"type\":\"uint256\"}],\"name\":\"RoleSet\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"getFeeConfig\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getFeeConfigLastChangedAt\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"readAllowList\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"name\":\"setFeeConfig\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setNone\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// IFeeManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use IFeeManagerMetaData.ABI instead.
var IFeeManagerABI = IFeeManagerMetaData.ABI

// IFeeManager is an auto generated Go binding around an Ethereum contract.
type IFeeManager struct {
	IFeeManagerCaller     // Read-only binding to the contract
	IFeeManagerTransactor // Write-only binding to the contract
	IFeeManagerFilterer   // Log filterer for contract events
}

// IFeeManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type IFeeManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IFeeManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type IFeeManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IFeeManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type IFeeManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IFeeManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type IFeeManagerSession struct {
	Contract     *IFeeManager      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// IFeeManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type IFeeManagerCallerSession struct {
	Contract *IFeeManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// IFeeManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type IFeeManagerTransactorSession struct {
	Contract     *IFeeManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// IFeeManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type IFeeManagerRaw struct {
	Contract *IFeeManager // Generic contract binding to access the raw methods on
}

// IFeeManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type IFeeManagerCallerRaw struct {
	Contract *IFeeManagerCaller // Generic read-only contract binding to access the raw methods on
}

// IFeeManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type IFeeManagerTransactorRaw struct {
	Contract *IFeeManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewIFeeManager creates a new instance of IFeeManager, bound to a specific deployed contract.
func NewIFeeManager(address common.Address, backend bind.ContractBackend) (*IFeeManager, error) {
	contract, err := bindIFeeManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &IFeeManager{IFeeManagerCaller: IFeeManagerCaller{contract: contract}, IFeeManagerTransactor: IFeeManagerTransactor{contract: contract}, IFeeManagerFilterer: IFeeManagerFilterer{contract: contract}}, nil
}

// NewIFeeManagerCaller creates a new read-only instance of IFeeManager, bound to a specific deployed contract.
func NewIFeeManagerCaller(address common.Address, caller bind.ContractCaller) (*IFeeManagerCaller, error) {
	contract, err := bindIFeeManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &IFeeManagerCaller{contract: contract}, nil
}

// NewIFeeManagerTransactor creates a new write-only instance of IFeeManager, bound to a specific deployed contract.
func NewIFeeManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*IFeeManagerTransactor, error) {
	contract, err := bindIFeeManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &IFeeManagerTransactor{contract: contract}, nil
}

// NewIFeeManagerFilterer creates a new log filterer instance of IFeeManager, bound to a specific deployed contract.
func NewIFeeManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*IFeeManagerFilterer, error) {
	contract, err := bindIFeeManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &IFeeManagerFilterer{contract: contract}, nil
}

// bindIFeeManager binds a generic wrapper to an already deployed contract.
func bindIFeeManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := IFeeManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IFeeManager *IFeeManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IFeeManager.Contract.IFeeManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IFeeManager *IFeeManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IFeeManager.Contract.IFeeManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IFeeManager *IFeeManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IFeeManager.Contract.IFeeManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IFeeManager *IFeeManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IFeeManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IFeeManager *IFeeManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IFeeManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IFeeManager *IFeeManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IFeeManager.Contract.contract.Transact(opts, method, params...)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep)
func (_IFeeManager *IFeeManagerCaller) GetFeeConfig(opts *bind.CallOpts) (struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}, error) {
	var out []interface{}
	err := _IFeeManager.contract.Call(opts, &out, "getFeeConfig")

	outstruct := new(struct {
		GasLimit                 *big.Int
		TargetBlockRate          *big.Int
		MinBaseFee               *big.Int
		TargetGas                *big.Int
		BaseFeeChangeDenominator *big.Int
		MinBlockGasCost          *big.Int
		MaxBlockGasCost          *big.Int
		BlockGasCostStep         *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.GasLimit = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.TargetBlockRate = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.MinBaseFee = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.TargetGas = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)
	outstruct.BaseFeeChangeDenominator = *abi.ConvertType(out[4], new(*big.Int)).(**big.Int)
	outstruct.MinBlockGasCost = *abi.ConvertType(out[5], new(*big.Int)).(**big.Int)
	outstruct.MaxBlockGasCost = *abi.ConvertType(out[6], new(*big.Int)).(**big.Int)
	outstruct.BlockGasCostStep = *abi.ConvertType(out[7], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep)
func (_IFeeManager *IFeeManagerSession) GetFeeConfig() (struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}, error) {
	return _IFeeManager.Contract.GetFeeConfig(&_IFeeManager.CallOpts)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep)
func (_IFeeManager *IFeeManagerCallerSession) GetFeeConfig() (struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}, error) {
	return _IFeeManager.Contract.GetFeeConfig(&_IFeeManager.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256 blockNumber)
func (_IFeeManager *IFeeManagerCaller) GetFeeConfigLastChangedAt(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _IFeeManager.contract.Call(opts, &out, "getFeeConfigLastChangedAt")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256 blockNumber)
func (_IFeeManager *IFeeManagerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _IFeeManager.Contract.GetFeeConfigLastChangedAt(&_IFeeManager.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256 blockNumber)
func (_IFeeManager *IFeeManagerCallerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _IFeeManager.Contract.GetFeeConfigLastChangedAt(&_IFeeManager.CallOpts)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IFeeManager *IFeeManagerCaller) ReadAllowList(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _IFeeManager.contract.Call(opts, &out, "readAllowList", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IFeeManager *IFeeManagerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IFeeManager.Contract.ReadAllowList(&_IFeeManager.CallOpts, addr)
}

// ReadAllowList is a free data retrieval call binding the contract method 0xeb54dae1.
//
// Solidity: function readAllowList(address addr) view returns(uint256 role)
func (_IFeeManager *IFeeManagerCallerSession) ReadAllowList(addr common.Address) (*big.Int, error) {
	return _IFeeManager.Contract.ReadAllowList(&_IFeeManager.CallOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IFeeManager *IFeeManagerTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IFeeManager *IFeeManagerSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetAdmin(&_IFeeManager.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_IFeeManager *IFeeManagerTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetAdmin(&_IFeeManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IFeeManager *IFeeManagerTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IFeeManager *IFeeManagerSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetEnabled(&_IFeeManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_IFeeManager *IFeeManagerTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetEnabled(&_IFeeManager.TransactOpts, addr)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x8f10b586.
//
// Solidity: function setFeeConfig(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep) returns()
func (_IFeeManager *IFeeManagerTransactor) SetFeeConfig(opts *bind.TransactOpts, gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error) {
	return _IFeeManager.contract.Transact(opts, "setFeeConfig", gasLimit, targetBlockRate, minBaseFee, targetGas, baseFeeChangeDenominator, minBlockGasCost, maxBlockGasCost, blockGasCostStep)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x8f10b586.
//
// Solidity: function setFeeConfig(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep) returns()
func (_IFeeManager *IFeeManagerSession) SetFeeConfig(gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetFeeConfig(&_IFeeManager.TransactOpts, gasLimit, targetBlockRate, minBaseFee, targetGas, baseFeeChangeDenominator, minBlockGasCost, maxBlockGasCost, blockGasCostStep)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x8f10b586.
//
// Solidity: function setFeeConfig(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep) returns()
func (_IFeeManager *IFeeManagerTransactorSession) SetFeeConfig(gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetFeeConfig(&_IFeeManager.TransactOpts, gasLimit, targetBlockRate, minBaseFee, targetGas, baseFeeChangeDenominator, minBlockGasCost, maxBlockGasCost, blockGasCostStep)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IFeeManager *IFeeManagerTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IFeeManager *IFeeManagerSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetManager(&_IFeeManager.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_IFeeManager *IFeeManagerTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetManager(&_IFeeManager.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IFeeManager *IFeeManagerTransactor) SetNone(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.contract.Transact(opts, "setNone", addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IFeeManager *IFeeManagerSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetNone(&_IFeeManager.TransactOpts, addr)
}

// SetNone is a paid mutator transaction binding the contract method 0x8c6bfb3b.
//
// Solidity: function setNone(address addr) returns()
func (_IFeeManager *IFeeManagerTransactorSession) SetNone(addr common.Address) (*types.Transaction, error) {
	return _IFeeManager.Contract.SetNone(&_IFeeManager.TransactOpts, addr)
}

// IFeeManagerFeeConfigChangedIterator is returned from FilterFeeConfigChanged and is used to iterate over the raw logs and unpacked data for FeeConfigChanged events raised by the IFeeManager contract.
type IFeeManagerFeeConfigChangedIterator struct {
	Event *IFeeManagerFeeConfigChanged // Event containing the contract specifics and raw log

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
func (it *IFeeManagerFeeConfigChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IFeeManagerFeeConfigChanged)
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
		it.Event = new(IFeeManagerFeeConfigChanged)
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
func (it *IFeeManagerFeeConfigChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IFeeManagerFeeConfigChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IFeeManagerFeeConfigChanged represents a FeeConfigChanged event raised by the IFeeManager contract.
type IFeeManagerFeeConfigChanged struct {
	Sender       common.Address
	OldFeeConfig IFeeManagerFeeConfig
	NewFeeConfig IFeeManagerFeeConfig
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterFeeConfigChanged is a free log retrieval operation binding the contract event 0x4c98e43adb5962c18f3f0e6dd066e2a2de258d3b4f695b317b77c8f27cd044fc.
//
// Solidity: event FeeConfigChanged(address indexed sender, (uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) oldFeeConfig, (uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) newFeeConfig)
func (_IFeeManager *IFeeManagerFilterer) FilterFeeConfigChanged(opts *bind.FilterOpts, sender []common.Address) (*IFeeManagerFeeConfigChangedIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IFeeManager.contract.FilterLogs(opts, "FeeConfigChanged", senderRule)
	if err != nil {
		return nil, err
	}
	return &IFeeManagerFeeConfigChangedIterator{contract: _IFeeManager.contract, event: "FeeConfigChanged", logs: logs, sub: sub}, nil
}

// WatchFeeConfigChanged is a free log subscription operation binding the contract event 0x4c98e43adb5962c18f3f0e6dd066e2a2de258d3b4f695b317b77c8f27cd044fc.
//
// Solidity: event FeeConfigChanged(address indexed sender, (uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) oldFeeConfig, (uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) newFeeConfig)
func (_IFeeManager *IFeeManagerFilterer) WatchFeeConfigChanged(opts *bind.WatchOpts, sink chan<- *IFeeManagerFeeConfigChanged, sender []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _IFeeManager.contract.WatchLogs(opts, "FeeConfigChanged", senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IFeeManagerFeeConfigChanged)
				if err := _IFeeManager.contract.UnpackLog(event, "FeeConfigChanged", log); err != nil {
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

// ParseFeeConfigChanged is a log parse operation binding the contract event 0x4c98e43adb5962c18f3f0e6dd066e2a2de258d3b4f695b317b77c8f27cd044fc.
//
// Solidity: event FeeConfigChanged(address indexed sender, (uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) oldFeeConfig, (uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) newFeeConfig)
func (_IFeeManager *IFeeManagerFilterer) ParseFeeConfigChanged(log types.Log) (*IFeeManagerFeeConfigChanged, error) {
	event := new(IFeeManagerFeeConfigChanged)
	if err := _IFeeManager.contract.UnpackLog(event, "FeeConfigChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// IFeeManagerRoleSetIterator is returned from FilterRoleSet and is used to iterate over the raw logs and unpacked data for RoleSet events raised by the IFeeManager contract.
type IFeeManagerRoleSetIterator struct {
	Event *IFeeManagerRoleSet // Event containing the contract specifics and raw log

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
func (it *IFeeManagerRoleSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(IFeeManagerRoleSet)
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
		it.Event = new(IFeeManagerRoleSet)
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
func (it *IFeeManagerRoleSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *IFeeManagerRoleSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// IFeeManagerRoleSet represents a RoleSet event raised by the IFeeManager contract.
type IFeeManagerRoleSet struct {
	Role    *big.Int
	Account common.Address
	Sender  common.Address
	OldRole *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleSet is a free log retrieval operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IFeeManager *IFeeManagerFilterer) FilterRoleSet(opts *bind.FilterOpts, role []*big.Int, account []common.Address, sender []common.Address) (*IFeeManagerRoleSetIterator, error) {

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

	logs, sub, err := _IFeeManager.contract.FilterLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &IFeeManagerRoleSetIterator{contract: _IFeeManager.contract, event: "RoleSet", logs: logs, sub: sub}, nil
}

// WatchRoleSet is a free log subscription operation binding the contract event 0xcdb7ea01f00a414d78757bdb0f6391664ba3fedf987eed280927c1e7d695be3e.
//
// Solidity: event RoleSet(uint256 indexed role, address indexed account, address indexed sender, uint256 oldRole)
func (_IFeeManager *IFeeManagerFilterer) WatchRoleSet(opts *bind.WatchOpts, sink chan<- *IFeeManagerRoleSet, role []*big.Int, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _IFeeManager.contract.WatchLogs(opts, "RoleSet", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(IFeeManagerRoleSet)
				if err := _IFeeManager.contract.UnpackLog(event, "RoleSet", log); err != nil {
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
func (_IFeeManager *IFeeManagerFilterer) ParseRoleSet(log types.Log) (*IFeeManagerRoleSet, error) {
	event := new(IFeeManagerRoleSet)
	if err := _IFeeManager.contract.UnpackLog(event, "RoleSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
