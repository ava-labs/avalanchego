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

// LoadSimulatorMetaData contains all meta data concerning the LoadSimulator contract.
var LoadSimulatorMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"name\":\"LargeCalldata\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"deploy\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"n\",\"type\":\"uint256\"}],\"name\":\"hash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"result\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"largeCalldata\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"numSlots\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"newValue\",\"type\":\"uint256\"}],\"name\":\"modify\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"offset\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"numSlots\",\"type\":\"uint256\"}],\"name\":\"read\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"numSlots\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"write\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x6080604052348015600e575f5ffd5b506002600181905550604051602190607c565b604051809103905ff0801580156039573d5f5f3e3d5ffd5b505f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055506089565b6103cd8061099983390190565b610903806100965f395ff3fe608060405234801561000f575f5ffd5b5060043610610060575f3560e01c80637508099714610064578063775c300c1461009457806385d058871461009e5780639c0e3f7a146100ce578063a78dac0d146100fe578063a977e1d11461012e575b5f5ffd5b61007e6004803603810190610079919061030e565b61014a565b60405161008b919061035b565b60405180910390f35b61009c61016f565b005b6100b860048036038101906100b3919061030e565b6101d5565b6040516100c5919061038e565b60405180910390f35b6100e860048036038101906100e3919061030e565b610209565b6040516100f5919061038e565b60405180910390f35b6101186004803603810190610113919061030e565b610239565b60405161012591906103bf565b60405180910390f35b61014860048036038101906101439190610439565b610289565b005b5f825b8284018110156101685780548201915060018101905061014d565b5092915050565b60405161017b906102c6565b604051809103905ff080158015610194573d5f5f3e3d5ffd5b505f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550565b5f600154600181018410156101fe575f5b848110156101fc578381556001810190506101e6565b505b506001905092915050565b5f600154805b8482018110156102275783815560018101905061020f565b50838101600155506001905092915050565b5f825f1b90505f5f90505b82811015610282578160405160200161025d91906103bf565b6040516020818303038152906040528051906020012091508080600101915050610244565b5092915050565b7f7cdeb400b923483a299a592d3b984525e73532d62e6861a342855a80c6a54a3182826040516102ba9291906104de565b60405180910390a15050565b6103cd8061050183390190565b5f5ffd5b5f5ffd5b5f819050919050565b6102ed816102db565b81146102f7575f5ffd5b50565b5f81359050610308816102e4565b92915050565b5f5f60408385031215610324576103236102d3565b5b5f610331858286016102fa565b9250506020610342858286016102fa565b9150509250929050565b610355816102db565b82525050565b5f60208201905061036e5f83018461034c565b92915050565b5f8115159050919050565b61038881610374565b82525050565b5f6020820190506103a15f83018461037f565b92915050565b5f819050919050565b6103b9816103a7565b82525050565b5f6020820190506103d25f8301846103b0565b92915050565b5f5ffd5b5f5ffd5b5f5ffd5b5f5f83601f8401126103f9576103f86103d8565b5b8235905067ffffffffffffffff811115610416576104156103dc565b5b602083019150836001820283011115610432576104316103e0565b5b9250929050565b5f5f6020838503121561044f5761044e6102d3565b5b5f83013567ffffffffffffffff81111561046c5761046b6102d7565b5b610478858286016103e4565b92509250509250929050565b5f82825260208201905092915050565b828183375f83830152505050565b5f601f19601f8301169050919050565b5f6104bd8385610484565b93506104ca838584610494565b6104d3836104a2565b840190509392505050565b5f6020820190508181035f8301526104f78184866104b2565b9050939250505056fe6080604052348015600e575f5ffd5b50602a5f819055503360015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555061036a806100635f395ff3fe608060405234801561000f575f5ffd5b5060043610610060575f3560e01c806337ebbc03146100645780633fa4f24514610094578063573c0bd3146100b25780638da5cb5b146100ce578063c71ba63b146100ec578063f0ba844014610108575b5f5ffd5b61007e60048036038101906100799190610224565b610138565b60405161008b919061025e565b60405180910390f35b61009c610152565b6040516100a9919061025e565b60405180910390f35b6100cc60048036038101906100c79190610224565b610157565b005b6100d6610160565b6040516100e391906102b6565b60405180910390f35b610106600480360381019061010191906102cf565b610185565b005b610122600480360381019061011d9190610224565b6101d8565b60405161012f919061025e565b60405180910390f35b5f60025f8381526020019081526020015f20549050919050565b5f5481565b805f8190555050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b8060025f8481526020019081526020015f20819055507f36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c28182826040516101cc92919061030d565b60405180910390a15050565b6002602052805f5260405f205f915090505481565b5f5ffd5b5f819050919050565b610203816101f1565b811461020d575f5ffd5b50565b5f8135905061021e816101fa565b92915050565b5f60208284031215610239576102386101ed565b5b5f61024684828501610210565b91505092915050565b610258816101f1565b82525050565b5f6020820190506102715f83018461024f565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102a082610277565b9050919050565b6102b081610296565b82525050565b5f6020820190506102c95f8301846102a7565b92915050565b5f5f604083850312156102e5576102e46101ed565b5b5f6102f285828601610210565b925050602061030385828601610210565b9150509250929050565b5f6040820190506103205f83018561024f565b61032d602083018461024f565b939250505056fea2646970667358221220c3459f01c7a5d1340193485b3495b5671336a204f3ff21406ac8d0950184e09a64736f6c634300081c0033a264697066735822122038d57c3498fab1474d2d0fd401c5093b87d6fd89c2a35bf03be1c5d98e60116364736f6c634300081c00336080604052348015600e575f5ffd5b50602a5f819055503360015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555061036a806100635f395ff3fe608060405234801561000f575f5ffd5b5060043610610060575f3560e01c806337ebbc03146100645780633fa4f24514610094578063573c0bd3146100b25780638da5cb5b146100ce578063c71ba63b146100ec578063f0ba844014610108575b5f5ffd5b61007e60048036038101906100799190610224565b610138565b60405161008b919061025e565b60405180910390f35b61009c610152565b6040516100a9919061025e565b60405180910390f35b6100cc60048036038101906100c79190610224565b610157565b005b6100d6610160565b6040516100e391906102b6565b60405180910390f35b610106600480360381019061010191906102cf565b610185565b005b610122600480360381019061011d9190610224565b6101d8565b60405161012f919061025e565b60405180910390f35b5f60025f8381526020019081526020015f20549050919050565b5f5481565b805f8190555050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b8060025f8481526020019081526020015f20819055507f36c0e38a11934bb6e80e00c4ae42212be021022fdb5aff12c53720f1d951c28182826040516101cc92919061030d565b60405180910390a15050565b6002602052805f5260405f205f915090505481565b5f5ffd5b5f819050919050565b610203816101f1565b811461020d575f5ffd5b50565b5f8135905061021e816101fa565b92915050565b5f60208284031215610239576102386101ed565b5b5f61024684828501610210565b91505092915050565b610258816101f1565b82525050565b5f6020820190506102715f83018461024f565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102a082610277565b9050919050565b6102b081610296565b82525050565b5f6020820190506102c95f8301846102a7565b92915050565b5f5f604083850312156102e5576102e46101ed565b5b5f6102f285828601610210565b925050602061030385828601610210565b9150509250929050565b5f6040820190506103205f83018561024f565b61032d602083018461024f565b939250505056fea2646970667358221220c3459f01c7a5d1340193485b3495b5671336a204f3ff21406ac8d0950184e09a64736f6c634300081c0033",
}

