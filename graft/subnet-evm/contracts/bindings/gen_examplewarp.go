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

// ExampleWarpMetaData contains all meta data concerning the ExampleWarp contract.
var ExampleWarpMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"}],\"name\":\"sendWarpMessage\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"blockchainID\",\"type\":\"bytes32\"}],\"name\":\"validateGetBlockchainID\",\"outputs\":[],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"index\",\"type\":\"uint32\"}],\"name\":\"validateInvalidWarpBlockHash\",\"outputs\":[],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"index\",\"type\":\"uint32\"}],\"name\":\"validateInvalidWarpMessage\",\"outputs\":[],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"index\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"sourceChainID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"blockHash\",\"type\":\"bytes32\"}],\"name\":\"validateWarpBlockHash\",\"outputs\":[],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"index\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"sourceChainID\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"originSenderAddress\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"}],\"name\":\"validateWarpMessage\",\"outputs\":[],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x60806040527302000000000000000000000000000000000000055f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055503480156061575f5ffd5b50610d158061006f5f395ff3fe608060405234801561000f575f5ffd5b5060043610610060575f3560e01c806315f0c959146100645780635bd05f061461008057806377ca84db1461009c578063e519286f146100b8578063ee5b48eb146100d4578063f25ec06a146100f0575b5f5ffd5b61007e60048036038101906100799190610672565b61010c565b005b61009a60048036038101906100959190610791565b6101a6565b005b6100b660048036038101906100b19190610815565b6102cf565b005b6100d260048036038101906100cd9190610840565b61039d565b005b6100ee60048036038101906100e99190610890565b610468565b005b61010a60048036038101906101059190610815565b610508565b005b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16634213cf786040518163ffffffff1660e01b8152600401602060405180830381865afa158015610175573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061019991906108ef565b81146101a3575f5ffd5b50565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16636f825350886040518263ffffffff1660e01b81526004016102019190610929565b5f60405180830381865afa15801561021b573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906102439190610b48565b9150915080610250575f5ffd5b85825f01511461025e575f5ffd5b8473ffffffffffffffffffffffffffffffffffffffff16826020015173ffffffffffffffffffffffffffffffffffffffff1614610299575f5ffd5b83836040516102a9929190610bde565b6040518091039020826040015180519060200120146102c6575f5ffd5b50505050505050565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663ce7f5929846040518263ffffffff1660e01b815260040161032a9190610929565b606060405180830381865afa158015610345573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103699190610c43565b915091508015610377575f5ffd5b5f5f1b825f015114610387575f5ffd5b5f5f1b826020015114610398575f5ffd5b505050565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663ce7f5929866040518263ffffffff1660e01b81526004016103f89190610929565b606060405180830381865afa158015610413573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906104379190610c43565b9150915080610444575f5ffd5b83825f015114610452575f5ffd5b82826020015114610461575f5ffd5b5050505050565b5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663ee5b48eb83836040518363ffffffff1660e01b81526004016104c3929190610cbd565b6020604051808303815f875af11580156104df573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061050391906108ef565b505050565b5f5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16636f825350846040518263ffffffff1660e01b81526004016105639190610929565b5f60405180830381865afa15801561057d573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906105a59190610b48565b9150915080156105b3575f5ffd5b5f5f1b825f0151146105c3575f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff16826020015173ffffffffffffffffffffffffffffffffffffffff16146105fe575f5ffd5b60405180602001604052805f8152508051906020012082604001518051906020012014610629575f5ffd5b505050565b5f604051905090565b5f5ffd5b5f5ffd5b5f819050919050565b6106518161063f565b811461065b575f5ffd5b50565b5f8135905061066c81610648565b92915050565b5f6020828403121561068757610686610637565b5b5f6106948482850161065e565b91505092915050565b5f63ffffffff82169050919050565b6106b58161069d565b81146106bf575f5ffd5b50565b5f813590506106d0816106ac565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6106ff826106d6565b9050919050565b61070f816106f5565b8114610719575f5ffd5b50565b5f8135905061072a81610706565b92915050565b5f5ffd5b5f5ffd5b5f5ffd5b5f5f83601f84011261075157610750610730565b5b8235905067ffffffffffffffff81111561076e5761076d610734565b5b60208301915083600182028301111561078a57610789610738565b5b9250929050565b5f5f5f5f5f608086880312156107aa576107a9610637565b5b5f6107b7888289016106c2565b95505060206107c88882890161065e565b94505060406107d98882890161071c565b935050606086013567ffffffffffffffff8111156107fa576107f961063b565b5b6108068882890161073c565b92509250509295509295909350565b5f6020828403121561082a57610829610637565b5b5f610837848285016106c2565b91505092915050565b5f5f5f6060848603121561085757610856610637565b5b5f610864868287016106c2565b93505060206108758682870161065e565b92505060406108868682870161065e565b9150509250925092565b5f5f602083850312156108a6576108a5610637565b5b5f83013567ffffffffffffffff8111156108c3576108c261063b565b5b6108cf8582860161073c565b92509250509250929050565b5f815190506108e981610648565b92915050565b5f6020828403121561090457610903610637565b5b5f610911848285016108db565b91505092915050565b6109238161069d565b82525050565b5f60208201905061093c5f83018461091a565b92915050565b5f5ffd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b61098c82610946565b810181811067ffffffffffffffff821117156109ab576109aa610956565b5b80604052505050565b5f6109bd61062e565b90506109c98282610983565b919050565b5f5ffd5b5f815190506109e081610706565b92915050565b5f5ffd5b5f67ffffffffffffffff821115610a0457610a03610956565b5b610a0d82610946565b9050602081019050919050565b8281835e5f83830152505050565b5f610a3a610a35846109ea565b6109b4565b905082815260208101848484011115610a5657610a556109e6565b5b610a61848285610a1a565b509392505050565b5f82601f830112610a7d57610a7c610730565b5b8151610a8d848260208601610a28565b91505092915050565b5f60608284031215610aab57610aaa610942565b5b610ab560606109b4565b90505f610ac4848285016108db565b5f830152506020610ad7848285016109d2565b602083015250604082015167ffffffffffffffff811115610afb57610afa6109ce565b5b610b0784828501610a69565b60408301525092915050565b5f8115159050919050565b610b2781610b13565b8114610b31575f5ffd5b50565b5f81519050610b4281610b1e565b92915050565b5f5f60408385031215610b5e57610b5d610637565b5b5f83015167ffffffffffffffff811115610b7b57610b7a61063b565b5b610b8785828601610a96565b9250506020610b9885828601610b34565b9150509250929050565b5f81905092915050565b828183375f83830152505050565b5f610bc58385610ba2565b9350610bd2838584610bac565b82840190509392505050565b5f610bea828486610bba565b91508190509392505050565b5f60408284031215610c0b57610c0a610942565b5b610c1560406109b4565b90505f610c24848285016108db565b5f830152506020610c37848285016108db565b60208301525092915050565b5f5f60608385031215610c5957610c58610637565b5b5f610c6685828601610bf6565b9250506040610c7785828601610b34565b9150509250929050565b5f82825260208201905092915050565b5f610c9c8385610c81565b9350610ca9838584610bac565b610cb283610946565b840190509392505050565b5f6020820190508181035f830152610cd6818486610c91565b9050939250505056fea26469706673582212203aa2f3f51e0713d8d1f446f157ef2504c89940d33ec5cba0e160a28e6af1a1b664736f6c634300081e0033",
}

