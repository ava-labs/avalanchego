// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contracts

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
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"}],\"name\":\"HashCalculates\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"largeData\",\"type\":\"bytes\"}],\"name\":\"LargeLog\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256[]\",\"name\":\"arr\",\"type\":\"uint256[]\"}],\"name\":\"MemoryWritten\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"accountId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"StorageUpdate\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"name\":\"SumCalculated\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"balancesCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"dummy\",\"outputs\":[{\"internalType\":\"contractDummy\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"depth\",\"type\":\"uint256\"}],\"name\":\"simulateCallDepth\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"simulateContractCreation\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"simulateExternalCall\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"rounds\",\"type\":\"uint256\"}],\"name\":\"simulateHashing\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"size\",\"type\":\"uint256\"}],\"name\":\"simulateLargeEvent\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"sizeInWords\",\"type\":\"uint256\"}],\"name\":\"simulateMemory\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"count\",\"type\":\"uint256\"}],\"name\":\"simulateModification\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"iterations\",\"type\":\"uint256\"}],\"name\":\"simulatePureCompute\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"result\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"count\",\"type\":\"uint256\"}],\"name\":\"simulateRandomWrite\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"count\",\"type\":\"uint256\"}],\"name\":\"simulateReads\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561000f575f80fd5b506114608061001d5f395ff3fe608060405234801561000f575f80fd5b50600436106100b2575f3560e01c8063aae05a651161006f578063aae05a6514610178578063ab7611d114610194578063b77513d1146101b0578063e730b4bd146101cc578063f05ed79e146101d6578063fb0c001214610206576100b2565b8063130fcab6146100b657806332e43a11146100e65780633851d6e714610104578063542eedd9146101225780635de583ef1461013e5780637db6ecb114610148575b5f80fd5b6100d060048036038101906100cb9190610a4b565b610236565b6040516100dd9190610a85565b60405180910390f35b6100ee6102dc565b6040516100fb9190610b18565b60405180910390f35b61010c610301565b6040516101199190610a85565b60405180910390f35b61013c60048036038101906101379190610a4b565b610307565b005b6101466103c1565b005b610162600480360381019061015d9190610a4b565b610428565b60405161016f9190610b49565b60405180910390f35b610192600480360381019061018d9190610a4b565b6104d5565b005b6101ae60048036038101906101a99190610a4b565b6105de565b005b6101ca60048036038101906101c59190610a4b565b6106cc565b005b6101d4610757565b005b6101f060048036038101906101eb9190610a4b565b61089b565b6040516101fd9190610a85565b60405180910390f35b610220600480360381019061021b9190610a4b565b61098b565b60405161022d9190610a85565b60405180910390f35b5f805f90505b8281101561029e576001816102519190610b8f565b81600283846102609190610bc2565b61026a9190610c30565b6102749190610b8f565b61027e9190610c60565b826102899190610b8f565b9150808061029690610c90565b91505061023c565b505f7fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc85f6040516102cf9190610d10565b60405180910390a2919050565b60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60015481565b5f810361034b575f7fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc85f60405161033e9190610d10565b60405180910390a26103be565b3073ffffffffffffffffffffffffffffffffffffffff1663542eedd96001836103749190610d29565b6040518263ffffffff1660e01b81526004016103909190610a85565b5f604051808303815f87803b1580156103a7575f80fd5b505af11580156103b9573d5f803e3d5ffd5b505050505b50565b6040516103cd90610a07565b604051809103905ff0801580156103e6573d5f803e3d5ffd5b5060025f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550565b5f60405160200161043890610db0565b6040516020818303038152906040528051906020012090505f5b8281101561049857818160405160200161046d929190610e04565b604051602081830303815290604052805190602001209150808061049090610c90565b915050610452565b507f30ca2ef0880ae63712fdaf11aefb67752968cff6f845956fcbdfcf421f4647cb816040516104c89190610b49565b60405180910390a1919050565b5f600190505b8181116105da5760015481101561055f575f60015f808481526020019081526020015f205461050a9190610b8f565b9050805f808481526020019081526020015f2081905550817fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8826040516105519190610a85565b60405180910390a2506105c7565b5f60015f81548092919061057290610c90565b919050559050815f808381526020019081526020015f2081905550807fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8836040516105bd9190610a85565b60405180910390a2505b80806105d290610c90565b9150506104db565b5050565b5f8167ffffffffffffffff8111156105f9576105f8610e2f565b5b6040519080825280601f01601f19166020018201604052801561062b5781602001600182028036833780820191505090505b5090505f5b82811015610690578060f81b82828151811061064f5761064e610e5c565b5b60200101907effffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff191690815f1a905350808061068890610c90565b915050610630565b507f5e53254f5b56e942cb89e1beff9257b039a5593ffe94274d0640a636b57fd0ac816040516106c09190610f13565b60405180910390a15050565b5f600190505b818111610753575f60015f8154809291906106ec90610c90565b919050559050815f808381526020019081526020015f2081905550807fbed7bf46680bfe44399acf02887c2443b1894b86596db85714936273e7db7cc8836040516107379190610a85565b60405180910390a250808061074b90610c90565b9150506106d2565b5050565b5f73ffffffffffffffffffffffffffffffffffffffff1660025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1603610811576040516107b790610a07565b604051809103905ff0801580156107d0573d5f803e3d5ffd5b5060025f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055505b60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663573c0bd3602a6040518263ffffffff1660e01b815260040161086c9190610f6c565b5f604051808303815f87803b158015610883575f80fd5b505af1158015610895573d5f803e3d5ffd5b50505050565b5f808267ffffffffffffffff8111156108b7576108b6610e2f565b5b6040519080825280602002602001820160405280156108e55781602001602082028036833780820191505090505b5090505f5b8381101561094d578082828151811061090657610905610e5c565b5b60200260200101818152505081818151811061092557610924610e5c565b5b6020026020010151836109389190610b8f565b9250808061094590610c90565b9150506108ea565b507f542a9e74627abe4fb012aa9be028f3234ff2b2253530c6fa2220e29f03e4215d8160405161097d919061103c565b60405180910390a150919050565b5f80600190505b8281116109ca575f808281526020019081526020015f2054826109b59190610b8f565b915080806109c290610c90565b915050610992565b507fe32d91cad5061d7491327c51e7b799c677b41d033204a5c5022b120f5da4becb816040516109fa9190610a85565b60405180910390a1919050565b6103ce8061105d83390190565b5f80fd5b5f819050919050565b610a2a81610a18565b8114610a34575f80fd5b50565b5f81359050610a4581610a21565b92915050565b5f60208284031215610a6057610a5f610a14565b5b5f610a6d84828501610a37565b91505092915050565b610a7f81610a18565b82525050565b5f602082019050610a985f830184610a76565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f819050919050565b5f610ae0610adb610ad684610a9e565b610abd565b610a9e565b9050919050565b5f610af182610ac6565b9050919050565b5f610b0282610ae7565b9050919050565b610b1281610af8565b82525050565b5f602082019050610b2b5f830184610b09565b92915050565b5f819050919050565b610b4381610b31565b82525050565b5f602082019050610b5c5f830184610b3a565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610b9982610a18565b9150610ba483610a18565b9250828201905080821115610bbc57610bbb610b62565b5b92915050565b5f610bcc82610a18565b9150610bd783610a18565b9250828202610be581610a18565b91508282048414831517610bfc57610bfb610b62565b5b5092915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601260045260245ffd5b5f610c3a82610a18565b9150610c4583610a18565b925082610c5557610c54610c03565b5b828204905092915050565b5f610c6a82610a18565b9150610c7583610a18565b925082610c8557610c84610c03565b5b828206905092915050565b5f610c9a82610a18565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff8203610ccc57610ccb610b62565b5b600182019050919050565b5f819050919050565b5f610cfa610cf5610cf084610cd7565b610abd565b610a18565b9050919050565b610d0a81610ce0565b82525050565b5f602082019050610d235f830184610d01565b92915050565b5f610d3382610a18565b9150610d3e83610a18565b9250828203905081811115610d5657610d55610b62565b5b92915050565b5f81905092915050565b7f696e697469616c000000000000000000000000000000000000000000000000005f82015250565b5f610d9a600783610d5c565b9150610da582610d66565b600782019050919050565b5f610dba82610d8e565b9150819050919050565b5f819050919050565b610dde610dd982610b31565b610dc4565b82525050565b5f819050919050565b610dfe610df982610a18565b610de4565b82525050565b5f610e0f8285610dcd565b602082019150610e1f8284610ded565b6020820191508190509392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52603260045260245ffd5b5f81519050919050565b5f82825260208201905092915050565b5f5b83811015610ec0578082015181840152602081019050610ea5565b5f8484015250505050565b5f601f19601f8301169050919050565b5f610ee582610e89565b610eef8185610e93565b9350610eff818560208601610ea3565b610f0881610ecb565b840191505092915050565b5f6020820190508181035f830152610f2b8184610edb565b905092915050565b5f819050919050565b5f610f56610f51610f4c84610f33565b610abd565b610a18565b9050919050565b610f6681610f3c565b82525050565b5f602082019050610f7f5f830184610f5d565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f819050602082019050919050565b610fb781610a18565b82525050565b5f610fc88383610fae565b60208301905092915050565b5f602082019050919050565b5f610fea82610f85565b610ff48185610f8f565b9350610fff83610f9f565b805f5b8381101561102f5781516110168882610fbd565b975061102183610fd4565b925050600181019050611002565b5085935050505092915050565b5f6020820190508181035f8301526110548184610fe0565b90509291505056fe608060405234801561000f575f80fd5b50602a5f819055503360015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555061036a806100645f395ff3fe608060405234801561000f575f80fd5b5060043610610060575f3560e01c806337ebbc03146100645780633fa4f24514610094578063573c0bd3146100b25780638da5cb5b146100ce578063c71ba63b146100ec578063f0ba844014610108575b5f80fd5b61007e60048036038101906100799190610224565b610138565b60405161008b919061025e565b60405180910390f35b61009c610152565b6040516100a9919061025e565b60405180910390f35b6100cc60048036038101906100c79190610224565b610157565b005b6100d6610160565b6040516100e391906102b6565b60405180910390f35b610106600480360381019061010191906102cf565b610185565b005b610122600480360381019061011d9190610224565b6101d8565b60405161012f919061025e565b60405180910390f35b5f60025f8381526020019081526020015f20549050919050565b5f5481565b805f8190555050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b8060025f8481526020019081526020015f20819055507f36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c28182826040516101cc92919061030d565b60405180910390a15050565b6002602052805f5260405f205f915090505481565b5f80fd5b5f819050919050565b610203816101f1565b811461020d575f80fd5b50565b5f8135905061021e816101fa565b92915050565b5f60208284031215610239576102386101ed565b5b5f61024684828501610210565b91505092915050565b610258816101f1565b82525050565b5f6020820190506102715f83018461024f565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102a082610277565b9050919050565b6102b081610296565b82525050565b5f6020820190506102c95f8301846102a7565b92915050565b5f80604083850312156102e5576102e46101ed565b5b5f6102f285828601610210565b925050602061030385828601610210565b9150509250929050565b5f6040820190506103205f83018561024f565b61032d602083018461024f565b939250505056fea2646970667358221220a44af404da38b49bae0be72ddce95db226c9a908c51f433a00208892fba4801964736f6c63430008150033a2646970667358221220dd764531ab8daa512ea36f609f93d58d0b9ba8711ea011fadd4aa298e0f9bcec64736f6c63430008150033",
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

