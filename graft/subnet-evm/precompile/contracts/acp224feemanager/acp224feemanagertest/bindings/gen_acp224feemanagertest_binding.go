// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
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


// ACP224FeeManagerTestMetaData contains all meta data concerning the ACP224FeeManagerTest contract.
var ACP224FeeManagerTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"feeManagerPrecompile\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"getFeeConfig\",\"outputs\":[{\"components\":[{\"internalType\":\"bool\",\"name\":\"validatorTargetGas\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"targetGas\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"staticPricing\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"minGasPrice\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"timeToDouble\",\"type\":\"uint64\"}],\"internalType\":\"structIACP224FeeManager.FeeConfig\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getFeeConfigLastChangedAt\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bool\",\"name\":\"validatorTargetGas\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"targetGas\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"staticPricing\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"minGasPrice\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"timeToDouble\",\"type\":\"uint64\"}],\"internalType\":\"structIACP224FeeManager.FeeConfig\",\"name\":\"config\",\"type\":\"tuple\"}],\"name\":\"setFeeConfig\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b506040516107e03803806107e0833981810160405281019061003191906100d4565b805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506100ff565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a38261007a565b9050919050565b6100b381610099565b81146100bd575f5ffd5b50565b5f815190506100ce816100aa565b92915050565b5f602082840312156100e9576100e8610076565b5b5f6100f6848285016100c0565b91505092915050565b6106d48061010c5f395ff3fe608060405234801561000f575f5ffd5b506004361061003f575f3560e01c80635fbbc0d21461004357806398d5aae4146100615780639e05549a1461007d575b5f5ffd5b61004b61009b565b604051610058919061033f565b60405180910390f35b61007b60048036038101906100769190610387565b610135565b005b6100856101be565b60405161009291906103ca565b60405180910390f35b6100a3610251565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16635fbbc0d26040518163ffffffff1660e01b815260040160a060405180830381865afa15801561010c573d5f5f3e3d5ffd5b505050506040513d601f19601f82011682018060405250810190610130919061054c565b905090565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166398d5aae4826040518263ffffffff1660e01b815260040161018e9190610659565b5f604051808303815f87803b1580156101a5575f5ffd5b505af11580156101b7573d5f5f3e3d5ffd5b5050505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16639e05549a6040518163ffffffff1660e01b8152600401602060405180830381865afa158015610228573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061024c919061069c565b905090565b6040518060a001604052805f151581526020015f67ffffffffffffffff1681526020015f151581526020015f67ffffffffffffffff1681526020015f67ffffffffffffffff1681525090565b5f8115159050919050565b6102b18161029d565b82525050565b5f67ffffffffffffffff82169050919050565b6102d3816102b7565b82525050565b60a082015f8201516102ed5f8501826102a8565b50602082015161030060208501826102ca565b50604082015161031360408501826102a8565b50606082015161032660608501826102ca565b50608082015161033960808501826102ca565b50505050565b5f60a0820190506103525f8301846102d9565b92915050565b5f604051905090565b5f5ffd5b5f5ffd5b5f60a0828403121561037e5761037d610365565b5b81905092915050565b5f60a0828403121561039c5761039b610361565b5b5f6103a984828501610369565b91505092915050565b5f819050919050565b6103c4816103b2565b82525050565b5f6020820190506103dd5f8301846103bb565b92915050565b5f5ffd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b61042d826103e7565b810181811067ffffffffffffffff8211171561044c5761044b6103f7565b5b80604052505050565b5f61045e610358565b905061046a8282610424565b919050565b6104788161029d565b8114610482575f5ffd5b50565b5f815190506104938161046f565b92915050565b6104a2816102b7565b81146104ac575f5ffd5b50565b5f815190506104bd81610499565b92915050565b5f60a082840312156104d8576104d76103e3565b5b6104e260a0610455565b90505f6104f184828501610485565b5f830152506020610504848285016104af565b602083015250604061051884828501610485565b604083015250606061052c848285016104af565b6060830152506080610540848285016104af565b60808301525092915050565b5f60a0828403121561056157610560610361565b5b5f61056e848285016104c3565b91505092915050565b5f813590506105858161046f565b92915050565b5f6105996020840184610577565b905092915050565b5f813590506105af81610499565b92915050565b5f6105c360208401846105a1565b905092915050565b60a082016105db5f83018361058b565b6105e75f8501826102a8565b506105f560208301836105b5565b61060260208501826102ca565b50610610604083018361058b565b61061d60408501826102a8565b5061062b60608301836105b5565b61063860608501826102ca565b5061064660808301836105b5565b61065360808501826102ca565b50505050565b5f60a08201905061066c5f8301846105cb565b92915050565b61067b816103b2565b8114610685575f5ffd5b50565b5f8151905061069681610672565b92915050565b5f602082840312156106b1576106b0610361565b5b5f6106be84828501610688565b9150509291505056fea164736f6c634300081c000a",
}

