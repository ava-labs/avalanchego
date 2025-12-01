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

// FeeConfig is an auto generated low-level Go binding around an user-defined struct.
type FeeConfig struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}

// ExampleFeeManagerMetaData contains all meta data concerning the ExampleFeeManager contract.
var ExampleFeeManagerMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"OwnableInvalidOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"OwnableUnauthorizedAccount\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"enableCChainFees\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"internalType\":\"structFeeConfig\",\"name\":\"config\",\"type\":\"tuple\"}],\"name\":\"enableCustomFees\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"enableWAGMIFees\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCurrentFeeConfig\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"gasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetBlockRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBaseFee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"targetGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"baseFeeChangeDenominator\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"minBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxBlockGasCost\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"blockGasCostStep\",\"type\":\"uint256\"}],\"internalType\":\"structFeeConfig\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getFeeConfigLastChangedAt\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isAdmin\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isEnabled\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isManager\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"revoke\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405273020000000000000000000000000000000000000360025f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550348015610063575f5ffd5b50730200000000000000000000000000000000000003335f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff16036100ea575f6040517f1e4fbdf70000000000000000000000000000000000000000000000000000000081526004016100e19190610240565b60405180910390fd5b6100f98161014060201b60201c565b508060015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050610259565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61022a82610201565b9050919050565b61023a81610220565b82525050565b5f6020820190506102535f830184610231565b92915050565b611449806102665f395ff3fe608060405234801561000f575f5ffd5b50600436106100f3575f3560e01c806374a8f103116100955780639e05549a116100645780639e05549a14610221578063d0ebdbe71461023f578063f2fde38b1461025b578063f3ae241514610277576100f3565b806374a8f103146101ad57806385c1b4ac146101c95780638da5cb5b146101d35780639015d371146101f1576100f3565b806352965cfc116100d157806352965cfc146101615780636f0edc9d1461017d578063704b6c0214610187578063715018a6146101a3576100f3565b80630aaf7043146100f757806324d7806c1461011357806341f5772814610143575b5f5ffd5b610111600480360381019061010c9190610e9e565b6102a7565b005b61012d60048036038101906101289190610e9e565b6102bb565b60405161013a9190610ee3565b60405180910390f35b61014b610364565b6040516101589190610fb4565b60405180910390f35b61017b6004803603810190610176919061114b565b610451565b005b610185610550565b005b6101a1600480360381019061019c9190610e9e565b610643565b005b6101ab610657565b005b6101c760048036038101906101c29190610e9e565b61066a565b005b6101d161067e565b005b6101db610770565b6040516101e89190611186565b60405180910390f35b61020b60048036038101906102069190610e9e565b610797565b6040516102189190610ee3565b60405180910390f35b610229610840565b60405161023691906111ae565b60405180910390f35b61025960048036038101906102549190610e9e565b6108d4565b005b61027560048036038101906102709190610e9e565b6108e8565b005b610291600480360381019061028c9190610e9e565b61096c565b60405161029e9190610ee3565b60405180910390f35b6102af610a15565b6102b881610a9c565b50565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016103179190611186565b602060405180830381865afa158015610332573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061035691906111db565b905060028114915050919050565b61036c610dfa565b610374610dfa565b60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16635fbbc0d26040518163ffffffff1660e01b815260040161010060405180830381865afa1580156103df573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906104039190611206565b885f01896020018a6040018b6060018c6080018d60a0018e60c0018f60e001888152508881525088815250888152508881525088815250888152508881525050505050505050508091505090565b61045a33610797565b610499576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161049090611311565b60405180910390fd5b60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638f10b586825f015183602001518460400151856060015186608001518760a001518860c001518960e001516040518963ffffffff1660e01b815260040161052098979695949392919061132f565b5f604051808303815f87803b158015610537575f5ffd5b505af1158015610549573d5f5f3e3d5ffd5b5050505050565b61055933610797565b610598576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161058f90611311565b60405180910390fd5b60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638f10b5866301312d006002633b9aca006305f5e10060305f629896806207a1206040518963ffffffff1660e01b815260040161061498979695949392919061132f565b5f604051808303815f87803b15801561062b575f5ffd5b505af115801561063d573d5f5f3e3d5ffd5b50505050565b61064b610a15565b61065481610b26565b50565b61065f610a15565b6106685f610bb0565b565b610672610a15565b61067b81610c71565b50565b61068733610797565b6106c6576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016106bd90611311565b60405180910390fd5b60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638f10b586627a120060026405d21dba0062e4e1c060245f620f4240620186a06040518963ffffffff1660e01b815260040161074198979695949392919061132f565b5f604051808303815f87803b158015610758575f5ffd5b505af115801561076a573d5f5f3e3d5ffd5b50505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016107f39190611186565b602060405180830381865afa15801561080e573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061083291906111db565b90505f811415915050919050565b5f60025f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16639e05549a6040518163ffffffff1660e01b8152600401602060405180830381865afa1580156108ab573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906108cf91906111db565b905090565b6108dc610a15565b6108e581610d69565b50565b6108f0610a15565b5f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1603610960575f6040517f1e4fbdf70000000000000000000000000000000000000000000000000000000081526004016109579190611186565b60405180910390fd5b61096981610bb0565b50565b5f5f60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016109c89190611186565b602060405180830381865afa1580156109e3573d5f5f3e3d5ffd5b505050506040513d601f19601f82011682018060405250810190610a0791906111db565b905060038114915050919050565b610a1d610df3565b73ffffffffffffffffffffffffffffffffffffffff16610a3b610770565b73ffffffffffffffffffffffffffffffffffffffff1614610a9a57610a5e610df3565b6040517f118cdaa7000000000000000000000000000000000000000000000000000000008152600401610a919190611186565b60405180910390fd5b565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630aaf7043826040518263ffffffff1660e01b8152600401610af69190611186565b5f604051808303815f87803b158015610b0d575f5ffd5b505af1158015610b1f573d5f5f3e3d5ffd5b5050505050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663704b6c02826040518263ffffffff1660e01b8152600401610b809190611186565b5f604051808303815f87803b158015610b97575f5ffd5b505af1158015610ba9573d5f5f3e3d5ffd5b5050505050565b5f5f5f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff169050815f5f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b8073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff1603610cdf576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610cd6906113f5565b60405180910390fd5b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638c6bfb3b826040518263ffffffff1660e01b8152600401610d399190611186565b5f604051808303815f87803b158015610d50575f5ffd5b505af1158015610d62573d5f5f3e3d5ffd5b5050505050565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663d0ebdbe7826040518263ffffffff1660e01b8152600401610dc39190611186565b5f604051808303815f87803b158015610dda575f5ffd5b505af1158015610dec573d5f5f3e3d5ffd5b5050505050565b5f33905090565b6040518061010001604052805f81526020015f81526020015f81526020015f81526020015f81526020015f81526020015f81526020015f81525090565b5f604051905090565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f610e6d82610e44565b9050919050565b610e7d81610e63565b8114610e87575f5ffd5b50565b5f81359050610e9881610e74565b92915050565b5f60208284031215610eb357610eb2610e40565b5b5f610ec084828501610e8a565b91505092915050565b5f8115159050919050565b610edd81610ec9565b82525050565b5f602082019050610ef65f830184610ed4565b92915050565b5f819050919050565b610f0e81610efc565b82525050565b61010082015f820151610f295f850182610f05565b506020820151610f3c6020850182610f05565b506040820151610f4f6040850182610f05565b506060820151610f626060850182610f05565b506080820151610f756080850182610f05565b5060a0820151610f8860a0850182610f05565b5060c0820151610f9b60c0850182610f05565b5060e0820151610fae60e0850182610f05565b50505050565b5f61010082019050610fc85f830184610f14565b92915050565b5f5ffd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b61101882610fd2565b810181811067ffffffffffffffff8211171561103757611036610fe2565b5b80604052505050565b5f611049610e37565b9050611055828261100f565b919050565b61106381610efc565b811461106d575f5ffd5b50565b5f8135905061107e8161105a565b92915050565b5f610100828403121561109a57611099610fce565b5b6110a5610100611040565b90505f6110b484828501611070565b5f8301525060206110c784828501611070565b60208301525060406110db84828501611070565b60408301525060606110ef84828501611070565b606083015250608061110384828501611070565b60808301525060a061111784828501611070565b60a08301525060c061112b84828501611070565b60c08301525060e061113f84828501611070565b60e08301525092915050565b5f610100828403121561116157611160610e40565b5b5f61116e84828501611084565b91505092915050565b61118081610e63565b82525050565b5f6020820190506111995f830184611177565b92915050565b6111a881610efc565b82525050565b5f6020820190506111c15f83018461119f565b92915050565b5f815190506111d58161105a565b92915050565b5f602082840312156111f0576111ef610e40565b5b5f6111fd848285016111c7565b91505092915050565b5f5f5f5f5f5f5f5f610100898b03121561122357611222610e40565b5b5f6112308b828c016111c7565b98505060206112418b828c016111c7565b97505060406112528b828c016111c7565b96505060606112638b828c016111c7565b95505060806112748b828c016111c7565b94505060a06112858b828c016111c7565b93505060c06112968b828c016111c7565b92505060e06112a78b828c016111c7565b9150509295985092959890939650565b5f82825260208201905092915050565b7f6e6f7420656e61626c65640000000000000000000000000000000000000000005f82015250565b5f6112fb600b836112b7565b9150611306826112c7565b602082019050919050565b5f6020820190508181035f830152611328816112ef565b9050919050565b5f610100820190506113435f83018b61119f565b611350602083018a61119f565b61135d604083018961119f565b61136a606083018861119f565b611377608083018761119f565b61138460a083018661119f565b61139160c083018561119f565b61139e60e083018461119f565b9998505050505050505050565b7f63616e6e6f74207265766f6b65206f776e20726f6c65000000000000000000005f82015250565b5f6113df6016836112b7565b91506113ea826113ab565b602082019050919050565b5f6020820190508181035f83015261140c816113d3565b905091905056fea2646970667358221220f491e0f301327cca0d1fc34bdf47bd39144581a53eb8595dbdc9c31a6e922fb864736f6c634300081e0033",
}

// ExampleFeeManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use ExampleFeeManagerMetaData.ABI instead.
var ExampleFeeManagerABI = ExampleFeeManagerMetaData.ABI

// ExampleFeeManagerBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ExampleFeeManagerMetaData.Bin instead.
var ExampleFeeManagerBin = ExampleFeeManagerMetaData.Bin

// DeployExampleFeeManager deploys a new Ethereum contract, binding an instance of ExampleFeeManager to it.
func DeployExampleFeeManager(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ExampleFeeManager, error) {
	parsed, err := ExampleFeeManagerMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ExampleFeeManagerBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ExampleFeeManager{ExampleFeeManagerCaller: ExampleFeeManagerCaller{contract: contract}, ExampleFeeManagerTransactor: ExampleFeeManagerTransactor{contract: contract}, ExampleFeeManagerFilterer: ExampleFeeManagerFilterer{contract: contract}}, nil
}

// ExampleFeeManager is an auto generated Go binding around an Ethereum contract.
type ExampleFeeManager struct {
	ExampleFeeManagerCaller     // Read-only binding to the contract
	ExampleFeeManagerTransactor // Write-only binding to the contract
	ExampleFeeManagerFilterer   // Log filterer for contract events
}

// ExampleFeeManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type ExampleFeeManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleFeeManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ExampleFeeManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleFeeManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ExampleFeeManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ExampleFeeManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ExampleFeeManagerSession struct {
	Contract     *ExampleFeeManager // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// ExampleFeeManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ExampleFeeManagerCallerSession struct {
	Contract *ExampleFeeManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// ExampleFeeManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ExampleFeeManagerTransactorSession struct {
	Contract     *ExampleFeeManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// ExampleFeeManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type ExampleFeeManagerRaw struct {
	Contract *ExampleFeeManager // Generic contract binding to access the raw methods on
}

// ExampleFeeManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ExampleFeeManagerCallerRaw struct {
	Contract *ExampleFeeManagerCaller // Generic read-only contract binding to access the raw methods on
}

// ExampleFeeManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ExampleFeeManagerTransactorRaw struct {
	Contract *ExampleFeeManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewExampleFeeManager creates a new instance of ExampleFeeManager, bound to a specific deployed contract.
func NewExampleFeeManager(address common.Address, backend bind.ContractBackend) (*ExampleFeeManager, error) {
	contract, err := bindExampleFeeManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ExampleFeeManager{ExampleFeeManagerCaller: ExampleFeeManagerCaller{contract: contract}, ExampleFeeManagerTransactor: ExampleFeeManagerTransactor{contract: contract}, ExampleFeeManagerFilterer: ExampleFeeManagerFilterer{contract: contract}}, nil
}

// NewExampleFeeManagerCaller creates a new read-only instance of ExampleFeeManager, bound to a specific deployed contract.
func NewExampleFeeManagerCaller(address common.Address, caller bind.ContractCaller) (*ExampleFeeManagerCaller, error) {
	contract, err := bindExampleFeeManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleFeeManagerCaller{contract: contract}, nil
}

// NewExampleFeeManagerTransactor creates a new write-only instance of ExampleFeeManager, bound to a specific deployed contract.
func NewExampleFeeManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*ExampleFeeManagerTransactor, error) {
	contract, err := bindExampleFeeManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ExampleFeeManagerTransactor{contract: contract}, nil
}

// NewExampleFeeManagerFilterer creates a new log filterer instance of ExampleFeeManager, bound to a specific deployed contract.
func NewExampleFeeManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*ExampleFeeManagerFilterer, error) {
	contract, err := bindExampleFeeManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ExampleFeeManagerFilterer{contract: contract}, nil
}

