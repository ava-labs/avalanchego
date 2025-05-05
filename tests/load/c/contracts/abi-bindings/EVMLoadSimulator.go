// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package evmloadsimulator

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

// EVMLoadSimulatorMetaData contains all meta data concerning the EVMLoadSimulator contract.
var EVMLoadSimulatorMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"}],\"name\":\"HashCalculates\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256[]\",\"name\":\"arr\",\"type\":\"uint256[]\"}],\"name\":\"MemoryWritten\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"accountId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"StorageUpdate\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"name\":\"SumCalculated\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"balancesCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"depth\",\"type\":\"uint256\"}],\"name\":\"simulateCallDepth\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"simulateContractCreation\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"rounds\",\"type\":\"uint256\"}],\"name\":\"simulateHashing\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"sizeInWords\",\"type\":\"uint256\"}],\"name\":\"simulateMemory\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"count\",\"type\":\"uint256\"}],\"name\":\"simulateModification\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"count\",\"type\":\"uint256\"}],\"name\":\"simulateRandomWrite\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"count\",\"type\":\"uint256\"}],\"name\":\"simulateReads\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x6080604052348015600e575f5ffd5b50610dca8061001c5f395ff3fe608060405234801561000f575f5ffd5b5060043610610086575f3560e01c8063aae05a6511610059578063aae05a65146100fe578063b77513d11461011a578063f05ed79e14610136578063fb0c00121461016657610086565b80633851d6e71461008a578063542eedd9146100a85780635de583ef146100c45780637db6ecb1146100ce575b5f5ffd5b610092610196565b60405161009f919061060f565b60405180910390f35b6100c260048036038101906100bd9190610656565b61019c565b005b6100cc61021a565b005b6100e860048036038101906100e39190610656565b610243565b6040516100f59190610699565b60405180910390f35b61011860048036038101906101139190610656565b6102ed565b005b610134600480360381019061012f9190610656565b6103f6565b005b610150600480360381019061014b9190610656565b610481565b60405161015d919061060f565b60405180910390f35b610180600480360381019061017b9190610656565b61056e565b60405161018d919061060f565b60405180910390f35b60015481565b5f811115610217573073ffffffffffffffffffffffffffffffffffffffff1663542eedd96001836101cd91906106df565b6040518263ffffffff1660e01b81526004016101e9919061060f565b5f604051808303815f87803b158015610200575f5ffd5b505af1158015610212573d5f5f3e3d5ffd5b505050505b50565b604051610226906105ea565b604051809103905ff08015801561023f573d5f5f3e3d5ffd5b5050565b5f60405160200161025390610766565b6040516020818303038152906040528051906020012090505f5f90505b828110156102b057818160405160200161028b9291906107ba565b6040516020818303038152906040528051906020012091508080600101915050610270565b507f30ca2ef0880ae63712fdaf11aefb67752968cff6f845956fcbdfcf421f4647cb816040516102e09190610699565b60405180910390a1919050565b5f600190505b8181116103f257600154811015610377575f60015f5f8481526020019081526020015f205461032291906107e5565b9050805f5f8481526020019081526020015f2081905550817fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc882604051610369919061060f565b60405180910390a2506103df565b5f60015f81548092919061038a90610818565b919050559050815f5f8381526020019081526020015f2081905550807fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8836040516103d5919061060f565b60405180910390a2505b80806103ea90610818565b9150506102f3565b5050565b5f600190505b81811161047d575f60015f81548092919061041690610818565b919050559050815f5f8381526020019081526020015f2081905550807fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc883604051610461919061060f565b60405180910390a250808061047590610818565b9150506103fc565b5050565b5f5f8267ffffffffffffffff81111561049d5761049c61085f565b5b6040519080825280602002602001820160405280156104cb5781602001602082028036833780820191505090505b5090505f5f90505b8381101561053057808282815181106104ef576104ee61088c565b5b60200260200101818152505081818151811061050e5761050d61088c565b5b60200260200101518361052191906107e5565b925080806001019150506104d3565b507f542a9e74627abe4fb012aa9be028f3234ff2b2253530c6fa2220e29f03e4215d816040516105609190610970565b60405180910390a150919050565b5f5f600190505b8281116105ad575f5f8281526020019081526020015f20548261059891906107e5565b915080806105a590610818565b915050610575565b507fe32d91cad5061d7491327c51e7b799c677b41d033204a5c5022b120f5da4becb816040516105dd919061060f565b60405180910390a1919050565b6104048061099183390190565b5f819050919050565b610609816105f7565b82525050565b5f6020820190506106225f830184610600565b92915050565b5f5ffd5b610635816105f7565b811461063f575f5ffd5b50565b5f813590506106508161062c565b92915050565b5f6020828403121561066b5761066a610628565b5b5f61067884828501610642565b91505092915050565b5f819050919050565b61069381610681565b82525050565b5f6020820190506106ac5f83018461068a565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6106e9826105f7565b91506106f4836105f7565b925082820390508181111561070c5761070b6106b2565b5b92915050565b5f81905092915050565b7f696e697469616c000000000000000000000000000000000000000000000000005f82015250565b5f610750600783610712565b915061075b8261071c565b600782019050919050565b5f61077082610744565b9150819050919050565b5f819050919050565b61079461078f82610681565b61077a565b82525050565b5f819050919050565b6107b46107af826105f7565b61079a565b82525050565b5f6107c58285610783565b6020820191506107d582846107a3565b6020820191508190509392505050565b5f6107ef826105f7565b91506107fa836105f7565b9250828201905080821115610812576108116106b2565b5b92915050565b5f610822826105f7565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff8203610854576108536106b2565b5b600182019050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52603260045260245ffd5b5f81519050919050565b5f82825260208201905092915050565b5f819050602082019050919050565b6108eb816105f7565b82525050565b5f6108fc83836108e2565b60208301905092915050565b5f602082019050919050565b5f61091e826108b9565b61092881856108c3565b9350610933836108d3565b805f5b8381101561096357815161094a88826108f1565b975061095583610908565b925050600181019050610936565b5085935050505092915050565b5f6020820190508181035f8301526109888184610914565b90509291505056fe6080604052348015600e575f5ffd5b50602a5f819055503360015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055506103a1806100635f395ff3fe608060405234801561000f575f5ffd5b5060043610610060575f3560e01c806337ebbc03146100645780633fa4f24514610094578063573c0bd3146100b25780638da5cb5b146100ce578063c71ba63b146100ec578063f0ba844014610108575b5f5ffd5b61007e6004803603810190610079919061025b565b610138565b60405161008b9190610295565b60405180910390f35b61009c610152565b6040516100a99190610295565b60405180910390f35b6100cc60048036038101906100c7919061025b565b610157565b005b6100d6610197565b6040516100e391906102ed565b60405180910390f35b61010660048036038101906101019190610306565b6101bc565b005b610122600480360381019061011d919061025b565b61020f565b60405161012f9190610295565b60405180910390f35b5f60025f8381526020019081526020015f20549050919050565b5f5481565b805f819055507f4273d0736f60e0dedfe745e86718093d8ec8646ebd2a60cd60643eeced5658118160405161018c9190610295565b60405180910390a150565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b8060025f8481526020019081526020015f20819055507f36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c2818282604051610203929190610344565b60405180910390a15050565b6002602052805f5260405f205f915090505481565b5f5ffd5b5f819050919050565b61023a81610228565b8114610244575f5ffd5b50565b5f8135905061025581610231565b92915050565b5f602082840312156102705761026f610224565b5b5f61027d84828501610247565b91505092915050565b61028f81610228565b82525050565b5f6020820190506102a85f830184610286565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102d7826102ae565b9050919050565b6102e7816102cd565b82525050565b5f6020820190506103005f8301846102de565b92915050565b5f5f6040838503121561031c5761031b610224565b5b5f61032985828601610247565b925050602061033a85828601610247565b9150509250929050565b5f6040820190506103575f830185610286565b6103646020830184610286565b939250505056fea26469706673582212200bd74d81b93b7dc77fade12be1f53f8209ef223885d94714c8cee2cb297385c364736f6c634300081d0033a264697066735822122073ad563c28027c7f04ffb0a102fcc20c96ae4742617f9470dd929073a38500de64736f6c634300081d0033",
}