// ACP224FeeManagerTestABI is the input ABI used to generate the binding from.
// Deprecated: Use ACP224FeeManagerTestMetaData.ABI instead.
var ACP224FeeManagerTestABI = ACP224FeeManagerTestMetaData.ABI

// ACP224FeeManagerTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ACP224FeeManagerTestMetaData.Bin instead.
var ACP224FeeManagerTestBin = ACP224FeeManagerTestMetaData.Bin

// DeployACP224FeeManagerTest deploys a new Ethereum contract, binding an instance of ACP224FeeManagerTest to it.
func DeployACP224FeeManagerTest(auth *bind.TransactOpts, backend bind.ContractBackend, feeManagerPrecompile common.Address) (common.Address, *types.Transaction, *ACP224FeeManagerTest, error) {
	parsed, err := ACP224FeeManagerTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ACP224FeeManagerTestBin), backend, feeManagerPrecompile)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ACP224FeeManagerTest{ACP224FeeManagerTestCaller: ACP224FeeManagerTestCaller{contract: contract}, ACP224FeeManagerTestTransactor: ACP224FeeManagerTestTransactor{contract: contract}, ACP224FeeManagerTestFilterer: ACP224FeeManagerTestFilterer{contract: contract}}, nil
}

// ACP224FeeManagerTest is an auto generated Go binding around an Ethereum contract.
type ACP224FeeManagerTest struct {
	ACP224FeeManagerTestCaller     // Read-only binding to the contract
	ACP224FeeManagerTestTransactor // Write-only binding to the contract
	ACP224FeeManagerTestFilterer   // Log filterer for contract events
}

// ACP224FeeManagerTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type ACP224FeeManagerTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ACP224FeeManagerTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ACP224FeeManagerTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ACP224FeeManagerTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ACP224FeeManagerTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ACP224FeeManagerTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ACP224FeeManagerTestSession struct {
	Contract     *ACP224FeeManagerTest // Generic contract binding to set the session for
	CallOpts     bind.CallOpts         // Call options to use throughout this session
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// ACP224FeeManagerTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ACP224FeeManagerTestCallerSession struct {
	Contract *ACP224FeeManagerTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts               // Call options to use throughout this session
}

// ACP224FeeManagerTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ACP224FeeManagerTestTransactorSession struct {
	Contract     *ACP224FeeManagerTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts               // Transaction auth options to use throughout this session
}

// ACP224FeeManagerTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type ACP224FeeManagerTestRaw struct {
	Contract *ACP224FeeManagerTest // Generic contract binding to access the raw methods on
}

// ACP224FeeManagerTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ACP224FeeManagerTestCallerRaw struct {
	Contract *ACP224FeeManagerTestCaller // Generic read-only contract binding to access the raw methods on
}

// ACP224FeeManagerTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ACP224FeeManagerTestTransactorRaw struct {
	Contract *ACP224FeeManagerTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewACP224FeeManagerTest creates a new instance of ACP224FeeManagerTest, bound to a specific deployed contract.
func NewACP224FeeManagerTest(address common.Address, backend bind.ContractBackend) (*ACP224FeeManagerTest, error) {
	contract, err := bindACP224FeeManagerTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ACP224FeeManagerTest{ACP224FeeManagerTestCaller: ACP224FeeManagerTestCaller{contract: contract}, ACP224FeeManagerTestTransactor: ACP224FeeManagerTestTransactor{contract: contract}, ACP224FeeManagerTestFilterer: ACP224FeeManagerTestFilterer{contract: contract}}, nil
}

// NewACP224FeeManagerTestCaller creates a new read-only instance of ACP224FeeManagerTest, bound to a specific deployed contract.
func NewACP224FeeManagerTestCaller(address common.Address, caller bind.ContractCaller) (*ACP224FeeManagerTestCaller, error) {
	contract, err := bindACP224FeeManagerTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ACP224FeeManagerTestCaller{contract: contract}, nil
}

// NewACP224FeeManagerTestTransactor creates a new write-only instance of ACP224FeeManagerTest, bound to a specific deployed contract.
func NewACP224FeeManagerTestTransactor(address common.Address, transactor bind.ContractTransactor) (*ACP224FeeManagerTestTransactor, error) {
	contract, err := bindACP224FeeManagerTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ACP224FeeManagerTestTransactor{contract: contract}, nil
}