// bindExampleFeeManager binds a generic wrapper to an already deployed contract.
func bindExampleFeeManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ExampleFeeManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleFeeManager *ExampleFeeManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleFeeManager.Contract.ExampleFeeManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleFeeManager *ExampleFeeManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.ExampleFeeManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleFeeManager *ExampleFeeManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.ExampleFeeManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ExampleFeeManager *ExampleFeeManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ExampleFeeManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ExampleFeeManager *ExampleFeeManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ExampleFeeManager *ExampleFeeManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.contract.Transact(opts, method, params...)
}

// GetCurrentFeeConfig is a free data retrieval call binding the contract method 0x41f57728.
//
// Solidity: function getCurrentFeeConfig() view returns((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256))
func (_ExampleFeeManager *ExampleFeeManagerCaller) GetCurrentFeeConfig(opts *bind.CallOpts) (FeeConfig, error) {
	var out []interface{}
	err := _ExampleFeeManager.contract.Call(opts, &out, "getCurrentFeeConfig")

	if err != nil {
		return *new(FeeConfig), err
	}

	out0 := *abi.ConvertType(out[0], new(FeeConfig)).(*FeeConfig)

	return out0, err

}

// GetCurrentFeeConfig is a free data retrieval call binding the contract method 0x41f57728.
//
// Solidity: function getCurrentFeeConfig() view returns((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256))
func (_ExampleFeeManager *ExampleFeeManagerSession) GetCurrentFeeConfig() (FeeConfig, error) {
	return _ExampleFeeManager.Contract.GetCurrentFeeConfig(&_ExampleFeeManager.CallOpts)
}