// EVMLoadSimulatorABI is the input ABI used to generate the binding from.
// Deprecated: Use EVMLoadSimulatorMetaData.ABI instead.
var EVMLoadSimulatorABI = EVMLoadSimulatorMetaData.ABI

// EVMLoadSimulatorBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use EVMLoadSimulatorMetaData.Bin instead.
var EVMLoadSimulatorBin = EVMLoadSimulatorMetaData.Bin

// DeployEVMLoadSimulator deploys a new Ethereum contract, binding an instance of EVMLoadSimulator to it.
func DeployEVMLoadSimulator(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *EVMLoadSimulator, error) {
	parsed, err := EVMLoadSimulatorMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(EVMLoadSimulatorBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &EVMLoadSimulator{EVMLoadSimulatorCaller: EVMLoadSimulatorCaller{contract: contract}, EVMLoadSimulatorTransactor: EVMLoadSimulatorTransactor{contract: contract}, EVMLoadSimulatorFilterer: EVMLoadSimulatorFilterer{contract: contract}}, nil
}

// EVMLoadSimulator is an auto generated Go binding around an Ethereum contract.
type EVMLoadSimulator struct {
	EVMLoadSimulatorCaller     // Read-only binding to the contract
	EVMLoadSimulatorTransactor // Write-only binding to the contract
	EVMLoadSimulatorFilterer   // Log filterer for contract events
}

// EVMLoadSimulatorCaller is an auto generated read-only Go binding around an Ethereum contract.
type EVMLoadSimulatorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EVMLoadSimulatorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EVMLoadSimulatorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EVMLoadSimulatorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EVMLoadSimulatorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EVMLoadSimulatorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EVMLoadSimulatorSession struct {
	Contract     *EVMLoadSimulator // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EVMLoadSimulatorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EVMLoadSimulatorCallerSession struct {
	Contract *EVMLoadSimulatorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// EVMLoadSimulatorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EVMLoadSimulatorTransactorSession struct {
	Contract     *EVMLoadSimulatorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// EVMLoadSimulatorRaw is an auto generated low-level Go binding around an Ethereum contract.
type EVMLoadSimulatorRaw struct {
	Contract *EVMLoadSimulator // Generic contract binding to access the raw methods on
}

// EVMLoadSimulatorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EVMLoadSimulatorCallerRaw struct {
	Contract *EVMLoadSimulatorCaller // Generic read-only contract binding to access the raw methods on
}

// EVMLoadSimulatorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EVMLoadSimulatorTransactorRaw struct {
	Contract *EVMLoadSimulatorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEVMLoadSimulator creates a new instance of EVMLoadSimulator, bound to a specific deployed contract.
func NewEVMLoadSimulator(address common.Address, backend bind.ContractBackend) (*EVMLoadSimulator, error) {
	contract, err := bindEVMLoadSimulator(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulator{EVMLoadSimulatorCaller: EVMLoadSimulatorCaller{contract: contract}, EVMLoadSimulatorTransactor: EVMLoadSimulatorTransactor{contract: contract}, EVMLoadSimulatorFilterer: EVMLoadSimulatorFilterer{contract: contract}}, nil
}

// NewEVMLoadSimulatorCaller creates a new read-only instance of EVMLoadSimulator, bound to a specific deployed contract.
func NewEVMLoadSimulatorCaller(address common.Address, caller bind.ContractCaller) (*EVMLoadSimulatorCaller, error) {
	contract, err := bindEVMLoadSimulator(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorCaller{contract: contract}, nil
}

// NewEVMLoadSimulatorTransactor creates a new write-only instance of EVMLoadSimulator, bound to a specific deployed contract.
func NewEVMLoadSimulatorTransactor(address common.Address, transactor bind.ContractTransactor) (*EVMLoadSimulatorTransactor, error) {
	contract, err := bindEVMLoadSimulator(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorTransactor{contract: contract}, nil
}

// NewEVMLoadSimulatorFilterer creates a new log filterer instance of EVMLoadSimulator, bound to a specific deployed contract.
func NewEVMLoadSimulatorFilterer(address common.Address, filterer bind.ContractFilterer) (*EVMLoadSimulatorFilterer, error) {
	contract, err := bindEVMLoadSimulator(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorFilterer{contract: contract}, nil
}

// bindEVMLoadSimulator binds a generic wrapper to an already deployed contract.
func bindEVMLoadSimulator(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := EVMLoadSimulatorMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EVMLoadSimulator *EVMLoadSimulatorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _EVMLoadSimulator.Contract.EVMLoadSimulatorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EVMLoadSimulator *EVMLoadSimulatorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.EVMLoadSimulatorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EVMLoadSimulator *EVMLoadSimulatorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.EVMLoadSimulatorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EVMLoadSimulator *EVMLoadSimulatorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _EVMLoadSimulator.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.contract.Transact(opts, method, params...)
}

// BalancesCount is a free data retrieval call binding the contract method 0x3851d6e7.
//
// Solidity: function balancesCount() view returns(uint256)
func (_EVMLoadSimulator *EVMLoadSimulatorCaller) BalancesCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _EVMLoadSimulator.contract.Call(opts, &out, "balancesCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalancesCount is a free data retrieval call binding the contract method 0x3851d6e7.
//
// Solidity: function balancesCount() view returns(uint256)
func (_EVMLoadSimulator *EVMLoadSimulatorSession) BalancesCount() (*big.Int, error) {
	return _EVMLoadSimulator.Contract.BalancesCount(&_EVMLoadSimulator.CallOpts)
}

// BalancesCount is a free data retrieval call binding the contract method 0x3851d6e7.
//
// Solidity: function balancesCount() view returns(uint256)
func (_EVMLoadSimulator *EVMLoadSimulatorCallerSession) BalancesCount() (*big.Int, error) {
	return _EVMLoadSimulator.Contract.BalancesCount(&_EVMLoadSimulator.CallOpts)
}

// SimulateCallDepth is a paid mutator transaction binding the contract method 0x542eedd9.
//
// Solidity: function simulateCallDepth(uint256 depth) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateCallDepth(opts *bind.TransactOpts, depth *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateCallDepth", depth)
}

// SimulateCallDepth is a paid mutator transaction binding the contract method 0x542eedd9.
//
// Solidity: function simulateCallDepth(uint256 depth) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateCallDepth(depth *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateCallDepth(&_EVMLoadSimulator.TransactOpts, depth)
}

// SimulateCallDepth is a paid mutator transaction binding the contract method 0x542eedd9.
//
// Solidity: function simulateCallDepth(uint256 depth) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateCallDepth(depth *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateCallDepth(&_EVMLoadSimulator.TransactOpts, depth)
}

// SimulateContractCreation is a paid mutator transaction binding the contract method 0x5de583ef.
//
// Solidity: function simulateContractCreation() returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateContractCreation(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateContractCreation")
}

// SimulateContractCreation is a paid mutator transaction binding the contract method 0x5de583ef.
//
// Solidity: function simulateContractCreation() returns()
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateContractCreation() (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateContractCreation(&_EVMLoadSimulator.TransactOpts)
}

// SimulateContractCreation is a paid mutator transaction binding the contract method 0x5de583ef.
//
// Solidity: function simulateContractCreation() returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateContractCreation() (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateContractCreation(&_EVMLoadSimulator.TransactOpts)
}

// SimulateHashing is a paid mutator transaction binding the contract method 0x7db6ecb1.
//
// Solidity: function simulateHashing(uint256 rounds) returns(bytes32 hash)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateHashing(opts *bind.TransactOpts, rounds *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateHashing", rounds)
}