// Dummy is a free data retrieval call binding the contract method 0x32e43a11.
//
// Solidity: function dummy() view returns(address)
func (_EVMLoadSimulator *EVMLoadSimulatorCaller) Dummy(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _EVMLoadSimulator.contract.Call(opts, &out, "dummy")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Dummy is a free data retrieval call binding the contract method 0x32e43a11.
//
// Solidity: function dummy() view returns(address)
func (_EVMLoadSimulator *EVMLoadSimulatorSession) Dummy() (common.Address, error) {
	return _EVMLoadSimulator.Contract.Dummy(&_EVMLoadSimulator.CallOpts)
}

// Dummy is a free data retrieval call binding the contract method 0x32e43a11.
//
// Solidity: function dummy() view returns(address)
func (_EVMLoadSimulator *EVMLoadSimulatorCallerSession) Dummy() (common.Address, error) {
	return _EVMLoadSimulator.Contract.Dummy(&_EVMLoadSimulator.CallOpts)
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

// SimulateExternalCall is a paid mutator transaction binding the contract method 0xe730b4bd.
//
// Solidity: function simulateExternalCall() returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateExternalCall(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateExternalCall")
}

// SimulateExternalCall is a paid mutator transaction binding the contract method 0xe730b4bd.
//
// Solidity: function simulateExternalCall() returns()
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateExternalCall() (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateExternalCall(&_EVMLoadSimulator.TransactOpts)
}

// SimulateExternalCall is a paid mutator transaction binding the contract method 0xe730b4bd.
//
// Solidity: function simulateExternalCall() returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateExternalCall() (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateExternalCall(&_EVMLoadSimulator.TransactOpts)
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

// SimulateLargeEvent is a paid mutator transaction binding the contract method 0xab7611d1.
//
// Solidity: function simulateLargeEvent(uint256 size) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulateLargeEvent(opts *bind.TransactOpts, size *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulateLargeEvent", size)
}