// GetCurrentFeeConfig is a free data retrieval call binding the contract method 0x41f57728.
//
// Solidity: function getCurrentFeeConfig() view returns((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256))
func (_ExampleFeeManager *ExampleFeeManagerCallerSession) GetCurrentFeeConfig() (FeeConfig, error) {
	return _ExampleFeeManager.Contract.GetCurrentFeeConfig(&_ExampleFeeManager.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_ExampleFeeManager *ExampleFeeManagerCaller) GetFeeConfigLastChangedAt(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ExampleFeeManager.contract.Call(opts, &out, "getFeeConfigLastChangedAt")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_ExampleFeeManager *ExampleFeeManagerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _ExampleFeeManager.Contract.GetFeeConfigLastChangedAt(&_ExampleFeeManager.CallOpts)
}

// GetFeeConfigLastChangedAt is a free data retrieval call binding the contract method 0x9e05549a.
//
// Solidity: function getFeeConfigLastChangedAt() view returns(uint256)
func (_ExampleFeeManager *ExampleFeeManagerCallerSession) GetFeeConfigLastChangedAt() (*big.Int, error) {
	return _ExampleFeeManager.Contract.GetFeeConfigLastChangedAt(&_ExampleFeeManager.CallOpts)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerCaller) IsAdmin(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ExampleFeeManager.contract.Call(opts, &out, "isAdmin", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerSession) IsAdmin(addr common.Address) (bool, error) {
	return _ExampleFeeManager.Contract.IsAdmin(&_ExampleFeeManager.CallOpts, addr)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerCallerSession) IsAdmin(addr common.Address) (bool, error) {
	return _ExampleFeeManager.Contract.IsAdmin(&_ExampleFeeManager.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerCaller) IsEnabled(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ExampleFeeManager.contract.Call(opts, &out, "isEnabled", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerSession) IsEnabled(addr common.Address) (bool, error) {
	return _ExampleFeeManager.Contract.IsEnabled(&_ExampleFeeManager.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerCallerSession) IsEnabled(addr common.Address) (bool, error) {
	return _ExampleFeeManager.Contract.IsEnabled(&_ExampleFeeManager.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerCaller) IsManager(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ExampleFeeManager.contract.Call(opts, &out, "isManager", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerSession) IsManager(addr common.Address) (bool, error) {
	return _ExampleFeeManager.Contract.IsManager(&_ExampleFeeManager.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ExampleFeeManager *ExampleFeeManagerCallerSession) IsManager(addr common.Address) (bool, error) {
	return _ExampleFeeManager.Contract.IsManager(&_ExampleFeeManager.CallOpts, addr)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleFeeManager *ExampleFeeManagerCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ExampleFeeManager.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleFeeManager *ExampleFeeManagerSession) Owner() (common.Address, error) {
	return _ExampleFeeManager.Contract.Owner(&_ExampleFeeManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ExampleFeeManager *ExampleFeeManagerCallerSession) Owner() (common.Address, error) {
	return _ExampleFeeManager.Contract.Owner(&_ExampleFeeManager.CallOpts)
}

// EnableCChainFees is a paid mutator transaction binding the contract method 0x85c1b4ac.
//
// Solidity: function enableCChainFees() returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) EnableCChainFees(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "enableCChainFees")
}

// EnableCChainFees is a paid mutator transaction binding the contract method 0x85c1b4ac.
//
// Solidity: function enableCChainFees() returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) EnableCChainFees() (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.EnableCChainFees(&_ExampleFeeManager.TransactOpts)
}

// EnableCChainFees is a paid mutator transaction binding the contract method 0x85c1b4ac.
//
// Solidity: function enableCChainFees() returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) EnableCChainFees() (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.EnableCChainFees(&_ExampleFeeManager.TransactOpts)
}

// EnableCustomFees is a paid mutator transaction binding the contract method 0x52965cfc.
//
// Solidity: function enableCustomFees((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) config) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) EnableCustomFees(opts *bind.TransactOpts, config FeeConfig) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "enableCustomFees", config)
}

// EnableCustomFees is a paid mutator transaction binding the contract method 0x52965cfc.
//
// Solidity: function enableCustomFees((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) config) returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) EnableCustomFees(config FeeConfig) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.EnableCustomFees(&_ExampleFeeManager.TransactOpts, config)
}

// EnableCustomFees is a paid mutator transaction binding the contract method 0x52965cfc.
//
// Solidity: function enableCustomFees((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256) config) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) EnableCustomFees(config FeeConfig) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.EnableCustomFees(&_ExampleFeeManager.TransactOpts, config)
}

// EnableWAGMIFees is a paid mutator transaction binding the contract method 0x6f0edc9d.
//
// Solidity: function enableWAGMIFees() returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) EnableWAGMIFees(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "enableWAGMIFees")
}

// EnableWAGMIFees is a paid mutator transaction binding the contract method 0x6f0edc9d.
//
// Solidity: function enableWAGMIFees() returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) EnableWAGMIFees() (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.EnableWAGMIFees(&_ExampleFeeManager.TransactOpts)
}

// EnableWAGMIFees is a paid mutator transaction binding the contract method 0x6f0edc9d.
//
// Solidity: function enableWAGMIFees() returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) EnableWAGMIFees() (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.EnableWAGMIFees(&_ExampleFeeManager.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) RenounceOwnership() (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.RenounceOwnership(&_ExampleFeeManager.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.RenounceOwnership(&_ExampleFeeManager.TransactOpts)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) Revoke(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "revoke", addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.Revoke(&_ExampleFeeManager.TransactOpts, addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.Revoke(&_ExampleFeeManager.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.SetAdmin(&_ExampleFeeManager.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.SetAdmin(&_ExampleFeeManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.SetEnabled(&_ExampleFeeManager.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.SetEnabled(&_ExampleFeeManager.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.SetManager(&_ExampleFeeManager.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.SetManager(&_ExampleFeeManager.TransactOpts, addr)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleFeeManager *ExampleFeeManagerSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.TransferOwnership(&_ExampleFeeManager.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ExampleFeeManager *ExampleFeeManagerTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ExampleFeeManager.Contract.TransferOwnership(&_ExampleFeeManager.TransactOpts, newOwner)
}

// ExampleFeeManagerOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the ExampleFeeManager contract.
type ExampleFeeManagerOwnershipTransferredIterator struct {
	Event *ExampleFeeManagerOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *ExampleFeeManagerOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ExampleFeeManagerOwnershipTransferred)
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
		it.Event = new(ExampleFeeManagerOwnershipTransferred)
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
func (it *ExampleFeeManagerOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ExampleFeeManagerOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ExampleFeeManagerOwnershipTransferred represents a OwnershipTransferred event raised by the ExampleFeeManager contract.
type ExampleFeeManagerOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleFeeManager *ExampleFeeManagerFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ExampleFeeManagerOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ExampleFeeManager.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ExampleFeeManagerOwnershipTransferredIterator{contract: _ExampleFeeManager.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleFeeManager *ExampleFeeManagerFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *ExampleFeeManagerOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ExampleFeeManager.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ExampleFeeManagerOwnershipTransferred)
				if err := _ExampleFeeManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ExampleFeeManager *ExampleFeeManagerFilterer) ParseOwnershipTransferred(log types.Log) (*ExampleFeeManagerOwnershipTransferred, error) {
	event := new(ExampleFeeManagerOwnershipTransferred)
	if err := _ExampleFeeManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