// SimulateHashing is a paid mutator transaction binding the contract method 0x7db6ecb1.
//
// Solidity: function simulateHashing(uint256 rounds) returns(bytes32 hash)
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateHashing(rounds *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateHashing(&_EVMLoadSimulator.TransactOpts, rounds)
}

// SimulateHashing is a paid mutator transaction binding the contract method 0x7db6ecb1.
//
// Solidity: function simulateHashing(uint256 rounds) returns(bytes32 hash)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateHashing(rounds *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateHashing(&_EVMLoadSimulator.TransactOpts, rounds)
}

// SimulateMemory is a paid mutator transaction binding the contract method 0xf05ed79e.
//
// Solidity: function simulateMemory(uint256 sizeInWords) returns(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateMemory(opts *bind.TransactOpts, sizeInWords *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateMemory", sizeInWords)
}

// SimulateMemory is a paid mutator transaction binding the contract method 0xf05ed79e.
//
// Solidity: function simulateMemory(uint256 sizeInWords) returns(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateMemory(sizeInWords *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateMemory(&_EVMLoadSimulator.TransactOpts, sizeInWords)
}

// SimulateMemory is a paid mutator transaction binding the contract method 0xf05ed79e.
//
// Solidity: function simulateMemory(uint256 sizeInWords) returns(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateMemory(sizeInWords *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateMemory(&_EVMLoadSimulator.TransactOpts, sizeInWords)
}