// SimulateLargeEvent is a paid mutator transaction binding the contract method 0xab7611d1.
//
// Solidity: function simulateLargeEvent(uint256 size) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulateLargeEvent(size *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateLargeEvent(&_EVMLoadSimulator.TransactOpts, size)
}

// SimulateLargeEvent is a paid mutator transaction binding the contract method 0xab7611d1.
//
// Solidity: function simulateLargeEvent(uint256 size) returns()
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulateLargeEvent(size *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulateLargeEvent(&_EVMLoadSimulator.TransactOpts, size)
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

// SimulatePureCompute is a paid mutator transaction binding the contract method 0x130fcab6.
//
// Solidity: function simulatePureCompute(uint256 iterations) returns(uint256 result)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactor) SimulatePureCompute(opts *bind.TransactOpts, iterations *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.contract.Transact(opts, "simulatePureCompute", iterations)
}

// SimulatePureCompute is a paid mutator transaction binding the contract method 0x130fcab6.
//
// Solidity: function simulatePureCompute(uint256 iterations) returns(uint256 result)
func (_EVMLoadSimulator *EVMLoadSimulatorSession) SimulatePureCompute(iterations *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulatePureCompute(&_EVMLoadSimulator.TransactOpts, iterations)
}

// SimulatePureCompute is a paid mutator transaction binding the contract method 0x130fcab6.
//
// Solidity: function simulatePureCompute(uint256 iterations) returns(uint256 result)
func (_EVMLoadSimulator *EVMLoadSimulatorTransactorSession) SimulatePureCompute(iterations *big.Int) (*types.Transaction, error) {
	return _EVMLoadSimulator.Contract.SimulatePureCompute(&_EVMLoadSimulator.TransactOpts, iterations)
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

// EVMLoadSimulatorLargeLogIterator is returned from FilterLargeLog and is used to iterate over the raw logs and unpacked data for LargeLog events raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorLargeLogIterator struct {
	Event *EVMLoadSimulatorLargeLog // Event containing the contract specifics and raw log

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
func (it *EVMLoadSimulatorLargeLogIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EVMLoadSimulatorLargeLog)
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
		it.Event = new(EVMLoadSimulatorLargeLog)
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
func (it *EVMLoadSimulatorLargeLogIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EVMLoadSimulatorLargeLogIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EVMLoadSimulatorLargeLog represents a LargeLog event raised by the EVMLoadSimulator contract.
type EVMLoadSimulatorLargeLog struct {
	LargeData []byte
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterLargeLog is a free log retrieval operation binding the contract event 0x5e53254f5b56e942cb89e1beff9257b039a5593ffe94274d0640a636b57fd0ac.
//
// Solidity: event LargeLog(bytes largeData)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) FilterLargeLog(opts *bind.FilterOpts) (*EVMLoadSimulatorLargeLogIterator, error) {

	logs, sub, err := _EVMLoadSimulator.contract.FilterLogs(opts, "LargeLog")
	if err != nil {
		return nil, err
	}
	return &EVMLoadSimulatorLargeLogIterator{contract: _EVMLoadSimulator.contract, event: "LargeLog", logs: logs, sub: sub}, nil
}

// WatchLargeLog is a free log subscription operation binding the contract event 0x5e53254f5b56e942cb89e1beff9257b039a5593ffe94274d0640a636b57fd0ac.
//
// Solidity: event LargeLog(bytes largeData)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) WatchLargeLog(opts *bind.WatchOpts, sink chan<- *EVMLoadSimulatorLargeLog) (event.Subscription, error) {

	logs, sub, err := _EVMLoadSimulator.contract.WatchLogs(opts, "LargeLog")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EVMLoadSimulatorLargeLog)
				if err := _EVMLoadSimulator.contract.UnpackLog(event, "LargeLog", log); err != nil {
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

// ParseLargeLog is a log parse operation binding the contract event 0x5e53254f5b56e942cb89e1beff9257b039a5593ffe94274d0640a636b57fd0ac.
//
// Solidity: event LargeLog(bytes largeData)
func (_EVMLoadSimulator *EVMLoadSimulatorFilterer) ParseLargeLog(log types.Log) (*EVMLoadSimulatorLargeLog, error) {
	event := new(EVMLoadSimulatorLargeLog)
	if err := _EVMLoadSimulator.contract.UnpackLog(event, "LargeLog", log); err != nil {
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
