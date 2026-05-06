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

// GasPriceManagerTestMetaData contains all meta data concerning the GasPriceManagerTest contract.
var GasPriceManagerTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"gasPriceManagerPrecompile\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"getGasPriceConfig\",\"outputs\":[{\"components\":[{\"internalType\":\"bool\",\"name\":\"validatorTargetGas\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"targetGas\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"staticPricing\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"minGasPrice\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"timeToDouble\",\"type\":\"uint64\"}],\"internalType\":\"structIGasPriceManager.GasPriceConfig\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getGasPriceConfigLastChangedAt\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bool\",\"name\":\"validatorTargetGas\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"targetGas\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"staticPricing\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"minGasPrice\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"timeToDouble\",\"type\":\"uint64\"}],\"internalType\":\"structIGasPriceManager.GasPriceConfig\",\"name\":\"config\",\"type\":\"tuple\"}],\"name\":\"setGasPriceConfig\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b506040516107e03803806107e0833981810160405281019061003191906100d4565b805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506100ff565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a38261007a565b9050919050565b6100b381610099565b81146100bd575f5ffd5b50565b5f815190506100ce816100aa565b92915050565b5f602082840312156100e9576100e8610076565b5b5f6100f6848285016100c0565b91505092915050565b6106d48061010c5f395ff3fe608060405234801561000f575f5ffd5b506004361061003f575f3560e01c8063445821e31461004357806365c97a2814610061578063eb8df3961461007d575b5f5ffd5b61004b61009b565b604051610058919061033f565b60405180910390f35b61007b60048036038101906100769190610387565b610135565b005b6100856101be565b60405161009291906103ca565b60405180910390f35b6100a3610251565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663445821e36040518163ffffffff1660e01b815260040160a060405180830381865afa15801561010c573d5f5f3e3d5ffd5b505050506040513d601f19601f82011682018060405250810190610130919061054c565b905090565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166365c97a28826040518263ffffffff1660e01b815260040161018e9190610659565b5f604051808303815f87803b1580156101a5575f5ffd5b505af11580156101b7573d5f5f3e3d5ffd5b5050505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb8df3966040518163ffffffff1660e01b8152600401602060405180830381865afa158015610228573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061024c919061069c565b905090565b6040518060a001604052805f151581526020015f67ffffffffffffffff1681526020015f151581526020015f67ffffffffffffffff1681526020015f67ffffffffffffffff1681525090565b5f8115159050919050565b6102b18161029d565b82525050565b5f67ffffffffffffffff82169050919050565b6102d3816102b7565b82525050565b60a082015f8201516102ed5f8501826102a8565b50602082015161030060208501826102ca565b50604082015161031360408501826102a8565b50606082015161032660608501826102ca565b50608082015161033960808501826102ca565b50505050565b5f60a0820190506103525f8301846102d9565b92915050565b5f604051905090565b5f5ffd5b5f5ffd5b5f60a0828403121561037e5761037d610365565b5b81905092915050565b5f60a0828403121561039c5761039b610361565b5b5f6103a984828501610369565b91505092915050565b5f819050919050565b6103c4816103b2565b82525050565b5f6020820190506103dd5f8301846103bb565b92915050565b5f5ffd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b61042d826103e7565b810181811067ffffffffffffffff8211171561044c5761044b6103f7565b5b80604052505050565b5f61045e610358565b905061046a8282610424565b919050565b6104788161029d565b8114610482575f5ffd5b50565b5f815190506104938161046f565b92915050565b6104a2816102b7565b81146104ac575f5ffd5b50565b5f815190506104bd81610499565b92915050565b5f60a082840312156104d8576104d76103e3565b5b6104e260a0610455565b90505f6104f184828501610485565b5f830152506020610504848285016104af565b602083015250604061051884828501610485565b604083015250606061052c848285016104af565b6060830152506080610540848285016104af565b60808301525092915050565b5f60a0828403121561056157610560610361565b5b5f61056e848285016104c3565b91505092915050565b5f813590506105858161046f565b92915050565b5f6105996020840184610577565b905092915050565b5f813590506105af81610499565b92915050565b5f6105c360208401846105a1565b905092915050565b60a082016105db5f83018361058b565b6105e75f8501826102a8565b506105f560208301836105b5565b61060260208501826102ca565b50610610604083018361058b565b61061d60408501826102a8565b5061062b60608301836105b5565b61063860608501826102ca565b5061064660808301836105b5565b61065360808501826102ca565b50505050565b5f60a08201905061066c5f8301846105cb565b92915050565b61067b816103b2565b8114610685575f5ffd5b50565b5f8151905061069681610672565b92915050565b5f602082840312156106b1576106b0610361565b5b5f6106be84828501610688565b9150509291505056fea164736f6c634300081c000a",
}

// GasPriceManagerTestABI is the input ABI used to generate the binding from.
// Deprecated: Use GasPriceManagerTestMetaData.ABI instead.
var GasPriceManagerTestABI = GasPriceManagerTestMetaData.ABI

// GasPriceManagerTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use GasPriceManagerTestMetaData.Bin instead.
var GasPriceManagerTestBin = GasPriceManagerTestMetaData.Bin

// DeployGasPriceManagerTest deploys a new Ethereum contract, binding an instance of GasPriceManagerTest to it.
func DeployGasPriceManagerTest(auth *bind.TransactOpts, backend bind.ContractBackend, gasPriceManagerPrecompile common.Address) (common.Address, *types.Transaction, *GasPriceManagerTest, error) {
	parsed, err := GasPriceManagerTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(GasPriceManagerTestBin), backend, gasPriceManagerPrecompile)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &GasPriceManagerTest{GasPriceManagerTestCaller: GasPriceManagerTestCaller{contract: contract}, GasPriceManagerTestTransactor: GasPriceManagerTestTransactor{contract: contract}, GasPriceManagerTestFilterer: GasPriceManagerTestFilterer{contract: contract}}, nil
}

// GasPriceManagerTest is an auto generated Go binding around an Ethereum contract.
type GasPriceManagerTest struct {
	GasPriceManagerTestCaller     // Read-only binding to the contract
	GasPriceManagerTestTransactor // Write-only binding to the contract
	GasPriceManagerTestFilterer   // Log filterer for contract events
}

// GasPriceManagerTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type GasPriceManagerTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GasPriceManagerTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type GasPriceManagerTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GasPriceManagerTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type GasPriceManagerTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GasPriceManagerTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type GasPriceManagerTestSession struct {
	Contract     *GasPriceManagerTest // Generic contract binding to set the session for
	CallOpts     bind.CallOpts        // Call options to use throughout this session
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// GasPriceManagerTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type GasPriceManagerTestCallerSession struct {
	Contract *GasPriceManagerTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts              // Call options to use throughout this session
}

// GasPriceManagerTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type GasPriceManagerTestTransactorSession struct {
	Contract     *GasPriceManagerTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts              // Transaction auth options to use throughout this session
}

// GasPriceManagerTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type GasPriceManagerTestRaw struct {
	Contract *GasPriceManagerTest // Generic contract binding to access the raw methods on
}

// GasPriceManagerTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type GasPriceManagerTestCallerRaw struct {
	Contract *GasPriceManagerTestCaller // Generic read-only contract binding to access the raw methods on
}

// GasPriceManagerTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type GasPriceManagerTestTransactorRaw struct {
	Contract *GasPriceManagerTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewGasPriceManagerTest creates a new instance of GasPriceManagerTest, bound to a specific deployed contract.
func NewGasPriceManagerTest(address common.Address, backend bind.ContractBackend) (*GasPriceManagerTest, error) {
	contract, err := bindGasPriceManagerTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &GasPriceManagerTest{GasPriceManagerTestCaller: GasPriceManagerTestCaller{contract: contract}, GasPriceManagerTestTransactor: GasPriceManagerTestTransactor{contract: contract}, GasPriceManagerTestFilterer: GasPriceManagerTestFilterer{contract: contract}}, nil
}

// NewGasPriceManagerTestCaller creates a new read-only instance of GasPriceManagerTest, bound to a specific deployed contract.
func NewGasPriceManagerTestCaller(address common.Address, caller bind.ContractCaller) (*GasPriceManagerTestCaller, error) {
	contract, err := bindGasPriceManagerTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &GasPriceManagerTestCaller{contract: contract}, nil
}

// NewGasPriceManagerTestTransactor creates a new write-only instance of GasPriceManagerTest, bound to a specific deployed contract.
func NewGasPriceManagerTestTransactor(address common.Address, transactor bind.ContractTransactor) (*GasPriceManagerTestTransactor, error) {
	contract, err := bindGasPriceManagerTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &GasPriceManagerTestTransactor{contract: contract}, nil
}

// NewGasPriceManagerTestFilterer creates a new log filterer instance of GasPriceManagerTest, bound to a specific deployed contract.
func NewGasPriceManagerTestFilterer(address common.Address, filterer bind.ContractFilterer) (*GasPriceManagerTestFilterer, error) {
	contract, err := bindGasPriceManagerTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &GasPriceManagerTestFilterer{contract: contract}, nil
}

// bindGasPriceManagerTest binds a generic wrapper to an already deployed contract.
func bindGasPriceManagerTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := GasPriceManagerTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_GasPriceManagerTest *GasPriceManagerTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _GasPriceManagerTest.Contract.GasPriceManagerTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_GasPriceManagerTest *GasPriceManagerTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _GasPriceManagerTest.Contract.GasPriceManagerTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_GasPriceManagerTest *GasPriceManagerTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _GasPriceManagerTest.Contract.GasPriceManagerTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_GasPriceManagerTest *GasPriceManagerTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _GasPriceManagerTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_GasPriceManagerTest *GasPriceManagerTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _GasPriceManagerTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_GasPriceManagerTest *GasPriceManagerTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _GasPriceManagerTest.Contract.contract.Transact(opts, method, params...)
}