// SimulateModification is a paid mutator transaction binding the contract method 0xaae05a65.
//
// Solidity: function simulateModification(uint256 count) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateModification(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateModification", count)
}

// SimulateModification is a paid mutator transaction binding the contract method 0xaae05a65.
//
// Solidity: function simulateModification(uint256 count) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateModification(count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateModification(&_EVMLoadSimulator.TransactOpts, count)
}

// SimulateModification is a paid mutator transaction binding the contract method 0xaae05a65.
//
// Solidity: function simulateModification(uint256 count) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateModification(count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateModification(&_EVMLoadSimulator.TransactOpts, count)
}

// SimulateRandomWrite is a paid mutator transaction binding the contract method 0xb77513d1.
//
// Solidity: function simulateRandomWrite(uint256 count) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateRandomWrite(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateRandomWrite", count)
}

// SimulateRandomWrite is a paid mutator transaction binding the contract method 0xb77513d1.
//
// Solidity: function simulateRandomWrite(uint256 count) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateRandomWrite(count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateRandomWrite(&_EVMLoadSimulator.TransactOpts, count)
}

// SimulateRandomWrite is a paid mutator transaction binding the contract method 0xb77513d1.
//
// Solidity: function simulateRandomWrite(uint256 count) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateRandomWrite(count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateRandomWrite(&_EVMLoadSimulator.TransactOpts, count)
}