// ExampleWarpABI is the input ABI used to generate the binding from.
// Deprecated: Use ExampleWarpMetaData.ABI instead.
var ExampleWarpABI = ExampleWarpMetaData.ABI

// ExampleWarpBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ExampleWarpMetaData.Bin instead.
var ExampleWarpBin = ExampleWarpMetaData.Bin

// DeployExampleWarp deploys a new Ethereum contract, binding an instance of ExampleWarp to it.
func DeployExampleWarp(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ExampleWarp, error) {
	parsed, err := ExampleWarpMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ExampleWarpBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ExampleWarp{ExampleWarpCaller: ExampleWarpCaller{contract: contract}, ExampleWarpTransactor: ExampleWarpTransactor{contract: contract}, ExampleWarpFilterer: ExampleWarpFilterer{contract: contract}}, nil
}

// ExampleWarp is an auto generated Go binding around an Ethereum contract.
type ExampleWarp struct {
	ExampleWarpCaller     // Read-only binding to the contract
	ExampleWarpTransactor // Write-only binding to the contract
	ExampleWarpFilterer   // Log filterer for contract events
}

// ExampleWarpCaller is an auto generated read-only Go binding around an Ethereum contract.
type ExampleWarpCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleWarpTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ExampleWarpTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleWarpFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ExampleWarpFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleWarpSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ExampleWarpSession struct {
	Contract     *ExampleWarp      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ExampleWarpCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ExampleWarpCallerSession struct {
	Contract *ExampleWarpCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// ExampleWarpTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ExampleWarpTransactorSession struct {
	Contract     *ExampleWarpTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// ExampleWarpRaw is an auto generated low-level Go binding around an Ethereum contract.
type ExampleWarpRaw struct {
	Contract *ExampleWarp // Generic contract binding to access the raw methods on
}

// ExampleWarpCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ExampleWarpCallerRaw struct {
	Contract *ExampleWarpCaller // Generic read-only contract binding to access the raw methods on
}

// ExampleWarpTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ExampleWarpTransactorRaw struct {
	Contract *ExampleWarpTransactor // Generic write-only contract binding to access the raw methods on
}

