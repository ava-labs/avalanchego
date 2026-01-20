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

// FeeManagerTestMetaData contains all meta data concerning the FeeManagerTest contract.
var FeeManagerTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"feeManagerPrecompile\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"getFeeConfig\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getFeeConfigLastChangedAt\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"name\":\"setFeeConfig\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b50604051610618380380610618833981810160405281019061003191906100d4565b805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506100ff565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a38261007a565b9050919050565b6100b381610099565b81146100bd575f5ffd5b50565b5f815190506100ce816100aa565b92915050565b5f602082840312156100e9576100e8610076565b5b5f6100f6848285016100c0565b91505092915050565b61050c8061010c5f395ff3fe608060405234801561000f575f5ffd5b506004361061003f575f3560e01c80635fbbc0d2146100435780638f10b586146100685780639e05549a14610084575b5f5ffd5b61004b6100a2565b60405161005f98979695949392919061029b565b60405180910390f35b610082600480360381019061007d9190610345565b610152565b005b61008c6101f0565b60405161009991906103f6565b60405180910390f35b5f5f5f5f5f5f5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16635fbbc0d26040518163ffffffff1660e01b815260040161010060405180830381865afa158015610114573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906101389190610423565b975097509750975097509750975097509091929394959697565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638f10b58689898989898989896040518963ffffffff1660e01b81526004016101b998979695949392919061029b565b5f604051808303815f87803b1580156101d0575f5ffd5b505af11580156101e2573d5f5f3e3d5ffd5b505050505050505050505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16639e05549a6040518163ffffffff1660e01b8152600401602060405180830381865afa15801561025a573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061027e91906104d4565b905090565b5f819050919050565b61029581610283565b82525050565b5f610100820190506102af5f83018b61028c565b6102bc602083018a61028c565b6102c9604083018961028c565b6102d6606083018861028c565b6102e3608083018761028c565b6102f060a083018661028c565b6102fd60c083018561028c565b61030a60e083018461028c565b9998505050505050505050565b5f5ffd5b61032481610283565b811461032e575f5ffd5b50565b5f8135905061033f8161031b565b92915050565b5f5f5f5f5f5f5f5f610100898b03121561036257610361610317565b5b5f61036f8b828c01610331565b98505060206103808b828c01610331565b97505060406103918b828c01610331565b96505060606103a28b828c01610331565b95505060806103b38b828c01610331565b94505060a06103c48b828c01610331565b93505060c06103d58b828c01610331565b92505060e06103e68b828c01610331565b9150509295985092959890939650565b5f6020820190506104095f83018461028c565b92915050565b5f8151905061041d8161031b565b92915050565b5f5f5f5f5f5f5f5f610100898b0312156104405761043f610317565b5b5f61044d8b828c0161040f565b985050602061045e8b828c0161040f565b975050604061046f8b828c0161040f565b96505060606104808b828c0161040f565b95505060806104918b828c0161040f565b94505060a06104a28b828c0161040f565b93505060c06104b38b828c0161040f565b92505060e06104c48b828c0161040f565b9150509295985092959890939650565b5f602082840312156104e9576104e8610317565b5b5f6104f68482850161040f565b9150509291505056fea164736f6c634300081e000a",
}

// FeeManagerTestABI is the input ABI used to generate the binding from.
// Deprecated: Use FeeManagerTestMetaData.ABI instead.
var FeeManagerTestABI = FeeManagerTestMetaData.ABI

// FeeManagerTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use FeeManagerTestMetaData.Bin instead.
var FeeManagerTestBin = FeeManagerTestMetaData.Bin

// DeployFeeManagerTest deploys a new Ethereum contract, binding an instance of FeeManagerTest to it.
func DeployFeeManagerTest(auth *bind.TransactOpts, backend bind.ContractBackend, feeManagerPrecompile common.Address) (common.Address, *types.Transaction, *FeeManagerTest, error) {
	parsed, err := FeeManagerTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(FeeManagerTestBin), backend, feeManagerPrecompile)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &FeeManagerTest{FeeManagerTestCaller: FeeManagerTestCaller{contract: contract}, FeeManagerTestTransactor: FeeManagerTestTransactor{contract: contract}, FeeManagerTestFilterer: FeeManagerTestFilterer{contract: contract}}, nil
}

// FeeManagerTest is an auto generated Go binding around an Ethereum contract.
type FeeManagerTest struct {
	FeeManagerTestCaller     // Read-only binding to the contract
	FeeManagerTestTransactor // Write-only binding to the contract
	FeeManagerTestFilterer   // Log filterer for contract events
}

// FeeManagerTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type FeeManagerTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeeManagerTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FeeManagerTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeeManagerTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FeeManagerTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeeManagerTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FeeManagerTestSession struct {
	Contract     *FeeManagerTest   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// FeeManagerTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FeeManagerTestCallerSession struct {
	Contract *FeeManagerTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// FeeManagerTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FeeManagerTestTransactorSession struct {
	Contract     *FeeManagerTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// FeeManagerTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type FeeManagerTestRaw struct {
	Contract *FeeManagerTest // Generic contract binding to access the raw methods on
}

// FeeManagerTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FeeManagerTestCallerRaw struct {
	Contract *FeeManagerTestCaller // Generic read-only contract binding to access the raw methods on
}

// FeeManagerTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FeeManagerTestTransactorRaw struct {
	Contract *FeeManagerTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFeeManagerTest creates a new instance of FeeManagerTest, bound to a specific deployed contract.
func NewFeeManagerTest(address common.Address, backend bind.ContractBackend) (*FeeManagerTest, error) {
	contract, err := bindFeeManagerTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FeeManagerTest{FeeManagerTestCaller: FeeManagerTestCaller{contract: contract}, FeeManagerTestTransactor: FeeManagerTestTransactor{contract: contract}, FeeManagerTestFilterer: FeeManagerTestFilterer{contract: contract}}, nil
}

// NewFeeManagerTestCaller creates a new read-only instance of FeeManagerTest, bound to a specific deployed contract.
func NewFeeManagerTestCaller(address common.Address, caller bind.ContractCaller) (*FeeManagerTestCaller, error) {
	contract, err := bindFeeManagerTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FeeManagerTestCaller{contract: contract}, nil
}

// NewFeeManagerTestTransactor creates a new write-only instance of FeeManagerTest, bound to a specific deployed contract.
func NewFeeManagerTestTransactor(address common.Address, transactor bind.ContractTransactor) (*FeeManagerTestTransactor, error) {
	contract, err := bindFeeManagerTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FeeManagerTestTransactor{contract: contract}, nil
}

// NewFeeManagerTestFilterer creates a new log filterer instance of FeeManagerTest, bound to a specific deployed contract.
func NewFeeManagerTestFilterer(address common.Address, filterer bind.ContractFilterer) (*FeeManagerTestFilterer, error) {
	contract, err := bindFeeManagerTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FeeManagerTestFilterer{contract: contract}, nil
}

// bindFeeManagerTest binds a generic wrapper to an already deployed contract.
func bindFeeManagerTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FeeManagerTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FeeManagerTest *FeeManagerTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FeeManagerTest.Contract.FeeManagerTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FeeManagerTest *FeeManagerTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FeeManagerTest.Contract.FeeManagerTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FeeManagerTest *FeeManagerTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FeeManagerTest.Contract.FeeManagerTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FeeManagerTest *FeeManagerTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FeeManagerTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FeeManagerTest *FeeManagerTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FeeManagerTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FeeManagerTest *FeeManagerTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FeeManagerTest.Contract.contract.Transact(opts, method, params...)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep)
func (_FeeManagerTest *FeeManagerTestCaller) GetFeeConfig(opts *bind.CallOpts) (struct {
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
	err := _FeeManagerTest.contract.Call(opts, &out, "getFeeConfig")

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
func (_FeeManagerTest *FeeManagerTestSession) GetFeeConfig() (struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}, error) {
	return _FeeManagerTest.Contract.GetFeeConfig(&_FeeManagerTest.CallOpts)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep)
func (_FeeManagerTest *FeeManagerTestCallerSession) GetFeeConfig() (struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}, error) {
	return _FeeManagerTest.Contract.GetFeeConfig(&_FeeManagerTest.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_FeeManagerTest *FeeManagerTestCaller) GetFeeConfigLastChangedAt(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _FeeManagerTest.contract.Call(opts, &out, "getFeeConfigLastChangedAt")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_FeeManagerTest *FeeManagerTestSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _FeeManagerTest.Contract.GetFeeConfigLastChangedAt(&_FeeManagerTest.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_FeeManagerTest *FeeManagerTestCallerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _FeeManagerTest.Contract.GetFeeConfigLastChangedAt(&_FeeManagerTest.CallOpts)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x8f10b586.
//
// Solidity: function setFeeConfig(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep) returns()
func (_FeeManagerTest *FeeManagerTestTransactor) SetFeeConfig(opts *bind.TransactOpts, gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error) {
	return _FeeManagerTest.contract.Transact(opts, "setFeeConfig", gasLimit, targetBlockRate, minBaseFee, targetGas, baseFeeChangeDenominator, minBlockGasCost, maxBlockGasCost, blockGasCostStep)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x8f10b586.
//
// Solidity: function setFeeConfig(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep) returns()
func (_FeeManagerTest *FeeManagerTestSession) SetFeeConfig(gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error) {
	return _FeeManagerTest.Contract.SetFeeConfig(&_FeeManagerTest.TransactOpts, gasLimit, targetBlockRate, minBaseFee, targetGas, baseFeeChangeDenominator, minBlockGasCost, maxBlockGasCost, blockGasCostStep)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x8f10b586.
//
// Solidity: function setFeeConfig(uint256 gasLimit, uint256 targetBlockRate, uint256 minBaseFee, uint256 targetGas, uint256 baseFeeChangeDenominator, uint256 minBlockGasCost, uint256 maxBlockGasCost, uint256 blockGasCostStep) returns()
func (_FeeManagerTest *FeeManagerTestTransactorSession) SetFeeConfig(gasLimit *big.Int, targetBlockRate *big.Int, minBaseFee *big.Int, targetGas *big.Int, baseFeeChangeDenominator *big.Int, minBlockGasCost *big.Int, maxBlockGasCost *big.Int, blockGasCostStep *big.Int) (*types.Transaction, error) {
	return _FeeManagerTest.Contract.SetFeeConfig(&_FeeManagerTest.TransactOpts, gasLimit, targetBlockRate, minBaseFee, targetGas, baseFeeChangeDenominator, minBlockGasCost, maxBlockGasCost, blockGasCostStep)
}
