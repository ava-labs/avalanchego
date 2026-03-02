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

// WarpBlockHash is an auto generated low-level Go binding around an user-defined struct.
type WarpBlockHash struct {
	SourceChainID [32]byte
	BlockHash     [32]byte
}

// WarpMessage is an auto generated low-level Go binding around an user-defined struct.
type WarpMessage struct {
	SourceChainID       [32]byte
	OriginSenderAddress common.Address
	Payload             []byte
}

// WarpTestMetaData contains all meta data concerning the WarpTest contract.
var WarpTestMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"warpPrecompile\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"getBlockchainID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"index\",\"type\":\"uint32\"}],\"name\":\"getVerifiedWarpBlockHash\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"sourceChainID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"blockHash\",\"type\":\"bytes32\"}],\"internalType\":\"structWarpBlockHash\",\"name\":\"warpBlockHash\",\"type\":\"tuple\"},{\"internalType\":\"bool\",\"name\":\"valid\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"index\",\"type\":\"uint32\"}],\"name\":\"getVerifiedWarpMessage\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"sourceChainID\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"originSenderAddress\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"}],\"internalType\":\"structWarpMessage\",\"name\":\"message\",\"type\":\"tuple\"},{\"internalType\":\"bool\",\"name\":\"valid\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"}],\"name\":\"sendWarpMessage\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"messageID\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f5ffd5b50604051610bd5380380610bd5833981810160405281019061003191906100d4565b805f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506100ff565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6100a38261007a565b9050919050565b6100b381610099565b81146100bd575f5ffd5b50565b5f815190506100ce816100aa565b92915050565b5f602082840312156100e9576100e8610076565b5b5f6100f6848285016100c0565b91505092915050565b610ac98061010c5f395ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c80634213cf781461004e5780636f8253501461006c578063ce7f59291461009d578063ee5b48eb146100ce575b5f5ffd5b6100566100fe565b60405161006391906103f1565b60405180910390f35b61008660048036038101906100819190610454565b610191565b6040516100949291906105a4565b60405180910390f35b6100b760048036038101906100b29190610454565b61023e565b6040516100c59291906105ff565b60405180910390f35b6100e860048036038101906100e39190610687565b6102e8565b6040516100f591906103f1565b60405180910390f35b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16634213cf786040518163ffffffff1660e01b8152600401602060405180830381865afa158015610168573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061018c91906106fc565b905090565b61019961038c565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16636f825350846040518263ffffffff1660e01b81526004016101f39190610736565b5f60405180830381865afa15801561020d573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906102359190610942565b91509150915091565b6102466103c1565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663ce7f5929846040518263ffffffff1660e01b81526004016102a09190610736565b606060405180830381865afa1580156102bb573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102df91906109e9565b91509150915091565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663ee5b48eb84846040518363ffffffff1660e01b8152600401610344929190610a71565b6020604051808303815f875af1158015610360573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061038491906106fc565b905092915050565b60405180606001604052805f81526020015f73ffffffffffffffffffffffffffffffffffffffff168152602001606081525090565b60405180604001604052805f81526020015f81525090565b5f819050919050565b6103eb816103d9565b82525050565b5f6020820190506104045f8301846103e2565b92915050565b5f604051905090565b5f5ffd5b5f5ffd5b5f63ffffffff82169050919050565b6104338161041b565b811461043d575f5ffd5b50565b5f8135905061044e8161042a565b92915050565b5f6020828403121561046957610468610413565b5b5f61047684828501610440565b91505092915050565b610488816103d9565b82525050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6104b78261048e565b9050919050565b6104c7816104ad565b82525050565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f61050f826104cd565b61051981856104d7565b93506105298185602086016104e7565b610532816104f5565b840191505092915050565b5f606083015f8301516105525f86018261047f565b50602083015161056560208601826104be565b506040830151848203604086015261057d8282610505565b9150508091505092915050565b5f8115159050919050565b61059e8161058a565b82525050565b5f6040820190508181035f8301526105bc818561053d565b90506105cb6020830184610595565b9392505050565b604082015f8201516105e65f85018261047f565b5060208201516105f9602085018261047f565b50505050565b5f6060820190506106125f8301856105d2565b61061f6040830184610595565b9392505050565b5f5ffd5b5f5ffd5b5f5ffd5b5f5f83601f84011261064757610646610626565b5b8235905067ffffffffffffffff8111156106645761066361062a565b5b6020830191508360018202830111156106805761067f61062e565b5b9250929050565b5f5f6020838503121561069d5761069c610413565b5b5f83013567ffffffffffffffff8111156106ba576106b9610417565b5b6106c685828601610632565b92509250509250929050565b6106db816103d9565b81146106e5575f5ffd5b50565b5f815190506106f6816106d2565b92915050565b5f6020828403121561071157610710610413565b5b5f61071e848285016106e8565b91505092915050565b6107308161041b565b82525050565b5f6020820190506107495f830184610727565b92915050565b5f5ffd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b610789826104f5565b810181811067ffffffffffffffff821117156107a8576107a7610753565b5b80604052505050565b5f6107ba61040a565b90506107c68282610780565b919050565b5f5ffd5b6107d8816104ad565b81146107e2575f5ffd5b50565b5f815190506107f3816107cf565b92915050565b5f5ffd5b5f67ffffffffffffffff82111561081757610816610753565b5b610820826104f5565b9050602081019050919050565b5f61083f61083a846107fd565b6107b1565b90508281526020810184848401111561085b5761085a6107f9565b5b6108668482856104e7565b509392505050565b5f82601f83011261088257610881610626565b5b815161089284826020860161082d565b91505092915050565b5f606082840312156108b0576108af61074f565b5b6108ba60606107b1565b90505f6108c9848285016106e8565b5f8301525060206108dc848285016107e5565b602083015250604082015167ffffffffffffffff811115610900576108ff6107cb565b5b61090c8482850161086e565b60408301525092915050565b6109218161058a565b811461092b575f5ffd5b50565b5f8151905061093c81610918565b92915050565b5f5f6040838503121561095857610957610413565b5b5f83015167ffffffffffffffff81111561097557610974610417565b5b6109818582860161089b565b92505060206109928582860161092e565b9150509250929050565b5f604082840312156109b1576109b061074f565b5b6109bb60406107b1565b90505f6109ca848285016106e8565b5f8301525060206109dd848285016106e8565b60208301525092915050565b5f5f606083850312156109ff576109fe610413565b5b5f610a0c8582860161099c565b9250506040610a1d8582860161092e565b9150509250929050565b5f82825260208201905092915050565b828183375f83830152505050565b5f610a508385610a27565b9350610a5d838584610a37565b610a66836104f5565b840190509392505050565b5f6020820190508181035f830152610a8a818486610a45565b9050939250505056fea2646970667358221220bbb12f336f8c60a3a2cf360e4a0df574af399d8c67b56aac2139d3fea2e50a6664736f6c634300081c0033",
}