// NewExampleWarp creates a new instance of ExampleWarp, bound to a specific deployed contract.
func NewExampleWarp(address common.Address, backend bind.ContractBackend) (*ExampleWarp, error) {
	contract, err := bindExampleWarp(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ExampleWarp{ExampleWarpCaller: ExampleWarpCaller{contract: contract}, ExampleWarpTransactor: ExampleWarpTransactor{contract: contract}, ExampleWarpFilterer: ExampleWarpFilterer{contract: contract}}, nil
}

// NewExampleWarpCaller creates a new read-only instance of ExampleWarp, bound to a specific deployed contract.
func NewExampleWarpCaller(address common.Address, caller bind.ContractCaller) (*ExampleWarpCaller, error) {
	contract, err := bindExampleWarp(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleWarpCaller{contract: contract}, nil
}

// NewExampleWarpTransactor creates a new write-only instance of ExampleWarp, bound to a specific deployed contract.
func NewExampleWarpTransactor(address common.Address, transactor bind.ContractTransactor) (*ExampleWarpTransactor, error) {
	contract, err := bindExampleWarp(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleWarpTransactor{contract: contract}, nil
}

// NewExampleWarpFilterer creates a new log filterer instance of ExampleWarp, bound to a specific deployed contract.
func NewExampleWarpFilterer(address common.Address, filterer bind.ContractFilterer) (*ExampleWarpFilterer, error) {
	contract, err := bindExampleWarp(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ExampleWarpFilterer{contract: contract}, nil
}

// bindExampleWarp binds a generic wrapper to an already deployed contract.
func bindExampleWarp(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ExampleWarpMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleWarp *ExampleWarpRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleWarp.Contract.ExampleWarpCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleWarp *ExampleWarpRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleWarp.Contract.ExampleWarpTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleWarp *ExampleWarpRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleWarp.Contract.ExampleWarpTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleWarp *ExampleWarpCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleWarp.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleWarp *ExampleWarpTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleWarp.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleWarp *ExampleWarpTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleWarp.Contract.contract.Transact(opts, method, params...)
}

// ValidateGetBlockchainID is a free data retrieval call binding the contract method 0x15f0c959.
//
// Solidity: function validateGetBlockchainID(bytes32 blockchainID) view returns()
func (_ExampleWarp *ExampleWarpCaller) ValidateGetBlockchainID(opts *bind.CallOpts, blockchainID [32]byte) error {
	var out []interface{}
	err := _ExampleWarp.contract.Call(opts, &out, "validateGetBlockchainID", blockchainID)

	if err != nil {
		return err
	}

	return err

}

// ValidateGetBlockchainID is a free data retrieval call binding the contract method 0x15f0c959.
//
// Solidity: function validateGetBlockchainID(bytes32 blockchainID) view returns()
func (_ExampleWarp *ExampleWarpSession) ValidateGetBlockchainID(blockchainID [32]byte) error {
	return _ExampleWarp.Contract.ValidateGetBlockchainID(&_ExampleWarp.CallOpts, blockchainID)
}

// ValidateGetBlockchainID is a free data retrieval call binding the contract method 0x15f0c959.
//
// Solidity: function validateGetBlockchainID(bytes32 blockchainID) view returns()
func (_ExampleWarp *ExampleWarpCallerSession) ValidateGetBlockchainID(blockchainID [32]byte) error {
	return _ExampleWarp.Contract.ValidateGetBlockchainID(&_ExampleWarp.CallOpts, blockchainID)
}

// ValidateInvalidWarpBlockHash is a free data retrieval call binding the contract method 0x77ca84db.
//
// Solidity: function validateInvalidWarpBlockHash(uint32 index) view returns()
func (_ExampleWarp *ExampleWarpCaller) ValidateInvalidWarpBlockHash(opts *bind.CallOpts, index uint32) error {
	var out []interface{}
	err := _ExampleWarp.contract.Call(opts, &out, "validateInvalidWarpBlockHash", index)

	if err != nil {
		return err
	}

	return err

}

// ValidateInvalidWarpBlockHash is a free data retrieval call binding the contract method 0x77ca84db.
//
// Solidity: function validateInvalidWarpBlockHash(uint32 index) view returns()
func (_ExampleWarp *ExampleWarpSession) ValidateInvalidWarpBlockHash(index uint32) error {
	return _ExampleWarp.Contract.ValidateInvalidWarpBlockHash(&_ExampleWarp.CallOpts, index)
}

// ValidateInvalidWarpBlockHash is a free data retrieval call binding the contract method 0x77ca84db.
//
// Solidity: function validateInvalidWarpBlockHash(uint32 index) view returns()
func (_ExampleWarp *ExampleWarpCallerSession) ValidateInvalidWarpBlockHash(index uint32) error {
	return _ExampleWarp.Contract.ValidateInvalidWarpBlockHash(&_ExampleWarp.CallOpts, index)
}

// ValidateInvalidWarpMessage is a free data retrieval call binding the contract method 0xf25ec06a.
//
// Solidity: function validateInvalidWarpMessage(uint32 index) view returns()
func (_ExampleWarp *ExampleWarpCaller) ValidateInvalidWarpMessage(opts *bind.CallOpts, index uint32) error {
	var out []interface{}
	err := _ExampleWarp.contract.Call(opts, &out, "validateInvalidWarpMessage", index)

	if err != nil {
		return err
	}

	return err

}

// ValidateInvalidWarpMessage is a free data retrieval call binding the contract method 0xf25ec06a.
//
// Solidity: function validateInvalidWarpMessage(uint32 index) view returns()
func (_ExampleWarp *ExampleWarpSession) ValidateInvalidWarpMessage(index uint32) error {
	return _ExampleWarp.Contract.ValidateInvalidWarpMessage(&_ExampleWarp.CallOpts, index)
}

// ValidateInvalidWarpMessage is a free data retrieval call binding the contract method 0xf25ec06a.
//
// Solidity: function validateInvalidWarpMessage(uint32 index) view returns()
func (_ExampleWarp *ExampleWarpCallerSession) ValidateInvalidWarpMessage(index uint32) error {
	return _ExampleWarp.Contract.ValidateInvalidWarpMessage(&_ExampleWarp.CallOpts, index)
}

// ValidateWarpBlockHash is a free data retrieval call binding the contract method 0xe519286f.
//
// Solidity: function validateWarpBlockHash(uint32 index, bytes32 sourceChainID, bytes32 blockHash) view returns()
func (_ExampleWarp *ExampleWarpCaller) ValidateWarpBlockHash(opts *bind.CallOpts, index uint32, sourceChainID [32]byte, blockHash [32]byte) error {
	var out []interface{}
	err := _ExampleWarp.contract.Call(opts, &out, "validateWarpBlockHash", index, sourceChainID, blockHash)

	if err != nil {
		return err
	}

	return err

}

// ValidateWarpBlockHash is a free data retrieval call binding the contract method 0xe519286f.
//
// Solidity: function validateWarpBlockHash(uint32 index, bytes32 sourceChainID, bytes32 blockHash) view returns()
func (_ExampleWarp *ExampleWarpSession) ValidateWarpBlockHash(index uint32, sourceChainID [32]byte, blockHash [32]byte) error {
	return _ExampleWarp.Contract.ValidateWarpBlockHash(&_ExampleWarp.CallOpts, index, sourceChainID, blockHash)
}

// ValidateWarpBlockHash is a free data retrieval call binding the contract method 0xe519286f.
//
// Solidity: function validateWarpBlockHash(uint32 index, bytes32 sourceChainID, bytes32 blockHash) view returns()
func (_ExampleWarp *ExampleWarpCallerSession) ValidateWarpBlockHash(index uint32, sourceChainID [32]byte, blockHash [32]byte) error {
	return _ExampleWarp.Contract.ValidateWarpBlockHash(&_ExampleWarp.CallOpts, index, sourceChainID, blockHash)
}

// ValidateWarpMessage is a free data retrieval call binding the contract method 0x5bd05f06.
//
// Solidity: function validateWarpMessage(uint32 index, bytes32 sourceChainID, address originSenderAddress, bytes payload) view returns()
func (_ExampleWarp *ExampleWarpCaller) ValidateWarpMessage(opts *bind.CallOpts, index uint32, sourceChainID [32]byte, originSenderAddress common.Address, payload []byte) error {
	var out []interface{}
	err := _ExampleWarp.contract.Call(opts, &out, "validateWarpMessage", index, sourceChainID, originSenderAddress, payload)

	if err != nil {
		return err
	}

	return err

}

// ValidateWarpMessage is a free data retrieval call binding the contract method 0x5bd05f06.
//
// Solidity: function validateWarpMessage(uint32 index, bytes32 sourceChainID, address originSenderAddress, bytes payload) view returns()
func (_ExampleWarp *ExampleWarpSession) ValidateWarpMessage(index uint32, sourceChainID [32]byte, originSenderAddress common.Address, payload []byte) error {
	return _ExampleWarp.Contract.ValidateWarpMessage(&_ExampleWarp.CallOpts, index, sourceChainID, originSenderAddress, payload)
}

// ValidateWarpMessage is a free data retrieval call binding the contract method 0x5bd05f06.
//
// Solidity: function validateWarpMessage(uint32 index, bytes32 sourceChainID, address originSenderAddress, bytes payload) view returns()
func (_ExampleWarp *ExampleWarpCallerSession) ValidateWarpMessage(index uint32, sourceChainID [32]byte, originSenderAddress common.Address, payload []byte) error {
	return _ExampleWarp.Contract.ValidateWarpMessage(&_ExampleWarp.CallOpts, index, sourceChainID, originSenderAddress, payload)
}

// SendWarpMessage is a paid mutator transaction binding the contract method 0xee5b48eb.
//
// Solidity: function sendWarpMessage(bytes payload) returns()
func (_ExampleWarp *ExampleWarpTransactor) SendWarpMessage(opts *bind.TransactOpts, payload []byte) (*types.Transaction, error) {
	return _ExampleWarp.contract.Transact(opts, "sendWarpMessage", payload)
}

// SendWarpMessage is a paid mutator transaction binding the contract method 0xee5b48eb.
//
// Solidity: function sendWarpMessage(bytes payload) returns()
func (_ExampleWarp *ExampleWarpSession) SendWarpMessage(payload []byte) (*types.Transaction, error) {
	return _ExampleWarp.Contract.SendWarpMessage(&_ExampleWarp.TransactOpts, payload)
}

// SendWarpMessage is a paid mutator transaction binding the contract method 0xee5b48eb.
//
// Solidity: function sendWarpMessage(bytes payload) returns()
func (_ExampleWarp *ExampleWarpTransactorSession) SendWarpMessage(payload []byte) (*types.Transaction, error) {
	return _ExampleWarp.Contract.SendWarpMessage(&_ExampleWarp.TransactOpts, payload)
}