// NewACP224FeeManagerTestFilterer creates a new log filterer instance of ACP224FeeManagerTest, bound to a specific deployed contract.
func NewACP224FeeManagerTestFilterer(address common.Address, filterer bind.ContractFilterer) (*ACP224FeeManagerTestFilterer, error) {
	contract, err := bindACP224FeeManagerTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ACP224FeeManagerTestFilterer{contract: contract}, nil
}

// bindACP224FeeManagerTest binds a generic wrapper to an already deployed contract.
func bindACP224FeeManagerTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ACP224FeeManagerTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ACP224FeeManagerTest *ACP224FeeManagerTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ACP224FeeManagerTest.Contract.ACP224FeeManagerTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ACP224FeeManagerTest *ACP224FeeManagerTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.Contract.ACP224FeeManagerTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ACP224FeeManagerTest *ACP224FeeManagerTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.Contract.ACP224FeeManagerTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ACP224FeeManagerTest *ACP224FeeManagerTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ACP224FeeManagerTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ACP224FeeManagerTest *ACP224FeeManagerTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ACP224FeeManagerTest *ACP224FeeManagerTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.Contract.contract.Transact(opts, method, params...)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns((bool,uint64,bool,uint64,uint64))
func (_ACP224FeeManagerTest *ACP224FeeManagerTestCaller) GetFeeConfig(opts *bind.CallOpts) (IACP224FeeManagerFeeConfig, error) {
	var out []interface{}
	err := _ACP224FeeManagerTest.contract.Call(opts, &out, "getFeeConfig")

	if err != nil {
		return *new(IACP224FeeManagerFeeConfig), err
	}

	out0 := *abi.ConvertType(out[0], new(IACP224FeeManagerFeeConfig)).(*IACP224FeeManagerFeeConfig)

	return out0, err

}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns((bool,uint64,bool,uint64,uint64))
func (_ACP224FeeManagerTest *ACP224FeeManagerTestSession) GetFeeConfig() (IACP224FeeManagerFeeConfig, error) {
	return _ACP224FeeManagerTest.Contract.GetFeeConfig(&_ACP224FeeManagerTest.CallOpts)
}

// GetFeeConfig is a free data retrieval call binding the contract method 0x5fbbc0d2.
//
// Solidity: function getFeeConfig() view returns((bool,uint64,bool,uint64,uint64))
func (_ACP224FeeManagerTest *ACP224FeeManagerTestCallerSession) GetFeeConfig() (IACP224FeeManagerFeeConfig, error) {
	return _ACP224FeeManagerTest.Contract.GetFeeConfig(&_ACP224FeeManagerTest.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_ACP224FeeManagerTest *ACP224FeeManagerTestCaller) GetFeeConfigLastChangedAt(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ACP224FeeManagerTest.contract.Call(opts, &out, "getFeeConfigLastChangedAt")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_ACP224FeeManagerTest *ACP224FeeManagerTestSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _ACP224FeeManagerTest.Contract.GetFeeConfigLastChangedAt(&_ACP224FeeManagerTest.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_ACP224FeeManagerTest *ACP224FeeManagerTestCallerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _ACP224FeeManagerTest.Contract.GetFeeConfigLastChangedAt(&_ACP224FeeManagerTest.CallOpts)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x98d5aae4.
//
// Solidity: function setFeeConfig((bool,uint64,bool,uint64,uint64) config) returns()
func (_ACP224FeeManagerTest *ACP224FeeManagerTestTransactor) SetFeeConfig(opts *bind.TransactOpts, config IACP224FeeManagerFeeConfig) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.contract.Transact(opts, "setFeeConfig", config)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x98d5aae4.
//
// Solidity: function setFeeConfig((bool,uint64,bool,uint64,uint64) config) returns()
func (_ACP224FeeManagerTest *ACP224FeeManagerTestSession) SetFeeConfig(config IACP224FeeManagerFeeConfig) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.Contract.SetFeeConfig(&_ACP224FeeManagerTest.TransactOpts, config)
}

// SetFeeConfig is a paid mutator transaction binding the contract method 0x98d5aae4.
//
// Solidity: function setFeeConfig((bool,uint64,bool,uint64,uint64) config) returns()
func (_ACP224FeeManagerTest *ACP224FeeManagerTestTransactorSession) SetFeeConfig(config IACP224FeeManagerFeeConfig) (*types.Transaction, error) {
	return _ACP224FeeManagerTest.Contract.SetFeeConfig(&_ACP224FeeManagerTest.TransactOpts, config)
}
