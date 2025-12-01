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

// ERC20NativeMinterMetaData contains all meta data concerning the ERC20NativeMinter contract.
var ERC20NativeMinterMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"initSupply\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"allowance\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"needed\",\"type\":\"uint256\"}],\"name\":\"ERC20InsufficientAllowance\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"needed\",\"type\":\"uint256\"}],\"name\":\"ERC20InsufficientBalance\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"approver\",\"type\":\"address\"}],\"name\":\"ERC20InvalidApprover\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"receiver\",\"type\":\"address\"}],\"name\":\"ERC20InvalidReceiver\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"ERC20InvalidSender\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"ERC20InvalidSpender\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"OwnableInvalidOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"OwnableUnauthorizedAccount\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"dst\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"wad\",\"type\":\"uint256\"}],\"name\":\"Deposit\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"src\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"wad\",\"type\":\"uint256\"}],\"name\":\"Mintdrawal\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"burn\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"deposit\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isAdmin\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isEnabled\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"isManager\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"mint\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"wad\",\"type\":\"uint256\"}],\"name\":\"mintdraw\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"revoke\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405273020000000000000000000000000000000000000160075f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550348015610063575f5ffd5b506040516125fc3803806125fc833981810160405281019061008591906105ac565b730200000000000000000000000000000000000001336040518060400160405280601681526020017f45524332304e61746976654d696e746572546f6b656e000000000000000000008152506040518060400160405280600481526020017f584d504c000000000000000000000000000000000000000000000000000000008152508160039081610116919061080b565b508060049081610126919061080b565b5050505f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1603610199575f6040517f1e4fbdf70000000000000000000000000000000000000000000000000000000081526004016101909190610919565b60405180910390fd5b6101a88161020d60201b60201c565b508060065f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550506102076101fb6102d060201b60201c565b826102d760201b60201c565b506109ef565b5f60055f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690508160055f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b5f33905090565b5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff1603610347575f6040517fec442f0500000000000000000000000000000000000000000000000000000000815260040161033e9190610919565b60405180910390fd5b6103585f838361035c60201b60201c565b5050565b5f73ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff16036103ac578060025f8282546103a0919061095f565b9250508190555061047a565b5f5f5f8573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2054905081811015610435578381836040517fe450d38c00000000000000000000000000000000000000000000000000000000815260040161042c939291906109a1565b60405180910390fd5b8181035f5f8673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550505b5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff16036104c1578060025f828254039250508190555061050b565b805f5f8473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205f82825401925050819055505b8173ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef8360405161056891906109d6565b60405180910390a3505050565b5f5ffd5b5f819050919050565b61058b81610579565b8114610595575f5ffd5b50565b5f815190506105a681610582565b92915050565b5f602082840312156105c1576105c0610575565b5b5f6105ce84828501610598565b91505092915050565b5f81519050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52602260045260245ffd5b5f600282049050600182168061065257607f821691505b6020821081036106655761066461060e565b5b50919050565b5f819050815f5260205f209050919050565b5f6020601f8301049050919050565b5f82821b905092915050565b5f600883026106c77fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff8261068c565b6106d1868361068c565b95508019841693508086168417925050509392505050565b5f819050919050565b5f61070c61070761070284610579565b6106e9565b610579565b9050919050565b5f819050919050565b610725836106f2565b61073961073182610713565b848454610698565b825550505050565b5f5f905090565b610750610741565b61075b81848461071c565b505050565b5b8181101561077e576107735f82610748565b600181019050610761565b5050565b601f8211156107c3576107948161066b565b61079d8461067d565b810160208510156107ac578190505b6107c06107b88561067d565b830182610760565b50505b505050565b5f82821c905092915050565b5f6107e35f19846008026107c8565b1980831691505092915050565b5f6107fb83836107d4565b9150826002028217905092915050565b610814826105d7565b67ffffffffffffffff81111561082d5761082c6105e1565b5b610837825461063b565b610842828285610782565b5f60209050601f831160018114610873575f8415610861578287015190505b61086b85826107f0565b8655506108d2565b601f1984166108818661066b565b5f5b828110156108a857848901518255600182019150602085019450602081019050610883565b868310156108c557848901516108c1601f8916826107d4565b8355505b6001600288020188555050505b505050505050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f610903826108da565b9050919050565b610913816108f9565b82525050565b5f60208201905061092c5f83018461090a565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f61096982610579565b915061097483610579565b925082820190508082111561098c5761098b610932565b5b92915050565b61099b81610579565b82525050565b5f6060820190506109b45f83018661090a565b6109c16020830185610992565b6109ce6040830184610992565b949350505050565b5f6020820190506109e95f830184610992565b92915050565b611c00806109fc5f395ff3fe60806040526004361061013f575f3560e01c8063715018a6116100b5578063a9059cbb1161006e578063a9059cbb14610447578063d0e30db014610483578063d0ebdbe71461048d578063dd62ed3e146104b5578063f2fde38b146104f1578063f3ae2415146105195761013f565b8063715018a61461035157806374a8f103146103675780638da5cb5b1461038f5780639015d371146103b957806395d89b41146103f55780639dc29fac1461041f5761013f565b806323b872dd1161010757806323b872dd1461022357806324d7806c1461025f578063313ce5671461029b57806340c10f19146102c5578063704b6c02146102ed57806370a08231146103155761013f565b80630356b6cd1461014357806306fdde031461016b578063095ea7b3146101955780630aaf7043146101d157806318160ddd146101f9575b5f5ffd5b34801561014e575f5ffd5b5061016960048036038101906101649190611747565b610555565b005b348015610176575f5ffd5b5061017f61064e565b60405161018c91906117e2565b60405180910390f35b3480156101a0575f5ffd5b506101bb60048036038101906101b6919061185c565b6106de565b6040516101c891906118b4565b60405180910390f35b3480156101dc575f5ffd5b506101f760048036038101906101f291906118cd565b610700565b005b348015610204575f5ffd5b5061020d610714565b60405161021a9190611907565b60405180910390f35b34801561022e575f5ffd5b5061024960048036038101906102449190611920565b61071d565b60405161025691906118b4565b60405180910390f35b34801561026a575f5ffd5b50610285600480360381019061028091906118cd565b61074b565b60405161029291906118b4565b60405180910390f35b3480156102a6575f5ffd5b506102af6107f4565b6040516102bc919061198b565b60405180910390f35b3480156102d0575f5ffd5b506102eb60048036038101906102e6919061185c565b6107fc565b005b3480156102f8575f5ffd5b50610313600480360381019061030e91906118cd565b610812565b005b348015610320575f5ffd5b5061033b600480360381019061033691906118cd565b610826565b6040516103489190611907565b60405180910390f35b34801561035c575f5ffd5b5061036561086b565b005b348015610372575f5ffd5b5061038d600480360381019061038891906118cd565b61087e565b005b34801561039a575f5ffd5b506103a3610892565b6040516103b091906119b3565b60405180910390f35b3480156103c4575f5ffd5b506103df60048036038101906103da91906118cd565b6108ba565b6040516103ec91906118b4565b60405180910390f35b348015610400575f5ffd5b50610409610963565b60405161041691906117e2565b60405180910390f35b34801561042a575f5ffd5b506104456004803603810190610440919061185c565b6109f3565b005b348015610452575f5ffd5b5061046d6004803603810190610468919061185c565b610a09565b60405161047a91906118b4565b60405180910390f35b61048b610a2b565b005b348015610498575f5ffd5b506104b360048036038101906104ae91906118cd565b610aeb565b005b3480156104c0575f5ffd5b506104db60048036038101906104d691906119cc565b610aff565b6040516104e89190611907565b60405180910390f35b3480156104fc575f5ffd5b50610517600480360381019061051291906118cd565b610b81565b005b348015610524575f5ffd5b5061053f600480360381019061053a91906118cd565b610c05565b60405161054c91906118b4565b60405180910390f35b610566610560610cae565b82610cb5565b60075f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16634f5aaaba6105ab610cae565b836040518363ffffffff1660e01b81526004016105c9929190611a0a565b5f604051808303815f87803b1580156105e0575f5ffd5b505af11580156105f2573d5f5f3e3d5ffd5b505050506105fe610cae565b73ffffffffffffffffffffffffffffffffffffffff167f25bedde6c8ebd3a89b719a16299dbfe271c7bffa42fe1ac1a52e15ab0cb767e6826040516106439190611907565b60405180910390a250565b60606003805461065d90611a5e565b80601f016020809104026020016040519081016040528092919081815260200182805461068990611a5e565b80156106d45780601f106106ab576101008083540402835291602001916106d4565b820191905f5260205f20905b8154815290600101906020018083116106b757829003601f168201915b5050505050905090565b5f5f6106e8610cae565b90506106f5818585610d34565b600191505092915050565b610708610d46565b61071181610dcd565b50565b5f600254905090565b5f5f610727610cae565b9050610734858285610e57565b61073f858585610eea565b60019150509392505050565b5f5f60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b81526004016107a791906119b3565b602060405180830381865afa1580156107c2573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906107e69190611aa2565b905060028114915050919050565b5f6012905090565b610804610d46565b61080e8282610fda565b5050565b61081a610d46565b61082381611059565b50565b5f5f5f8373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f20549050919050565b610873610d46565b61087c5f6110e3565b565b610886610d46565b61088f816111a6565b50565b5f60055f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b5f5f60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b815260040161091691906119b3565b602060405180830381865afa158015610931573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906109559190611aa2565b90505f811415915050919050565b60606004805461097290611a5e565b80601f016020809104026020016040519081016040528092919081815260200182805461099e90611a5e565b80156109e95780601f106109c0576101008083540402835291602001916109e9565b820191905f5260205f20905b8154815290600101906020018083116109cc57829003601f168201915b5050505050905090565b6109fb610d46565b610a058282610cb5565b5050565b5f5f610a13610cae565b9050610a20818585610eea565b600191505092915050565b73010000000000000000000000000000000000000073ffffffffffffffffffffffffffffffffffffffff166108fc3490811502906040515f60405180830381858888f19350505050158015610a82573d5f5f3e3d5ffd5b50610a94610a8e610cae565b34610fda565b610a9c610cae565b73ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c34604051610ae19190611907565b60405180910390a2565b610af3610d46565b610afc8161129e565b50565b5f60015f8473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205f8373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2054905092915050565b610b89610d46565b5f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1603610bf9575f6040517f1e4fbdf7000000000000000000000000000000000000000000000000000000008152600401610bf091906119b3565b60405180910390fd5b610c02816110e3565b50565b5f5f60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663eb54dae1846040518263ffffffff1660e01b8152600401610c6191906119b3565b602060405180830381865afa158015610c7c573d5f5f3e3d5ffd5b505050506040513d601f19601f82011682018060405250810190610ca09190611aa2565b905060038114915050919050565b5f33905090565b5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff1603610d25575f6040517f96c6fd1e000000000000000000000000000000000000000000000000000000008152600401610d1c91906119b3565b60405180910390fd5b610d30825f83611328565b5050565b610d418383836001611541565b505050565b610d4e610cae565b73ffffffffffffffffffffffffffffffffffffffff16610d6c610892565b73ffffffffffffffffffffffffffffffffffffffff1614610dcb57610d8f610cae565b6040517f118cdaa7000000000000000000000000000000000000000000000000000000008152600401610dc291906119b3565b60405180910390fd5b565b60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16630aaf7043826040518263ffffffff1660e01b8152600401610e2791906119b3565b5f604051808303815f87803b158015610e3e575f5ffd5b505af1158015610e50573d5f5f3e3d5ffd5b5050505050565b5f610e628484610aff565b90507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff811015610ee45781811015610ed5578281836040517ffb8f41b2000000000000000000000000000000000000000000000000000000008152600401610ecc93929190611acd565b60405180910390fd5b610ee384848484035f611541565b5b50505050565b5f73ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff1603610f5a575f6040517f96c6fd1e000000000000000000000000000000000000000000000000000000008152600401610f5191906119b3565b60405180910390fd5b5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff1603610fca575f6040517fec442f05000000000000000000000000000000000000000000000000000000008152600401610fc191906119b3565b60405180910390fd5b610fd5838383611328565b505050565b5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff160361104a575f6040517fec442f0500000000000000000000000000000000000000000000000000000000815260040161104191906119b3565b60405180910390fd5b6110555f8383611328565b5050565b60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663704b6c02826040518263ffffffff1660e01b81526004016110b391906119b3565b5f604051808303815f87803b1580156110ca575f5ffd5b505af11580156110dc573d5f5f3e3d5ffd5b5050505050565b5f60055f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690508160055f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff167f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e060405160405180910390a35050565b8073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff1603611214576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161120b90611b4c565b60405180910390fd5b60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16638c6bfb3b826040518263ffffffff1660e01b815260040161126e91906119b3565b5f604051808303815f87803b158015611285575f5ffd5b505af1158015611297573d5f5f3e3d5ffd5b5050505050565b60065f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663d0ebdbe7826040518263ffffffff1660e01b81526004016112f891906119b3565b5f604051808303815f87803b15801561130f575f5ffd5b505af1158015611321573d5f5f3e3d5ffd5b5050505050565b5f73ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff1603611378578060025f82825461136c9190611b97565b92505081905550611446565b5f5f5f8573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2054905081811015611401578381836040517fe450d38c0000000000000000000000000000000000000000000000000000000081526004016113f893929190611acd565b60405180910390fd5b8181035f5f8673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550505b5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff160361148d578060025f82825403925050819055506114d7565b805f5f8473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205f82825401925050819055505b8173ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef836040516115349190611907565b60405180910390a3505050565b5f73ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16036115b1575f6040517fe602df050000000000000000000000000000000000000000000000000000000081526004016115a891906119b3565b60405180910390fd5b5f73ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff1603611621575f6040517f94280d6200000000000000000000000000000000000000000000000000000000815260040161161891906119b3565b60405180910390fd5b8160015f8673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205f8573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550801561170a578273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040516117019190611907565b60405180910390a35b50505050565b5f5ffd5b5f819050919050565b61172681611714565b8114611730575f5ffd5b50565b5f813590506117418161171d565b92915050565b5f6020828403121561175c5761175b611710565b5b5f61176984828501611733565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f6117b482611772565b6117be818561177c565b93506117ce81856020860161178c565b6117d78161179a565b840191505092915050565b5f6020820190508181035f8301526117fa81846117aa565b905092915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61182b82611802565b9050919050565b61183b81611821565b8114611845575f5ffd5b50565b5f8135905061185681611832565b92915050565b5f5f6040838503121561187257611871611710565b5b5f61187f85828601611848565b925050602061189085828601611733565b9150509250929050565b5f8115159050919050565b6118ae8161189a565b82525050565b5f6020820190506118c75f8301846118a5565b92915050565b5f602082840312156118e2576118e1611710565b5b5f6118ef84828501611848565b91505092915050565b61190181611714565b82525050565b5f60208201905061191a5f8301846118f8565b92915050565b5f5f5f6060848603121561193757611936611710565b5b5f61194486828701611848565b935050602061195586828701611848565b925050604061196686828701611733565b9150509250925092565b5f60ff82169050919050565b61198581611970565b82525050565b5f60208201905061199e5f83018461197c565b92915050565b6119ad81611821565b82525050565b5f6020820190506119c65f8301846119a4565b92915050565b5f5f604083850312156119e2576119e1611710565b5b5f6119ef85828601611848565b9250506020611a0085828601611848565b9150509250929050565b5f604082019050611a1d5f8301856119a4565b611a2a60208301846118f8565b9392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52602260045260245ffd5b5f6002820490506001821680611a7557607f821691505b602082108103611a8857611a87611a31565b5b50919050565b5f81519050611a9c8161171d565b92915050565b5f60208284031215611ab757611ab6611710565b5b5f611ac484828501611a8e565b91505092915050565b5f606082019050611ae05f8301866119a4565b611aed60208301856118f8565b611afa60408301846118f8565b949350505050565b7f63616e6e6f74207265766f6b65206f776e20726f6c65000000000000000000005f82015250565b5f611b3660168361177c565b9150611b4182611b02565b602082019050919050565b5f6020820190508181035f830152611b6381611b2a565b9050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f611ba182611714565b9150611bac83611714565b9250828201905080821115611bc457611bc3611b6a565b5b9291505056fea2646970667358221220fca92aaeb8dc3d59a0a4fbaa4c4cb6ff44ab43947a730765492db15ebc96e77364736f6c634300081e0033",
}