// WarpTestABI is the input ABI used to generate the binding from.
// Deprecated: Use WarpTestMetaData.ABI instead.
var WarpTestABI = WarpTestMetaData.ABI

// WarpTestBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use WarpTestMetaData.Bin instead.
var WarpTestBin = WarpTestMetaData.Bin

// DeployWarpTest deploys a new Ethereum contract, binding an instance of WarpTest to it.
func DeployWarpTest(auth *bind.TransactOpts, backend bind.ContractBackend, warpPrecompile common.Address) (common.Address, *types.Transaction, *WarpTest, error) {
	parsed, err := WarpTestMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(WarpTestBin), backend, warpPrecompile)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &WarpTest{WarpTestCaller: WarpTestCaller{contract: contract}, WarpTestTransactor: WarpTestTransactor{contract: contract}, WarpTestFilterer: WarpTestFilterer{contract: contract}}, nil
}

// WarpTest is an auto generated Go binding around an Ethereum contract.
type WarpTest struct {
	WarpTestCaller     // Read-only binding to the contract
	WarpTestTransactor // Write-only binding to the contract
	WarpTestFilterer   // Log filterer for contract events
}

// WarpTestCaller is an auto generated read-only Go binding around an Ethereum contract.
type WarpTestCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// WarpTestTransactor is an auto generated write-only Go binding around an Ethereum contract.
type WarpTestTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// WarpTestFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type WarpTestFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// WarpTestSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type WarpTestSession struct {
	Contract     *WarpTest         // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// WarpTestCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type WarpTestCallerSession struct {
	Contract *WarpTestCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts   // Call options to use throughout this session
}

// WarpTestTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type WarpTestTransactorSession struct {
	Contract     *WarpTestTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// WarpTestRaw is an auto generated low-level Go binding around an Ethereum contract.
type WarpTestRaw struct {
	Contract *WarpTest // Generic contract binding to access the raw methods on
}

// WarpTestCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type WarpTestCallerRaw struct {
	Contract *WarpTestCaller // Generic read-only contract binding to access the raw methods on
}

// WarpTestTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type WarpTestTransactorRaw struct {
	Contract *WarpTestTransactor // Generic write-only contract binding to access the raw methods on
}

// NewWarpTest creates a new instance of WarpTest, bound to a specific deployed contract.
func NewWarpTest(address common.Address, backend bind.ContractBackend) (*WarpTest, error) {
	contract, err := bindWarpTest(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &WarpTest{WarpTestCaller: WarpTestCaller{contract: contract}, WarpTestTransactor: WarpTestTransactor{contract: contract}, WarpTestFilterer: WarpTestFilterer{contract: contract}}, nil
}

// NewWarpTestCaller creates a new read-only instance of WarpTest, bound to a specific deployed contract.
func NewWarpTestCaller(address common.Address, caller bind.ContractCaller) (*WarpTestCaller, error) {
	contract, err := bindWarpTest(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &WarpTestCaller{contract: contract}, nil
}

// NewWarpTestTransactor creates a new write-only instance of WarpTest, bound to a specific deployed contract.
func NewWarpTestTransactor(address common.Address, transactor bind.ContractTransactor) (*WarpTestTransactor, error) {
	contract, err := bindWarpTest(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &WarpTestTransactor{contract: contract}, nil
}

// NewWarpTestFilterer creates a new log filterer instance of WarpTest, bound to a specific deployed contract.
func NewWarpTestFilterer(address common.Address, filterer bind.ContractFilterer) (*WarpTestFilterer, error) {
	contract, err := bindWarpTest(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &WarpTestFilterer{contract: contract}, nil
}

// bindWarpTest binds a generic wrapper to an already deployed contract.
func bindWarpTest(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := WarpTestMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_WarpTest *WarpTestRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _WarpTest.Contract.WarpTestCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_WarpTest *WarpTestRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _WarpTest.Contract.WarpTestTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_WarpTest *WarpTestRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _WarpTest.Contract.WarpTestTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_WarpTest *WarpTestCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _WarpTest.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_WarpTest *WarpTestTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _WarpTest.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_WarpTest *WarpTestTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _WarpTest.Contract.contract.Transact(opts, method, params...)
}

// GetBlockchainID is a free data retrieval call binding the contract method 0x4213cf78.
//
// Solidity: function getBlockchainID() view returns(bytes32)
func (_WarpTest *WarpTestCaller) GetBlockchainID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _WarpTest.contract.Call(opts, &out, "getBlockchainID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetBlockchainID is a free data retrieval call binding the contract method 0x4213cf78.
//
// Solidity: function getBlockchainID() view returns(bytes32)
func (_WarpTest *WarpTestSession) GetBlockchainID() ([32]byte, error) {
	return _WarpTest.Contract.GetBlockchainID(&_WarpTest.CallOpts)
}

// GetBlockchainID is a free data retrieval call binding the contract method 0x4213cf78.
//
// Solidity: function getBlockchainID() view returns(bytes32)
func (_WarpTest *WarpTestCallerSession) GetBlockchainID() ([32]byte, error) {
	return _WarpTest.Contract.GetBlockchainID(&_WarpTest.CallOpts)
}

// GetVerifiedWarpBlockHash is a free data retrieval call binding the contract method 0xce7f5929.
//
// Solidity: function getVerifiedWarpBlockHash(uint32 index) view returns((bytes32,bytes32) warpBlockHash, bool valid)
func (_WarpTest *WarpTestCaller) GetVerifiedWarpBlockHash(opts *bind.CallOpts, index uint32) (struct {
	WarpBlockHash WarpBlockHash
	Valid         bool
}, error) {
	var out []interface{}
	err := _WarpTest.contract.Call(opts, &out, "getVerifiedWarpBlockHash", index)

	outstruct := new(struct {
		WarpBlockHash WarpBlockHash
		Valid         bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.WarpBlockHash = *abi.ConvertType(out[0], new(WarpBlockHash)).(*WarpBlockHash)
	outstruct.Valid = *abi.ConvertType(out[1], new(bool)).(*bool)

	return *outstruct, err

}

// GetVerifiedWarpBlockHash is a free data retrieval call binding the contract method 0xce7f5929.
//
// Solidity: function getVerifiedWarpBlockHash(uint32 index) view returns((bytes32,bytes32) warpBlockHash, bool valid)
func (_WarpTest *WarpTestSession) GetVerifiedWarpBlockHash(index uint32) (struct {
	WarpBlockHash WarpBlockHash
	Valid         bool
}, error) {
	return _WarpTest.Contract.GetVerifiedWarpBlockHash(&_WarpTest.CallOpts, index)
}

// GetVerifiedWarpBlockHash is a free data retrieval call binding the contract method 0xce7f5929.
//
// Solidity: function getVerifiedWarpBlockHash(uint32 index) view returns((bytes32,bytes32) warpBlockHash, bool valid)
func (_WarpTest *WarpTestCallerSession) GetVerifiedWarpBlockHash(index uint32) (struct {
	WarpBlockHash WarpBlockHash
	Valid         bool
}, error) {
	return _WarpTest.Contract.GetVerifiedWarpBlockHash(&_WarpTest.CallOpts, index)
}

// GetVerifiedWarpMessage is a free data retrieval call binding the contract method 0x6f825350.
//
// Solidity: function getVerifiedWarpMessage(uint32 index) view returns((bytes32,address,bytes) message, bool valid)
func (_WarpTest *WarpTestCaller) GetVerifiedWarpMessage(opts *bind.CallOpts, index uint32) (struct {
	Message WarpMessage
	Valid   bool
}, error) {
	var out []interface{}
	err := _WarpTest.contract.Call(opts, &out, "getVerifiedWarpMessage", index)

	outstruct := new(struct {
		Message WarpMessage
		Valid   bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Message = *abi.ConvertType(out[0], new(WarpMessage)).(*WarpMessage)
	outstruct.Valid = *abi.ConvertType(out[1], new(bool)).(*bool)

	return *outstruct, err

}

// GetVerifiedWarpMessage is a free data retrieval call binding the contract method 0x6f825350.
//
// Solidity: function getVerifiedWarpMessage(uint32 index) view returns((bytes32,address,bytes) message, bool valid)
func (_WarpTest *WarpTestSession) GetVerifiedWarpMessage(index uint32) (struct {
	Message WarpMessage
	Valid   bool
}, error) {
	return _WarpTest.Contract.GetVerifiedWarpMessage(&_WarpTest.CallOpts, index)
}

// GetVerifiedWarpMessage is a free data retrieval call binding the contract method 0x6f825350.
//
// Solidity: function getVerifiedWarpMessage(uint32 index) view returns((bytes32,address,bytes) message, bool valid)
func (_WarpTest *WarpTestCallerSession) GetVerifiedWarpMessage(index uint32) (struct {
	Message WarpMessage
	Valid   bool
}, error) {
	return _WarpTest.Contract.GetVerifiedWarpMessage(&_WarpTest.CallOpts, index)
}

// SendWarpMessage is a paid mutator transaction binding the contract method 0xee5b48eb.
//
// Solidity: function sendWarpMessage(bytes payload) returns(bytes32 messageID)
func (_WarpTest *WarpTestTransactor) SendWarpMessage(opts *bind.TransactOpts, payload []byte) (*types.Transaction, error) {
	return _WarpTest.contract.Transact(opts, "sendWarpMessage", payload)
}

// SendWarpMessage is a paid mutator transaction binding the contract method 0xee5b48eb.
//
// Solidity: function sendWarpMessage(bytes payload) returns(bytes32 messageID)
func (_WarpTest *WarpTestSession) SendWarpMessage(payload []byte) (*types.Transaction, error) {
	return _WarpTest.Contract.SendWarpMessage(&_WarpTest.TransactOpts, payload)
}

// SendWarpMessage is a paid mutator transaction binding the contract method 0xee5b48eb.
//
// Solidity: function sendWarpMessage(bytes payload) returns(bytes32 messageID)
func (_WarpTest *WarpTestTransactorSession) SendWarpMessage(payload []byte) (*types.Transaction, error) {
	return _WarpTest.Contract.SendWarpMessage(&_WarpTest.TransactOpts, payload)
}