// SimulateReads is a paid mutator transaction binding the contract method 0xfb0c0012.
//
// Solidity: function simulateReads(uint256 count) returns(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateReads(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateReads", count)
}

// SimulateReads is a paid mutator transaction binding the contract method 0xfb0c0012.
//
// Solidity: function simulateReads(uint256 count) returns(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateReads(count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateReads(&_EVMLoadSimulator.TransactOpts, count)
}

// SimulateReads is a paid mutator transaction binding the contract method 0xfb0c0012.
//
// Solidity: function simulateReads(uint256 count) returns(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateReads(count *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateReads(&_EVMLoadSimulator.TransactOpts, count)
}

// EVMLoadSimulatorHashCalculatesIterator is returned from FilterHashCalculates and is used to iterate over the raw logs and unpacked data for HashCalculates events raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorHashCalculatesIterator struct {
	Event *EVMLoadSimulatorHashCalculates // Event containing the contract specifics and raw log

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
func (it *EVMLoadSimulatorHashCalculatesIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EVMLoadSimulatorHashCalculates)
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
		it.Event = new(EVMLoadSimulatorHashCalculates)
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
func (it *EVMLoadSimulatorHashCalculatesIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EVMLoadSimulatorHashCalculatesIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EVMLoadSimulatorHashCalculates represents a HashCalculates event raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorHashCalculates struct {
	Hash [32]byte
	Raw  types.Log // Blockchain specific contextual infos
}

// FilterHashCalculates is a free log retrieval operation binding the contract event 0x30ca2ef0880ae63712fdaf11aefb67752968cff6f845956fcbdfcf421f4647cb.
//
// Solidity: event HashCalculates(bytes32 hash)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) FilterHashCalculates(opts *bind.FilterOpts) (*EVMLoadSimulatorHashCalculatesIterator, error) {

	logs, sub, err := _EVMLoadSimulator.contract.FilterLogs(opts, "HashCalculates")
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorHashCalculatesIterator{contract: _EVMLoadSimulator.contract, event: "HashCalculates", logs: logs, sub: sub}, nil
}