// ERC20NativeMinterABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20NativeMinterMetaData.ABI instead.
var ERC20NativeMinterABI = ERC20NativeMinterMetaData.ABI

// ERC20NativeMinterBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ERC20NativeMinterMetaData.Bin instead.
var ERC20NativeMinterBin = ERC20NativeMinterMetaData.Bin

// DeployERC20NativeMinter deploys a new Ethereum contract, binding an instance of ERC20NativeMinter to it.
func DeployERC20NativeMinter(auth *bind.TransactOpts, backend bind.ContractBackend, initSupply *big.Int) (common.Address, *types.Transaction, *ERC20NativeMinter, error) {
	parsed, err := ERC20NativeMinterMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ERC20NativeMinterBin), backend, initSupply)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ERC20NativeMinter{ERC20NativeMinterCaller: ERC20NativeMinterCaller{contract: contract}, ERC20NativeMinterTransactor: ERC20NativeMinterTransactor{contract: contract}, ERC20NativeMinterFilterer: ERC20NativeMinterFilterer{contract: contract}}, nil
}

// ERC20NativeMinter is an auto generated Go binding around an Ethereum contract.
type ERC20NativeMinter struct {
	ERC20NativeMinterCaller     // Read-only binding to the contract
	ERC20NativeMinterTransactor // Write-only binding to the contract
	ERC20NativeMinterFilterer   // Log filterer for contract events
}