// GetGasPriceConfig is a free data retrieval call binding the contract method 0x445821e3.
//
// Solidity: function getGasPriceConfig() view returns((bool,uint64,bool,uint64,uint64))
func (_GasPriceManagerTest *GasPriceManagerTestCaller) GetGasPriceConfig(opts *bind.CallOpts) (IGasPriceManagerGasPriceConfig, error) {
	var out []interface{}
	err := _GasPriceManagerTest.contract.Call(opts, &out, "getGasPriceConfig")

	if err != nil {
		return *new(IGasPriceManagerGasPriceConfig), err
	}

	out0 := *abi.ConvertType(out[0], new(IGasPriceManagerGasPriceConfig)).(*IGasPriceManagerGasPriceConfig)

	return out0, err

}

// GetGasPriceConfig is a free data retrieval call binding the contract method 0x445821e3.
//
// Solidity: function getGasPriceConfig() view returns((bool,uint64,bool,uint64,uint64))
func (_GasPriceManagerTest *GasPriceManagerTestSession) GetGasPriceConfig() (IGasPriceManagerGasPriceConfig, error) {
	return _GasPriceManagerTest.Contract.GetGasPriceConfig(&_GasPriceManagerTest.CallOpts)
}

// GetGasPriceConfig is a free data retrieval call binding the contract method 0x445821e3.
//
// Solidity: function getGasPriceConfig() view returns((bool,uint64,bool,uint64,uint64))
func (_GasPriceManagerTest *GasPriceManagerTestCallerSession) GetGasPriceConfig() (IGasPriceManagerGasPriceConfig, error) {
	return _GasPriceManagerTest.Contract.GetGasPriceConfig(&_GasPriceManagerTest.CallOpts)
}

// GetGasPriceConfigLastChangedAt is a free data retrieval call binding the contract method 0xeb8df396.
//
// Solidity: function getGasPriceConfigLastChangedAt() view returns(uint256)
func (_GasPriceManagerTest *GasPriceManagerTestCaller) GetGasPriceConfigLastChangedAt(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _GasPriceManagerTest.contract.Call(opts, &out, "getGasPriceConfigLastChangedAt")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetGasPriceConfigLastChangedAt is a free data retrieval call binding the contract method 0xeb8df396.
//
// Solidity: function getGasPriceConfigLastChangedAt() view returns(uint256)
func (_GasPriceManagerTest *GasPriceManagerTestSession) GetGasPriceConfigLastChangedAt() (*big.Int, error) {
	return _GasPriceManagerTest.Contract.GetGasPriceConfigLastChangedAt(&_GasPriceManagerTest.CallOpts)
}

// GetGasPriceConfigLastChangedAt is a free data retrieval call binding the contract method 0xeb8df396.
//
// Solidity: function getGasPriceConfigLastChangedAt() view returns(uint256)
func (_GasPriceManagerTest *GasPriceManagerTestCallerSession) GetGasPriceConfigLastChangedAt() (*big.Int, error) {
	return _GasPriceManagerTest.Contract.GetGasPriceConfigLastChangedAt(&_GasPriceManagerTest.CallOpts)
}

// SetGasPriceConfig is a paid mutator transaction binding the contract method 0x65c97a28.
//
// Solidity: function setGasPriceConfig((bool,uint64,bool,uint64,uint64) config) returns()
func (_GasPriceManagerTest *GasPriceManagerTestTransactor) SetGasPriceConfig(opts *bind.TransactOpts, config IGasPriceManagerGasPriceConfig) (*types.Transaction, error) {
	return _GasPriceManagerTest.contract.Transact(opts, "setGasPriceConfig", config)
}

// SetGasPriceConfig is a paid mutator transaction binding the contract method 0x65c97a28.
//
// Solidity: function setGasPriceConfig((bool,uint64,bool,uint64,uint64) config) returns()
func (_GasPriceManagerTest *GasPriceManagerTestSession) SetGasPriceConfig(config IGasPriceManagerGasPriceConfig) (*types.Transaction, error) {
	return _GasPriceManagerTest.Contract.SetGasPriceConfig(&_GasPriceManagerTest.TransactOpts, config)
}

// SetGasPriceConfig is a paid mutator transaction binding the contract method 0x65c97a28.
//
// Solidity: function setGasPriceConfig((bool,uint64,bool,uint64,uint64) config) returns()
func (_GasPriceManagerTest *GasPriceManagerTestTransactorSession) SetGasPriceConfig(config IGasPriceManagerGasPriceConfig) (*types.Transaction, error) {
	return _GasPriceManagerTest.Contract.SetGasPriceConfig(&_GasPriceManagerTest.TransactOpts, config)
}