// WatchHashCalculates is a free log subscription operation binding the contract event 0x30ca2ef0880ae63712fdaf11aefb67752968cff6f845956fcbdfcf421f4647cb.
//
// Solidity: event HashCalculates(bytes32 hash)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) WatchHashCalculates(opts *bind.WatchOpts, sink chan<- *EVMLoadSimulatorHashCalculates) (event.Subscription, error) {

	logs, sub, err := _EVMLoadSimulator.contract.WatchLogs(opts, "HashCalculates")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EVMLoadSimulatorHashCalculates)
				if err := _EVMLoadSimulator.contract.UnpackLog(event, "HashCalculates", log); err != nil {
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

// ParseHashCalculates is a log parse operation binding the contract event 0x30ca2ef0880ae63712fdaf11aefb67752968cff6f845956fcbdfcf421f4647cb.
//
// Solidity: event HashCalculates(bytes32 hash)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) ParseHashCalculates(log types.Log) (*EVMLoadSimulatorHashCalculates, error) {
	event := new(EVMLoadSimulatorHashCalculates)
	if err := _EVMLoadSimulator.contract.UnpackLog(event, "HashCalculates", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EVMLoadSimulatorMemoryWrittenIterator is returned from FilterMemoryWritten and is used to iterate over the raw logs and unpacked data for MemoryWritten events raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorMemoryWrittenIterator struct {
	Event *EVMLoadSimulatorMemoryWritten // Event containing the contract specifics and raw log

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
func (it *EVMLoadSimulatorMemoryWrittenIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EVMLoadSimulatorMemoryWritten)
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
		it.Event = new(EVMLoadSimulatorMemoryWritten)
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
func (it *EVMLoadSimulatorMemoryWrittenIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EVMLoadSimulatorMemoryWrittenIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EVMLoadSimulatorMemoryWritten represents a MemoryWritten event raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorMemoryWritten struct {
	Arr []*big.Int
	Raw types.Log // Blockchain specific contextual infos
}

// FilterMemoryWritten is a free log retrieval operation binding the contract event 0x542a9e74627abe4fb012aa9be028f3234ff2b2253530c6fa2220e29f03e4215d.
//
// Solidity: event MemoryWritten(uint256[] arr)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) FilterMemoryWritten(opts *bind.FilterOpts) (*EVMLoadSimulatorMemoryWrittenIterator, error) {

	logs, sub, err := _EVMLoadSimulator.contract.FilterLogs(opts, "MemoryWritten")
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorMemoryWrittenIterator{contract: _EVMLoadSimulator.contract, event: "MemoryWritten", logs: logs, sub: sub}, nil
}

// WatchMemoryWritten is a free log subscription operation binding the contract event 0x542a9e74627abe4fb012aa9be028f3234ff2b2253530c6fa2220e29f03e4215d.
//
// Solidity: event MemoryWritten(uint256[] arr)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) WatchMemoryWritten(opts *bind.WatchOpts, sink chan<- *EVMLoadSimulatorMemoryWritten) (event.Subscription, error) {

	logs, sub, err := _EVMLoadSimulator.contract.WatchLogs(opts, "MemoryWritten")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EVMLoadSimulatorMemoryWritten)
				if err := _EVMLoadSimulator.contract.UnpackLog(event, "MemoryWritten", log); err != nil {
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

// ParseMemoryWritten is a log parse operation binding the contract event 0x542a9e74627abe4fb012aa9be028f3234ff2b2253530c6fa2220e29f03e4215d.
//
// Solidity: event MemoryWritten(uint256[] arr)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) ParseMemoryWritten(log types.Log) (*EVMLoadSimulatorMemoryWritten, error) {
	event := new(EVMLoadSimulatorMemoryWritten)
	if err := _EVMLoadSimulator.contract.UnpackLog(event, "MemoryWritten", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EVMLoadSimulatorStorageUpdateIterator is returned from FilterStorageUpdate and is used to iterate over the raw logs and unpacked data for StorageUpdate events raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorStorageUpdateIterator struct {
	Event *EVMLoadSimulatorStorageUpdate // Event containing the contract specifics and raw log

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
func (it *EVMLoadSimulatorStorageUpdateIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EVMLoadSimulatorStorageUpdate)
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
		it.Event = new(EVMLoadSimulatorStorageUpdate)
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
func (it *EVMLoadSimulatorStorageUpdateIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EVMLoadSimulatorStorageUpdateIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EVMLoadSimulatorStorageUpdate represents a StorageUpdate event raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorStorageUpdate struct {
	AccountId *big.Int
	Value     *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterStorageUpdate is a free log retrieval operation binding the contract event 0xbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8.
//
// Solidity: event StorageUpdate(uint256 indexed accountId, uint256 value)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) FilterStorageUpdate(opts *bind.FilterOpts, accountId []*big.Int) (*EVMLoadSimulatorStorageUpdateIterator, error) {

	var accountIdRule []interface{}
	for _, accountIdItem := range accountId {
		accountIdRule = append(accountIdRule, accountIdItem)
	}

	logs, sub, err := _EVMLoadSimulator.contract.FilterLogs(opts, "StorageUpdate", accountIdRule)
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorStorageUpdateIterator{contract: _EVMLoadSimulator.contract, event: "StorageUpdate", logs: logs, sub: sub}, nil
}

// WatchStorageUpdate is a free log subscription operation binding the contract event 0xbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8.
//
// Solidity: event StorageUpdate(uint256 indexed accountId, uint256 value)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) WatchStorageUpdate(opts *bind.WatchOpts, sink chan<- *EVMLoadSimulatorStorageUpdate, accountId []*big.Int) (event.Subscription, error) {

	var accountIdRule []interface{}
	for _, accountIdItem := range accountId {
		accountIdRule = append(accountIdRule, accountIdItem)
	}

	logs, sub, err := _EVMLoadSimulator.contract.WatchLogs(opts, "StorageUpdate", accountIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EVMLoadSimulatorStorageUpdate)
				if err := _EVMLoadSimulator.contract.UnpackLog(event, "StorageUpdate", log); err != nil {
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

// ParseStorageUpdate is a log parse operation binding the contract event 0xbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8.
//
// Solidity: event StorageUpdate(uint256 indexed accountId, uint256 value)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) ParseStorageUpdate(log types.Log) (*EVMLoadSimulatorStorageUpdate, error) {
	event := new(EVMLoadSimulatorStorageUpdate)
	if err := _EVMLoadSimulator.contract.UnpackLog(event, "StorageUpdate", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EVMLoadSimulatorSumCalculatedIterator is returned from FilterSumCalculated and is used to iterate over the raw logs and unpacked data for SumCalculated events raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorSumCalculatedIterator struct {
	Event *EVMLoadSimulatorSumCalculated // Event containing the contract specifics and raw log

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
func (it *EVMLoadSimulatorSumCalculatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EVMLoadSimulatorSumCalculated)
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
		it.Event = new(EVMLoadSimulatorSumCalculated)
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
func (it *EVMLoadSimulatorSumCalculatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EVMLoadSimulatorSumCalculatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EVMLoadSimulatorSumCalculated represents a SumCalculated event raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorSumCalculated struct {
	Sum *big.Int
	Raw types.Log // Blockchain specific contextual infos
}

// FilterSumCalculated is a free log retrieval operation binding the contract event 0xe32d91cad5061d7491327c51e7b799c677b41d033204a5c5022b120f5da4becb.
//
// Solidity: event SumCalculated(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) FilterSumCalculated(opts *bind.FilterOpts) (*EVMLoadSimulatorSumCalculatedIterator, error) {

	logs, sub, err := _EVMLoadSimulator.contract.FilterLogs(opts, "SumCalculated")
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorSumCalculatedIterator{contract: _EVMLoadSimulator.contract, event: "SumCalculated", logs: logs, sub: sub}, nil
}

// WatchSumCalculated is a free log subscription operation binding the contract event 0xe32d91cad5061d7491327c51e7b799c677b41d033204a5c5022b120f5da4becb.
//
// Solidity: event SumCalculated(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) WatchSumCalculated(opts *bind.WatchOpts, sink chan<- *EVMLoadSimulatorSumCalculated) (event.Subscription, error) {

	logs, sub, err := _EVMLoadSimulator.contract.WatchLogs(opts, "SumCalculated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EVMLoadSimulatorSumCalculated)
				if err := _EVMLoadSimulator.contract.UnpackLog(event, "SumCalculated", log); err != nil {
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

// ParseSumCalculated is a log parse operation binding the contract event 0xe32d91cad5061d7491327c51e7b799c677b41d033204a5c5022b120f5da4becb.
//
// Solidity: event SumCalculated(uint256 sum)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) ParseSumCalculated(log types.Log) (*EVMLoadSimulatorSumCalculated, error) {
	event := new(EVMLoadSimulatorSumCalculated)
	if err := _EVMLoadSimulator.contract.UnpackLog(event, "SumCalculated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