// ERC20NativeMinterCaller is an auto generated read-only Go binding around an Ethereum contract.
type ERC20NativeMinterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20NativeMinterTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ERC20NativeMinterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20NativeMinterFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ERC20NativeMinterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20NativeMinterSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ERC20NativeMinterSession struct {
	Contract     *ERC20NativeMinter // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// ERC20NativeMinterCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ERC20NativeMinterCallerSession struct {
	Contract *ERC20NativeMinterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// ERC20NativeMinterTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ERC20NativeMinterTransactorSession struct {
	Contract     *ERC20NativeMinterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// ERC20NativeMinterRaw is an auto generated low-level Go binding around an Ethereum contract.
type ERC20NativeMinterRaw struct {
	Contract *ERC20NativeMinter // Generic contract binding to access the raw methods on
}

// ERC20NativeMinterCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ERC20NativeMinterCallerRaw struct {
	Contract *ERC20NativeMinterCaller // Generic read-only contract binding to access the raw methods on
}

// ERC20NativeMinterTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ERC20NativeMinterTransactorRaw struct {
	Contract *ERC20NativeMinterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewERC20NativeMinter creates a new instance of ERC20NativeMinter, bound to a specific deployed contract.
func NewERC20NativeMinter(address common.Address, backend bind.ContractBackend) (*ERC20NativeMinter, error) {
	contract, err := bindERC20NativeMinter(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinter{ERC20NativeMinterCaller: ERC20NativeMinterCaller{contract: contract}, ERC20NativeMinterTransactor: ERC20NativeMinterTransactor{contract: contract}, ERC20NativeMinterFilterer: ERC20NativeMinterFilterer{contract: contract}}, nil
}

// NewERC20NativeMinterCaller creates a new read-only instance of ERC20NativeMinter, bound to a specific deployed contract.
func NewERC20NativeMinterCaller(address common.Address, caller bind.ContractCaller) (*ERC20NativeMinterCaller, error) {
	contract, err := bindERC20NativeMinter(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterCaller{contract: contract}, nil
}

// NewERC20NativeMinterTransactor creates a new write-only instance of ERC20NativeMinter, bound to a specific deployed contract.
func NewERC20NativeMinterTransactor(address common.Address, transactor bind.ContractTransactor) (*ERC20NativeMinterTransactor, error) {
	contract, err := bindERC20NativeMinter(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterTransactor{contract: contract}, nil
}

// NewERC20NativeMinterFilterer creates a new log filterer instance of ERC20NativeMinter, bound to a specific deployed contract.
func NewERC20NativeMinterFilterer(address common.Address, filterer bind.ContractFilterer) (*ERC20NativeMinterFilterer, error) {
	contract, err := bindERC20NativeMinter(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterFilterer{contract: contract}, nil
}

// bindERC20NativeMinter binds a generic wrapper to an already deployed contract.
func bindERC20NativeMinter(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ERC20NativeMinterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20NativeMinter *ERC20NativeMinterRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20NativeMinter.Contract.ERC20NativeMinterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20NativeMinter *ERC20NativeMinterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.ERC20NativeMinterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20NativeMinter *ERC20NativeMinterRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.ERC20NativeMinterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20NativeMinter *ERC20NativeMinterCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20NativeMinter.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20NativeMinter *ERC20NativeMinterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20NativeMinter *ERC20NativeMinterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.contract.Transact(opts, method, params...)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ERC20NativeMinter.Contract.Allowance(&_ERC20NativeMinter.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ERC20NativeMinter.Contract.Allowance(&_ERC20NativeMinter.CallOpts, owner, spender)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ERC20NativeMinter.Contract.BalanceOf(&_ERC20NativeMinter.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ERC20NativeMinter.Contract.BalanceOf(&_ERC20NativeMinter.CallOpts, account)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Decimals() (uint8, error) {
	return _ERC20NativeMinter.Contract.Decimals(&_ERC20NativeMinter.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) Decimals() (uint8, error) {
	return _ERC20NativeMinter.Contract.Decimals(&_ERC20NativeMinter.CallOpts)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) IsAdmin(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "isAdmin", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterSession) IsAdmin(addr common.Address) (bool, error) {
	return _ERC20NativeMinter.Contract.IsAdmin(&_ERC20NativeMinter.CallOpts, addr)
}

// IsAdmin is a free data retrieval call binding the contract method 0x24d7806c.
//
// Solidity: function isAdmin(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) IsAdmin(addr common.Address) (bool, error) {
	return _ERC20NativeMinter.Contract.IsAdmin(&_ERC20NativeMinter.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) IsEnabled(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "isEnabled", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterSession) IsEnabled(addr common.Address) (bool, error) {
	return _ERC20NativeMinter.Contract.IsEnabled(&_ERC20NativeMinter.CallOpts, addr)
}

// IsEnabled is a free data retrieval call binding the contract method 0x9015d371.
//
// Solidity: function isEnabled(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) IsEnabled(addr common.Address) (bool, error) {
	return _ERC20NativeMinter.Contract.IsEnabled(&_ERC20NativeMinter.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) IsManager(opts *bind.CallOpts, addr common.Address) (bool, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "isManager", addr)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterSession) IsManager(addr common.Address) (bool, error) {
	return _ERC20NativeMinter.Contract.IsManager(&_ERC20NativeMinter.CallOpts, addr)
}

// IsManager is a free data retrieval call binding the contract method 0xf3ae2415.
//
// Solidity: function isManager(address addr) view returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) IsManager(addr common.Address) (bool, error) {
	return _ERC20NativeMinter.Contract.IsManager(&_ERC20NativeMinter.CallOpts, addr)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Name() (string, error) {
	return _ERC20NativeMinter.Contract.Name(&_ERC20NativeMinter.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) Name() (string, error) {
	return _ERC20NativeMinter.Contract.Name(&_ERC20NativeMinter.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Owner() (common.Address, error) {
	return _ERC20NativeMinter.Contract.Owner(&_ERC20NativeMinter.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) Owner() (common.Address, error) {
	return _ERC20NativeMinter.Contract.Owner(&_ERC20NativeMinter.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Symbol() (string, error) {
	return _ERC20NativeMinter.Contract.Symbol(&_ERC20NativeMinter.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) Symbol() (string, error) {
	return _ERC20NativeMinter.Contract.Symbol(&_ERC20NativeMinter.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20NativeMinter.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterSession) TotalSupply() (*big.Int, error) {
	return _ERC20NativeMinter.Contract.TotalSupply(&_ERC20NativeMinter.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20NativeMinter *ERC20NativeMinterCallerSession) TotalSupply() (*big.Int, error) {
	return _ERC20NativeMinter.Contract.TotalSupply(&_ERC20NativeMinter.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Approve(opts *bind.TransactOpts, spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "approve", spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Approve(&_ERC20NativeMinter.TransactOpts, spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Approve(&_ERC20NativeMinter.TransactOpts, spender, value)
}

// Burn is a paid mutator transaction binding the contract method 0x9dc29fac.
//
// Solidity: function burn(address from, uint256 amount) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Burn(opts *bind.TransactOpts, from common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "burn", from, amount)
}

// Burn is a paid mutator transaction binding the contract method 0x9dc29fac.
//
// Solidity: function burn(address from, uint256 amount) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) Burn(from common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Burn(&_ERC20NativeMinter.TransactOpts, from, amount)
}

// Burn is a paid mutator transaction binding the contract method 0x9dc29fac.
//
// Solidity: function burn(address from, uint256 amount) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Burn(from common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Burn(&_ERC20NativeMinter.TransactOpts, from, amount)
}

// Deposit is a paid mutator transaction binding the contract method 0xd0e30db0.
//
// Solidity: function deposit() payable returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Deposit(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "deposit")
}

// Deposit is a paid mutator transaction binding the contract method 0xd0e30db0.
//
// Solidity: function deposit() payable returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) Deposit() (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Deposit(&_ERC20NativeMinter.TransactOpts)
}

// Deposit is a paid mutator transaction binding the contract method 0xd0e30db0.
//
// Solidity: function deposit() payable returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Deposit() (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Deposit(&_ERC20NativeMinter.TransactOpts)
}

// Mint is a paid mutator transaction binding the contract method 0x40c10f19.
//
// Solidity: function mint(address to, uint256 amount) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Mint(opts *bind.TransactOpts, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "mint", to, amount)
}

// Mint is a paid mutator transaction binding the contract method 0x40c10f19.
//
// Solidity: function mint(address to, uint256 amount) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) Mint(to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Mint(&_ERC20NativeMinter.TransactOpts, to, amount)
}

// Mint is a paid mutator transaction binding the contract method 0x40c10f19.
//
// Solidity: function mint(address to, uint256 amount) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Mint(to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Mint(&_ERC20NativeMinter.TransactOpts, to, amount)
}

// Mintdraw is a paid mutator transaction binding the contract method 0x0356b6cd.
//
// Solidity: function mintdraw(uint256 wad) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Mintdraw(opts *bind.TransactOpts, wad *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "mintdraw", wad)
}

// Mintdraw is a paid mutator transaction binding the contract method 0x0356b6cd.
//
// Solidity: function mintdraw(uint256 wad) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) Mintdraw(wad *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Mintdraw(&_ERC20NativeMinter.TransactOpts, wad)
}

// Mintdraw is a paid mutator transaction binding the contract method 0x0356b6cd.
//
// Solidity: function mintdraw(uint256 wad) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Mintdraw(wad *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Mintdraw(&_ERC20NativeMinter.TransactOpts, wad)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) RenounceOwnership() (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.RenounceOwnership(&_ERC20NativeMinter.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.RenounceOwnership(&_ERC20NativeMinter.TransactOpts)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Revoke(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "revoke", addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Revoke(&_ERC20NativeMinter.TransactOpts, addr)
}

// Revoke is a paid mutator transaction binding the contract method 0x74a8f103.
//
// Solidity: function revoke(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Revoke(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Revoke(&_ERC20NativeMinter.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) SetAdmin(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "setAdmin", addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.SetAdmin(&_ERC20NativeMinter.TransactOpts, addr)
}

// SetAdmin is a paid mutator transaction binding the contract method 0x704b6c02.
//
// Solidity: function setAdmin(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) SetAdmin(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.SetAdmin(&_ERC20NativeMinter.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) SetEnabled(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "setEnabled", addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.SetEnabled(&_ERC20NativeMinter.TransactOpts, addr)
}

// SetEnabled is a paid mutator transaction binding the contract method 0x0aaf7043.
//
// Solidity: function setEnabled(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) SetEnabled(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.SetEnabled(&_ERC20NativeMinter.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) SetManager(opts *bind.TransactOpts, addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "setManager", addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.SetManager(&_ERC20NativeMinter.TransactOpts, addr)
}

// SetManager is a paid mutator transaction binding the contract method 0xd0ebdbe7.
//
// Solidity: function setManager(address addr) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) SetManager(addr common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.SetManager(&_ERC20NativeMinter.TransactOpts, addr)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) Transfer(opts *bind.TransactOpts, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "transfer", to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Transfer(&_ERC20NativeMinter.TransactOpts, to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.Transfer(&_ERC20NativeMinter.TransactOpts, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "transferFrom", from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.TransferFrom(&_ERC20NativeMinter.TransactOpts, from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.TransferFrom(&_ERC20NativeMinter.TransactOpts, from, to, value)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ERC20NativeMinter *ERC20NativeMinterSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.TransferOwnership(&_ERC20NativeMinter.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ERC20NativeMinter *ERC20NativeMinterTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ERC20NativeMinter.Contract.TransferOwnership(&_ERC20NativeMinter.TransactOpts, newOwner)
}

// ERC20NativeMinterApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the ERC20NativeMinter contract.
type ERC20NativeMinterApprovalIterator struct {
	Event *ERC20NativeMinterApproval // Event containing the contract specifics and raw log

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
func (it *ERC20NativeMinterApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NativeMinterApproval)
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
		it.Event = new(ERC20NativeMinterApproval)
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
func (it *ERC20NativeMinterApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NativeMinterApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NativeMinterApproval represents a Approval event raised by the ERC20NativeMinter contract.
type ERC20NativeMinterApproval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*ERC20NativeMinterApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterApprovalIterator{contract: _ERC20NativeMinter.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *ERC20NativeMinterApproval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NativeMinterApproval)
				if err := _ERC20NativeMinter.contract.UnpackLog(event, "Approval", log); err != nil {
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

// ParseApproval is a log parse operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) ParseApproval(log types.Log) (*ERC20NativeMinterApproval, error) {
	event := new(ERC20NativeMinterApproval)
	if err := _ERC20NativeMinter.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20NativeMinterDepositIterator is returned from FilterDeposit and is used to iterate over the raw logs and unpacked data for Deposit events raised by the ERC20NativeMinter contract.
type ERC20NativeMinterDepositIterator struct {
	Event *ERC20NativeMinterDeposit // Event containing the contract specifics and raw log

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
func (it *ERC20NativeMinterDepositIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NativeMinterDeposit)
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
		it.Event = new(ERC20NativeMinterDeposit)
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
func (it *ERC20NativeMinterDepositIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NativeMinterDepositIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NativeMinterDeposit represents a Deposit event raised by the ERC20NativeMinter contract.
type ERC20NativeMinterDeposit struct {
	Dst common.Address
	Wad *big.Int
	Raw types.Log // Blockchain specific contextual infos
}

// FilterDeposit is a free log retrieval operation binding the contract event 0xe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c.
//
// Solidity: event Deposit(address indexed dst, uint256 wad)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) FilterDeposit(opts *bind.FilterOpts, dst []common.Address) (*ERC20NativeMinterDepositIterator, error) {

	var dstRule []interface{}
	for _, dstItem := range dst {
		dstRule = append(dstRule, dstItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.FilterLogs(opts, "Deposit", dstRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterDepositIterator{contract: _ERC20NativeMinter.contract, event: "Deposit", logs: logs, sub: sub}, nil
}

// WatchDeposit is a free log subscription operation binding the contract event 0xe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c.
//
// Solidity: event Deposit(address indexed dst, uint256 wad)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) WatchDeposit(opts *bind.WatchOpts, sink chan<- *ERC20NativeMinterDeposit, dst []common.Address) (event.Subscription, error) {

	var dstRule []interface{}
	for _, dstItem := range dst {
		dstRule = append(dstRule, dstItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.WatchLogs(opts, "Deposit", dstRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NativeMinterDeposit)
				if err := _ERC20NativeMinter.contract.UnpackLog(event, "Deposit", log); err != nil {
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

// ParseDeposit is a log parse operation binding the contract event 0xe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c.
//
// Solidity: event Deposit(address indexed dst, uint256 wad)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) ParseDeposit(log types.Log) (*ERC20NativeMinterDeposit, error) {
	event := new(ERC20NativeMinterDeposit)
	if err := _ERC20NativeMinter.contract.UnpackLog(event, "Deposit", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20NativeMinterMintdrawalIterator is returned from FilterMintdrawal and is used to iterate over the raw logs and unpacked data for Mintdrawal events raised by the ERC20NativeMinter contract.
type ERC20NativeMinterMintdrawalIterator struct {
	Event *ERC20NativeMinterMintdrawal // Event containing the contract specifics and raw log

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
func (it *ERC20NativeMinterMintdrawalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NativeMinterMintdrawal)
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
		it.Event = new(ERC20NativeMinterMintdrawal)
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
func (it *ERC20NativeMinterMintdrawalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NativeMinterMintdrawalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NativeMinterMintdrawal represents a Mintdrawal event raised by the ERC20NativeMinter contract.
type ERC20NativeMinterMintdrawal struct {
	Src common.Address
	Wad *big.Int
	Raw types.Log // Blockchain specific contextual infos
}

// FilterMintdrawal is a free log retrieval operation binding the contract event 0x25bedde6c8ebd3a89b719a16299dbfe271c7bffa42fe1ac1a52e15ab0cb767e6.
//
// Solidity: event Mintdrawal(address indexed src, uint256 wad)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) FilterMintdrawal(opts *bind.FilterOpts, src []common.Address) (*ERC20NativeMinterMintdrawalIterator, error) {

	var srcRule []interface{}
	for _, srcItem := range src {
		srcRule = append(srcRule, srcItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.FilterLogs(opts, "Mintdrawal", srcRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterMintdrawalIterator{contract: _ERC20NativeMinter.contract, event: "Mintdrawal", logs: logs, sub: sub}, nil
}

// WatchMintdrawal is a free log subscription operation binding the contract event 0x25bedde6c8ebd3a89b719a16299dbfe271c7bffa42fe1ac1a52e15ab0cb767e6.
//
// Solidity: event Mintdrawal(address indexed src, uint256 wad)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) WatchMintdrawal(opts *bind.WatchOpts, sink chan<- *ERC20NativeMinterMintdrawal, src []common.Address) (event.Subscription, error) {

	var srcRule []interface{}
	for _, srcItem := range src {
		srcRule = append(srcRule, srcItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.WatchLogs(opts, "Mintdrawal", srcRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NativeMinterMintdrawal)
				if err := _ERC20NativeMinter.contract.UnpackLog(event, "Mintdrawal", log); err != nil {
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

// ParseMintdrawal is a log parse operation binding the contract event 0x25bedde6c8ebd3a89b719a16299dbfe271c7bffa42fe1ac1a52e15ab0cb767e6.
//
// Solidity: event Mintdrawal(address indexed src, uint256 wad)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) ParseMintdrawal(log types.Log) (*ERC20NativeMinterMintdrawal, error) {
	event := new(ERC20NativeMinterMintdrawal)
	if err := _ERC20NativeMinter.contract.UnpackLog(event, "Mintdrawal", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20NativeMinterOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the ERC20NativeMinter contract.
type ERC20NativeMinterOwnershipTransferredIterator struct {
	Event *ERC20NativeMinterOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *ERC20NativeMinterOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NativeMinterOwnershipTransferred)
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
		it.Event = new(ERC20NativeMinterOwnershipTransferred)
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
func (it *ERC20NativeMinterOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NativeMinterOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NativeMinterOwnershipTransferred represents a OwnershipTransferred event raised by the ERC20NativeMinter contract.
type ERC20NativeMinterOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ERC20NativeMinterOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterOwnershipTransferredIterator{contract: _ERC20NativeMinter.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *ERC20NativeMinterOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NativeMinterOwnershipTransferred)
				if err := _ERC20NativeMinter.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) ParseOwnershipTransferred(log types.Log) (*ERC20NativeMinterOwnershipTransferred, error) {
	event := new(ERC20NativeMinterOwnershipTransferred)
	if err := _ERC20NativeMinter.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20NativeMinterTransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the ERC20NativeMinter contract.
type ERC20NativeMinterTransferIterator struct {
	Event *ERC20NativeMinterTransfer // Event containing the contract specifics and raw log

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
func (it *ERC20NativeMinterTransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NativeMinterTransfer)
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
		it.Event = new(ERC20NativeMinterTransfer)
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
func (it *ERC20NativeMinterTransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NativeMinterTransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NativeMinterTransfer represents a Transfer event raised by the ERC20NativeMinter contract.
type ERC20NativeMinterTransfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*ERC20NativeMinterTransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NativeMinterTransferIterator{contract: _ERC20NativeMinter.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *ERC20NativeMinterTransfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ERC20NativeMinter.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NativeMinterTransfer)
				if err := _ERC20NativeMinter.contract.UnpackLog(event, "Transfer", log); err != nil {
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

// ParseTransfer is a log parse operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20NativeMinter *ERC20NativeMinterFilterer) ParseTransfer(log types.Log) (*ERC20NativeMinterTransfer, error) {
	event := new(ERC20NativeMinterTransfer)
	if err := _ERC20NativeMinter.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