// LoadSimulatorABI is the input ABI used to generate the binding from.
// Deprecated: Use LoadSimulatorMetaData.ABI instead.
var LoadSimulatorABI = LoadSimulatorMetaData.ABI

// LoadSimulatorBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use LoadSimulatorMetaData.Bin instead.
var LoadSimulatorBin = LoadSimulatorMetaData.Bin

// DeployLoadSimulator deploys a new Ethereum contract, binding an instance of LoadSimulator to it.
func DeployLoadSimulator(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *LoadSimulator, error) {
	parsed, err := LoadSimulatorMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(LoadSimulatorBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &LoadSimulator{LoadSimulatorCaller: LoadSimulatorCaller{contract: contract}, LoadSimulatorTransactor: LoadSimulatorTransactor{contract: contract}, LoadSimulatorFilterer: LoadSimulatorFilterer{contract: contract}}, nil
}

// LoadSimulator is an auto generated Go binding around an Ethereum contract.
type LoadSimulator struct {
	LoadSimulatorCaller     // Read-only binding to the contract
	LoadSimulatorTransactor // Write-only binding to the contract
	LoadSimulatorFilterer   // Log filterer for contract events
}

// LoadSimulatorCaller is an auto generated read-only Go binding around an Ethereum contract.
type LoadSimulatorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// LoadSimulatorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type LoadSimulatorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// LoadSimulatorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type LoadSimulatorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// LoadSimulatorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type LoadSimulatorSession struct {
	Contract     *LoadSimulator    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// LoadSimulatorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type LoadSimulatorCallerSession struct {
	Contract *LoadSimulatorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// LoadSimulatorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type LoadSimulatorTransactorSession struct {
	Contract     *LoadSimulatorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// LoadSimulatorRaw is an auto generated low-level Go binding around an Ethereum contract.
type LoadSimulatorRaw struct {
	Contract *LoadSimulator // Generic contract binding to access the raw methods on
}

// LoadSimulatorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type LoadSimulatorCallerRaw struct {
	Contract *LoadSimulatorCaller // Generic read-only contract binding to access the raw methods on
}

// LoadSimulatorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type LoadSimulatorTransactorRaw struct {
	Contract *LoadSimulatorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewLoadSimulator creates a new instance of LoadSimulator, bound to a specific deployed contract.
func NewLoadSimulator(address common.Address, backend bind.ContractBackend) (*LoadSimulator, error) {
	contract, err := bindLoadSimulator(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &LoadSimulator{LoadSimulatorCaller: LoadSimulatorCaller{contract: contract}, LoadSimulatorTransactor: LoadSimulatorTransactor{contract: contract}, LoadSimulatorFilterer: LoadSimulatorFilterer{contract: contract}}, nil
}

// NewLoadSimulatorCaller creates a new read-only instance of LoadSimulator, bound to a specific deployed contract.
func NewLoadSimulatorCaller(address common.Address, caller bind.ContractCaller) (*LoadSimulatorCaller, error) {
	contract, err := bindLoadSimulator(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &LoadSimulatorCaller{contract: contract}, nil
}

// NewLoadSimulatorTransactor creates a new write-only instance of LoadSimulator, bound to a specific deployed contract.
func NewLoadSimulatorTransactor(address common.Address, transactor bind.ContractTransactor) (*LoadSimulatorTransactor, error) {
	contract, err := bindLoadSimulator(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &LoadSimulatorTransactor{contract: contract}, nil
}

// NewLoadSimulatorFilterer creates a new log filterer instance of LoadSimulator, bound to a specific deployed contract.
func NewLoadSimulatorFilterer(address common.Address, filterer bind.ContractFilterer) (*LoadSimulatorFilterer, error) {
	contract, err := bindLoadSimulator(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &LoadSimulatorFilterer{contract: contract}, nil
}

// bindLoadSimulator binds a generic wrapper to an already deployed contract.
func bindLoadSimulator(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := LoadSimulatorMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_LoadSimulator *LoadSimulatorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _LoadSimulator.Contract.LoadSimulatorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_LoadSimulator *LoadSimulatorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _LoadSimulator.Contract.LoadSimulatorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_LoadSimulator *LoadSimulatorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _LoadSimulator.Contract.LoadSimulatorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_LoadSimulator *LoadSimulatorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _LoadSimulator.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_LoadSimulator *LoadSimulatorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _LoadSimulator.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_LoadSimulator *LoadSimulatorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _LoadSimulator.Contract.contract.Transact(opts, method, params...)
}

// Deploy is a paid mutator transaction binding the contract method 0x775c300c.
//
// Solidity: function deploy() returns()
func (_LoadSimulator *LoadSimulatorTransactor) Deploy(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _LoadSimulator.contract.Transact(opts, "deploy")
}

// Deploy is a paid mutator transaction binding the contract method 0x775c300c.
//
// Solidity: function deploy() returns()
func (_LoadSimulator *LoadSimulatorSession) Deploy() (*types.Transaction, error) {
	return _LoadSimulator.Contract.Deploy(&_LoadSimulator.TransactOpts)
}

// Deploy is a paid mutator transaction binding the contract method 0x775c300c.
//
// Solidity: function deploy() returns()
func (_LoadSimulator *LoadSimulatorTransactorSession) Deploy() (*types.Transaction, error) {
	return _LoadSimulator.Contract.Deploy(&_LoadSimulator.TransactOpts)
}

// Hash is a paid mutator transaction binding the contract method 0xa78dac0d.
//
// Solidity: function hash(uint256 value, uint256 n) returns(bytes32 result)
func (_LoadSimulator *LoadSimulatorTransactor) Hash(opts *bind.TransactOpts, value *big.Int, n *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.contract.Transact(opts, "hash", value, n)
}

// Hash is a paid mutator transaction binding the contract method 0xa78dac0d.
//
// Solidity: function hash(uint256 value, uint256 n) returns(bytes32 result)
func (_LoadSimulator *LoadSimulatorSession) Hash(value *big.Int, n *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Hash(&_LoadSimulator.TransactOpts, value, n)
}

// Hash is a paid mutator transaction binding the contract method 0xa78dac0d.
//
// Solidity: function hash(uint256 value, uint256 n) returns(bytes32 result)
func (_LoadSimulator *LoadSimulatorTransactorSession) Hash(value *big.Int, n *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Hash(&_LoadSimulator.TransactOpts, value, n)
}

// LargeCalldata is a paid mutator transaction binding the contract method 0xa977e1d1.
//
// Solidity: function largeCalldata(bytes data) returns()
func (_LoadSimulator *LoadSimulatorTransactor) LargeCalldata(opts *bind.TransactOpts, data []byte) (*types.Transaction, error) {
	return _LoadSimulator.contract.Transact(opts, "largeCalldata", data)
}

// LargeCalldata is a paid mutator transaction binding the contract method 0xa977e1d1.
//
// Solidity: function largeCalldata(bytes data) returns()
func (_LoadSimulator *LoadSimulatorSession) LargeCalldata(data []byte) (*types.Transaction, error) {
	return _LoadSimulator.Contract.LargeCalldata(&_LoadSimulator.TransactOpts, data)
}

// LargeCalldata is a paid mutator transaction binding the contract method 0xa977e1d1.
//
// Solidity: function largeCalldata(bytes data) returns()
func (_LoadSimulator *LoadSimulatorTransactorSession) LargeCalldata(data []byte) (*types.Transaction, error) {
	return _LoadSimulator.Contract.LargeCalldata(&_LoadSimulator.TransactOpts, data)
}

// Modify is a paid mutator transaction binding the contract method 0x85d05887.
//
// Solidity: function modify(uint256 numSlots, uint256 newValue) returns(bool)
func (_LoadSimulator *LoadSimulatorTransactor) Modify(opts *bind.TransactOpts, numSlots *big.Int, newValue *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.contract.Transact(opts, "modify", numSlots, newValue)
}

// Modify is a paid mutator transaction binding the contract method 0x85d05887.
//
// Solidity: function modify(uint256 numSlots, uint256 newValue) returns(bool)
func (_LoadSimulator *LoadSimulatorSession) Modify(numSlots *big.Int, newValue *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Modify(&_LoadSimulator.TransactOpts, numSlots, newValue)
}

// Modify is a paid mutator transaction binding the contract method 0x85d05887.
//
// Solidity: function modify(uint256 numSlots, uint256 newValue) returns(bool)
func (_LoadSimulator *LoadSimulatorTransactorSession) Modify(numSlots *big.Int, newValue *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Modify(&_LoadSimulator.TransactOpts, numSlots, newValue)
}

// Read is a paid mutator transaction binding the contract method 0x75080997.
//
// Solidity: function read(uint256 offset, uint256 numSlots) returns(uint256 sum)
func (_LoadSimulator *LoadSimulatorTransactor) Read(opts *bind.TransactOpts, offset *big.Int, numSlots *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.contract.Transact(opts, "read", offset, numSlots)
}

// Read is a paid mutator transaction binding the contract method 0x75080997.
//
// Solidity: function read(uint256 offset, uint256 numSlots) returns(uint256 sum)
func (_LoadSimulator *LoadSimulatorSession) Read(offset *big.Int, numSlots *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Read(&_LoadSimulator.TransactOpts, offset, numSlots)
}

// Read is a paid mutator transaction binding the contract method 0x75080997.
//
// Solidity: function read(uint256 offset, uint256 numSlots) returns(uint256 sum)
func (_LoadSimulator *LoadSimulatorTransactorSession) Read(offset *big.Int, numSlots *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Read(&_LoadSimulator.TransactOpts, offset, numSlots)
}

// Write is a paid mutator transaction binding the contract method 0x9c0e3f7a.
//
// Solidity: function write(uint256 numSlots, uint256 value) returns(bool)
func (_LoadSimulator *LoadSimulatorTransactor) Write(opts *bind.TransactOpts, numSlots *big.Int, value *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.contract.Transact(opts, "write", numSlots, value)
}

// Write is a paid mutator transaction binding the contract method 0x9c0e3f7a.
//
// Solidity: function write(uint256 numSlots, uint256 value) returns(bool)
func (_LoadSimulator *LoadSimulatorSession) Write(numSlots *big.Int, value *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Write(&_LoadSimulator.TransactOpts, numSlots, value)
}

// Write is a paid mutator transaction binding the contract method 0x9c0e3f7a.
//
// Solidity: function write(uint256 numSlots, uint256 value) returns(bool)
func (_LoadSimulator *LoadSimulatorTransactorSession) Write(numSlots *big.Int, value *big.Int) (*types.Transaction, error) {
	return _LoadSimulator.Contract.Write(&_LoadSimulator.TransactOpts, numSlots, value)
}

// LoadSimulatorLargeCalldataIterator is returned from FilterLargeCalldata and is used to iterate over the raw logs and unpacked data for LargeCalldata events raised by the LoadSimulator contract.
type LoadSimulatorLargeCalldataIterator struct {
	Event *LoadSimulatorLargeCalldata // Event containing the contract specifics and raw log

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
func (it *LoadSimulatorLargeCalldataIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(LoadSimulatorLargeCalldata)
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
		it.Event = new(LoadSimulatorLargeCalldata)
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
func (it *LoadSimulatorLargeCalldataIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *LoadSimulatorLargeCalldataIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// LoadSimulatorLargeCalldata represents a LargeCalldata event raised by the LoadSimulator contract.
type LoadSimulatorLargeCalldata struct {
	Arg0 []byte
	Raw  types.Log // Blockchain specific contextual infos
}

// FilterLargeCalldata is a free log retrieval operation binding the contract event 0x7cdeb400b923483a299a592d3b984525e73532d62e6861a342855a80c6a54a31.
//
// Solidity: event LargeCalldata(bytes arg0)
func (_LoadSimulator *LoadSimulatorFilterer) FilterLargeCalldata(opts *bind.FilterOpts) (*LoadSimulatorLargeCalldataIterator, error) {

	logs, sub, err := _LoadSimulator.contract.FilterLogs(opts, "LargeCalldata")
	if err != nil {
		return nil, err
	}
	return &LoadSimulatorLargeCalldataIterator{contract: _LoadSimulator.contract, event: "LargeCalldata", logs: logs, sub: sub}, nil
}

// WatchLargeCalldata is a free log subscription operation binding the contract event 0x7cdeb400b923483a299a592d3b984525e73532d62e6861a342855a80c6a54a31.
//
// Solidity: event LargeCalldata(bytes arg0)
func (_LoadSimulator *LoadSimulatorFilterer) WatchLargeCalldata(opts *bind.WatchOpts, sink chan<- *LoadSimulatorLargeCalldata) (event.Subscription, error) {

	logs, sub, err := _LoadSimulator.contract.WatchLogs(opts, "LargeCalldata")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(LoadSimulatorLargeCalldata)
				if err := _LoadSimulator.contract.UnpackLog(event, "LargeCalldata", log); err != nil {
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

// ParseLargeCalldata is a log parse operation binding the contract event 0x7cdeb400b923483a299a592d3b984525e73532d62e6861a342855a80c6a54a31.
//
// Solidity: event LargeCalldata(bytes arg0)
func (_LoadSimulator *LoadSimulatorFilterer) ParseLargeCalldata(log types.Log) (*LoadSimulatorLargeCalldata, error) {
	event := new(LoadSimulatorLargeCalldata)
	if err := _LoadSimulator.contract.UnpackLog(event, "LargeCalldata", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
