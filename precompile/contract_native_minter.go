// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

const (
	mintInputAddressSlot = iota
	mintInputAmountSlot

	mintInputLen = common.HashLength + common.HashLength

	MintGasCost = 30_000
)

var (
	_ StatefulPrecompileConfig = &ContractNativeMinterConfig{}
	// Singleton StatefulPrecompiledContract for minting native assets by permissioned callers.
	ContractNativeMinterPrecompile StatefulPrecompiledContract = createNativeMinterPrecompile(ContractNativeMinterAddress)

	mintSignature = CalculateFunctionSelector("mintNativeCoin(address,uint256)") // address, amount
	ErrCannotMint = errors.New("non-enabled cannot mint")
)

// ContractNativeMinterConfig wraps [AllowListConfig] and uses it to implement the StatefulPrecompileConfig
// interface while adding in the ContractNativeMinter specific precompile address.
type ContractNativeMinterConfig struct {
	AllowListConfig
	UpgradeableConfig
	InitialMint map[common.Address]*math.HexOrDecimal256 `json:"initialMint,omitempty"` // initial mint config to be immediately minted
}

// NewContractNativeMinterConfig returns a config for a network upgrade at [blockTimestamp] that enables
// ContractNativeMinter with the given [admins] and [enableds] as members of the allowlist. Also mints balances according to [initialMint] when the upgrade activates.
func NewContractNativeMinterConfig(blockTimestamp *big.Int, admins []common.Address, enableds []common.Address, initialMint map[common.Address]*math.HexOrDecimal256) *ContractNativeMinterConfig {
	return &ContractNativeMinterConfig{
		AllowListConfig: AllowListConfig{
			AllowListAdmins:  admins,
			EnabledAddresses: enableds,
		},
		UpgradeableConfig: UpgradeableConfig{BlockTimestamp: blockTimestamp},
		InitialMint:       initialMint,
	}
}

// NewDisableContractNativeMinterConfig returns config for a network upgrade at [blockTimestamp]
// that disables ContractNativeMinter.
func NewDisableContractNativeMinterConfig(blockTimestamp *big.Int) *ContractNativeMinterConfig {
	return &ContractNativeMinterConfig{
		UpgradeableConfig: UpgradeableConfig{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Address returns the address of the native minter contract.
func (c *ContractNativeMinterConfig) Address() common.Address {
	return ContractNativeMinterAddress
}

// Configure configures [state] with the desired admins based on [c].
func (c *ContractNativeMinterConfig) Configure(_ ChainConfig, state StateDB, _ BlockContext) {
	for to, amount := range c.InitialMint {
		if amount != nil {
			bigIntAmount := (*big.Int)(amount)
			state.AddBalance(to, bigIntAmount)
		}
	}

	c.AllowListConfig.Configure(state, ContractNativeMinterAddress)
}

// Contract returns the singleton stateful precompiled contract to be used for the native minter.
func (c *ContractNativeMinterConfig) Contract() StatefulPrecompiledContract {
	return ContractNativeMinterPrecompile
}

func (c *ContractNativeMinterConfig) Verify() error {
	if err := c.AllowListConfig.Verify(); err != nil {
		return err
	}
	// ensure that all of the initial mint values in the map are non-nil positive values
	for addr, amount := range c.InitialMint {
		if amount == nil {
			return fmt.Errorf("initial mint cannot contain nil amount for address %s", addr)
		}
		bigIntAmount := (*big.Int)(amount)
		if bigIntAmount.Sign() < 1 {
			return fmt.Errorf("initial mint cannot contain invalid amount %v for address %s", bigIntAmount, addr)
		}
	}
	return nil
}

// Equal returns true if [s] is a [*ContractNativeMinterConfig] and it has been configured identical to [c].
func (c *ContractNativeMinterConfig) Equal(s StatefulPrecompileConfig) bool {
	// typecast before comparison
	other, ok := (s).(*ContractNativeMinterConfig)
	if !ok {
		return false
	}
	eq := c.UpgradeableConfig.Equal(&other.UpgradeableConfig) && c.AllowListConfig.Equal(&other.AllowListConfig)
	if !eq {
		return false
	}

	if len(c.InitialMint) != len(other.InitialMint) {
		return false
	}

	for address, amount := range c.InitialMint {
		val, ok := other.InitialMint[address]
		if !ok {
			return false
		}
		bigIntAmount := (*big.Int)(amount)
		bigIntVal := (*big.Int)(val)
		if !utils.BigNumEqual(bigIntAmount, bigIntVal) {
			return false
		}
	}

	return true
}

// String returns a string representation of the ContractNativeMinterConfig.
func (c *ContractNativeMinterConfig) String() string {
	bytes, _ := json.Marshal(c)
	return string(bytes)
}

// GetContractNativeMinterStatus returns the role of [address] for the minter list.
func GetContractNativeMinterStatus(stateDB StateDB, address common.Address) AllowListRole {
	return getAllowListStatus(stateDB, ContractNativeMinterAddress, address)
}

// SetContractNativeMinterStatus sets the permissions of [address] to [role] for the
// minter list. assumes [role] has already been verified as valid.
func SetContractNativeMinterStatus(stateDB StateDB, address common.Address, role AllowListRole) {
	setAllowListRole(stateDB, ContractNativeMinterAddress, address, role)
}

// PackMintInput packs [address] and [amount] into the appropriate arguments for minting operation.
// Assumes that [amount] can be represented by 32 bytes.
func PackMintInput(address common.Address, amount *big.Int) ([]byte, error) {
	// function selector (4 bytes) + input(hash for address + hash for amount)
	res := make([]byte, selectorLen+mintInputLen)
	packOrderedHashesWithSelector(res, mintSignature, []common.Hash{
		address.Hash(),
		common.BigToHash(amount),
	})

	return res, nil
}

// UnpackMintInput attempts to unpack [input] into the arguments to the mint precompile
// assumes that [input] does not include selector (omits first 4 bytes in PackMintInput)
func UnpackMintInput(input []byte) (common.Address, *big.Int, error) {
	if len(input) != mintInputLen {
		return common.Address{}, nil, fmt.Errorf("invalid input length for minting: %d", len(input))
	}
	to := common.BytesToAddress(returnPackedHash(input, mintInputAddressSlot))
	assetAmount := new(big.Int).SetBytes(returnPackedHash(input, mintInputAmountSlot))
	return to, assetAmount, nil
}

// mintNativeCoin checks if the caller is permissioned for minting operation.
// The execution function parses the [input] into native coin amount and receiver address.
func mintNativeCoin(accessibleState PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = deductGas(suppliedGas, MintGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}

	to, amount, err := UnpackMintInput(input)
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := getAllowListStatus(stateDB, ContractNativeMinterAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotMint, caller)
	}

	// if there is no address in the state, create one.
	if !stateDB.Exist(to) {
		stateDB.CreateAccount(to)
	}

	stateDB.AddBalance(to, amount)
	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// createNativeMinterPrecompile returns a StatefulPrecompiledContract with R/W control of an allow list at [precompileAddr] and a native coin minter.
func createNativeMinterPrecompile(precompileAddr common.Address) StatefulPrecompiledContract {
	enabledFuncs := createAllowListFunctions(precompileAddr)

	mintFunc := newStatefulPrecompileFunction(mintSignature, mintNativeCoin)

	enabledFuncs = append(enabledFuncs, mintFunc)
	// Construct the contract with no fallback function.
	contract := newStatefulPrecompileWithFunctionSelectors(nil, enabledFuncs)
	return contract
}
