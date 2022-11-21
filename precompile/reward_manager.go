// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Code generated
// This file is a generated precompile contract with stubbed abstract functions.

package precompile

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/vmerrs"

	"github.com/ethereum/go-ethereum/common"
)

const (
	AllowFeeRecipientsGasCost      uint64 = (writeGasCostPerSlot) + ReadAllowListGasCost // write 1 slot + read allow list
	AreFeeRecipientsAllowedGasCost uint64 = readGasCostPerSlot
	CurrentRewardAddressGasCost    uint64 = readGasCostPerSlot
	DisableRewardsGasCost          uint64 = (writeGasCostPerSlot) + ReadAllowListGasCost // write 1 slot + read allow list
	SetRewardAddressGasCost        uint64 = (writeGasCostPerSlot) + ReadAllowListGasCost // write 1 slot + read allow list

	// RewardManagerRawABI contains the raw ABI of RewardManager contract.
	RewardManagerRawABI = "[{\"inputs\":[],\"name\":\"allowFeeRecipients\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"areFeeRecipientsAllowed\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"isAllowed\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"currentRewardAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"rewardAddress\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"disableRewards\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"readAllowList\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"role\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setNone\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"setRewardAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"
)

// Singleton StatefulPrecompiledContract and signatures.
var (
	_ StatefulPrecompileConfig = &RewardManagerConfig{}

	ErrCannotAllowFeeRecipients      = errors.New("non-enabled cannot call allowFeeRecipients")
	ErrCannotAreFeeRecipientsAllowed = errors.New("non-enabled cannot call areFeeRecipientsAllowed")
	ErrCannotCurrentRewardAddress    = errors.New("non-enabled cannot call currentRewardAddress")
	ErrCannotDisableRewards          = errors.New("non-enabled cannot call disableRewards")
	ErrCannotSetRewardAddress        = errors.New("non-enabled cannot call setRewardAddress")

	ErrCannotEnableBothRewards = errors.New("cannot enable both fee recipients and reward address at the same time")
	ErrEmptyRewardAddress      = errors.New("reward address cannot be empty")

	RewardManagerABI        abi.ABI                     // will be initialized by init function
	RewardManagerPrecompile StatefulPrecompiledContract // will be initialized by init function

	rewardAddressStorageKey        = common.Hash{'r', 'a', 's', 'k'}
	allowFeeRecipientsAddressValue = common.Hash{'a', 'f', 'r', 'a', 'v'}
)

type InitialRewardConfig struct {
	AllowFeeRecipients bool           `json:"allowFeeRecipients"`
	RewardAddress      common.Address `json:"rewardAddress,omitempty"`
}

func (i *InitialRewardConfig) Verify() error {
	switch {
	case i.AllowFeeRecipients && i.RewardAddress != (common.Address{}):
		return ErrCannotEnableBothRewards
	default:
		return nil
	}
}

func (c *InitialRewardConfig) Equal(other *InitialRewardConfig) bool {
	if other == nil {
		return false
	}

	return c.AllowFeeRecipients == other.AllowFeeRecipients && c.RewardAddress == other.RewardAddress
}

func (i *InitialRewardConfig) Configure(state StateDB) {
	// enable allow fee recipients
	if i.AllowFeeRecipients {
		EnableAllowFeeRecipients(state)
	} else if i.RewardAddress == (common.Address{}) {
		// if reward address is empty and allow fee recipients is false
		// then disable rewards
		DisableFeeRewards(state)
	} else {
		// set reward address
		if err := StoreRewardAddress(state, i.RewardAddress); err != nil {
			panic(err)
		}
	}
}

// RewardManagerConfig implements the StatefulPrecompileConfig
// interface while adding in the RewardManager specific precompile config.
type RewardManagerConfig struct {
	AllowListConfig
	UpgradeableConfig
	InitialRewardConfig *InitialRewardConfig `json:"initialRewardConfig,omitempty"`
}

func init() {
	parsed, err := abi.JSON(strings.NewReader(RewardManagerRawABI))
	if err != nil {
		panic(err)
	}
	RewardManagerABI = parsed
	RewardManagerPrecompile = createRewardManagerPrecompile(RewardManagerAddress)
}

// NewRewardManagerConfig returns a config for a network upgrade at [blockTimestamp] that enables
// RewardManager with the given [admins] and [enableds] as members of the allowlist with [initialConfig] as initial rewards config if specified.
func NewRewardManagerConfig(blockTimestamp *big.Int, admins []common.Address, enableds []common.Address, initialConfig *InitialRewardConfig) *RewardManagerConfig {
	return &RewardManagerConfig{
		AllowListConfig: AllowListConfig{
			AllowListAdmins:  admins,
			EnabledAddresses: enableds,
		},
		UpgradeableConfig:   UpgradeableConfig{BlockTimestamp: blockTimestamp},
		InitialRewardConfig: initialConfig,
	}
}

// NewDisableRewardManagerConfig returns config for a network upgrade at [blockTimestamp]
// that disables RewardManager.
func NewDisableRewardManagerConfig(blockTimestamp *big.Int) *RewardManagerConfig {
	return &RewardManagerConfig{
		UpgradeableConfig: UpgradeableConfig{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Equal returns true if [s] is a [*RewardManagerConfig] and it has been configured identical to [c].
func (c *RewardManagerConfig) Equal(s StatefulPrecompileConfig) bool {
	// typecast before comparison
	other, ok := (s).(*RewardManagerConfig)
	if !ok {
		return false
	}
	// modify this boolean accordingly with your custom RewardManagerConfig, to check if [other] and the current [c] are equal
	// if RewardManagerConfig contains only UpgradeableConfig and AllowListConfig you can skip modifying it.
	equals := c.UpgradeableConfig.Equal(&other.UpgradeableConfig) && c.AllowListConfig.Equal(&other.AllowListConfig)
	if !equals {
		return false
	}

	if c.InitialRewardConfig == nil {
		return other.InitialRewardConfig == nil
	}

	return c.InitialRewardConfig.Equal(other.InitialRewardConfig)
}

// Address returns the address of the RewardManager. Addresses reside under the precompile/params.go
// Select a non-conflicting address and set it in the params.go.
func (c *RewardManagerConfig) Address() common.Address {
	return RewardManagerAddress
}

// Configure configures [state] with the initial configuration.
func (c *RewardManagerConfig) Configure(chainConfig ChainConfig, state StateDB, _ BlockContext) {
	c.AllowListConfig.Configure(state, RewardManagerAddress)
	// configure the RewardManager with the given initial configuration
	if c.InitialRewardConfig != nil {
		c.InitialRewardConfig.Configure(state)
	} else if chainConfig.AllowedFeeRecipients() {
		// configure the RewardManager according to chainConfig
		EnableAllowFeeRecipients(state)
	} else {
		// chainConfig does not have any reward address
		// if chainConfig does not enable fee recipients
		// default to disabling rewards
		DisableFeeRewards(state)
	}
}

// Contract returns the singleton stateful precompiled contract to be used for RewardManager.
func (c *RewardManagerConfig) Contract() StatefulPrecompiledContract {
	return RewardManagerPrecompile
}

func (c *RewardManagerConfig) Verify() error {
	if err := c.AllowListConfig.Verify(); err != nil {
		return err
	}
	if c.InitialRewardConfig != nil {
		return c.InitialRewardConfig.Verify()
	}
	return nil
}

// String returns a string representation of the RewardManagerConfig.
func (c *RewardManagerConfig) String() string {
	bytes, _ := json.Marshal(c)
	return string(bytes)
}

// GetRewardManagerAllowListStatus returns the role of [address] for the RewardManager list.
func GetRewardManagerAllowListStatus(stateDB StateDB, address common.Address) AllowListRole {
	return getAllowListStatus(stateDB, RewardManagerAddress, address)
}

// SetRewardManagerAllowListStatus sets the permissions of [address] to [role] for the
// RewardManager list. Assumes [role] has already been verified as valid.
func SetRewardManagerAllowListStatus(stateDB StateDB, address common.Address, role AllowListRole) {
	setAllowListRole(stateDB, RewardManagerAddress, address, role)
}

// PackAllowFeeRecipients packs the function selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackAllowFeeRecipients() ([]byte, error) {
	return RewardManagerABI.Pack("allowFeeRecipients")
}

// EnableAllowFeeRecipients enables fee recipients.
func EnableAllowFeeRecipients(stateDB StateDB) {
	stateDB.SetState(RewardManagerAddress, rewardAddressStorageKey, allowFeeRecipientsAddressValue)
}

// DisableRewardAddress disables rewards and burns them by sending to Blackhole Address.
func DisableFeeRewards(stateDB StateDB) {
	stateDB.SetState(RewardManagerAddress, rewardAddressStorageKey, constants.BlackholeAddr.Hash())
}

func allowFeeRecipients(accessibleState PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = deductGas(suppliedGas, AllowFeeRecipientsGasCost); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}
	// no input provided for this function

	// Allow list is enabled and AllowFeeRecipients is a state-changer function.
	// This part of the code restricts the function to be called only by enabled/admin addresses in the allow list.
	// You can modify/delete this code if you don't want this function to be restricted by the allow list.
	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := getAllowListStatus(stateDB, RewardManagerAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotAllowFeeRecipients, caller)
	}
	// allow list code ends here.

	// this function does not return an output, leave this one as is
	EnableAllowFeeRecipients(stateDB)
	packedOutput := []byte{}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// PackAreFeeRecipientsAllowed packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackAreFeeRecipientsAllowed() ([]byte, error) {
	return RewardManagerABI.Pack("areFeeRecipientsAllowed")
}

// PackAreFeeRecipientsAllowedOutput attempts to pack given isAllowed of type bool
// to conform the ABI outputs.
func PackAreFeeRecipientsAllowedOutput(isAllowed bool) ([]byte, error) {
	return RewardManagerABI.PackOutput("areFeeRecipientsAllowed", isAllowed)
}

func areFeeRecipientsAllowed(accessibleState PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = deductGas(suppliedGas, AreFeeRecipientsAllowedGasCost); err != nil {
		return nil, 0, err
	}
	// no input provided for this function

	stateDB := accessibleState.GetStateDB()
	var output bool
	_, output = GetStoredRewardAddress(stateDB)

	packedOutput, err := PackAreFeeRecipientsAllowedOutput(output)
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// PackCurrentRewardAddress packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackCurrentRewardAddress() ([]byte, error) {
	return RewardManagerABI.Pack("currentRewardAddress")
}

// PackCurrentRewardAddressOutput attempts to pack given rewardAddress of type common.Address
// to conform the ABI outputs.
func PackCurrentRewardAddressOutput(rewardAddress common.Address) ([]byte, error) {
	return RewardManagerABI.PackOutput("currentRewardAddress", rewardAddress)
}

// GetStoredRewardAddress returns the current value of the address stored under rewardAddressStorageKey.
// Returns an empty address and true if allow fee recipients is enabled, otherwise returns current reward address and false.
func GetStoredRewardAddress(stateDB StateDB) (common.Address, bool) {
	val := stateDB.GetState(RewardManagerAddress, rewardAddressStorageKey)
	return common.BytesToAddress(val.Bytes()), val == allowFeeRecipientsAddressValue
}

// StoredRewardAddress stores the given [val] under rewardAddressStorageKey.
func StoreRewardAddress(stateDB StateDB, val common.Address) error {
	// if input is empty, return an error
	if val == (common.Address{}) {
		return ErrEmptyRewardAddress
	}
	stateDB.SetState(RewardManagerAddress, rewardAddressStorageKey, val.Hash())
	return nil
}

// PackSetRewardAddress packs [addr] of type common.Address into the appropriate arguments for setRewardAddress.
// the packed bytes include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackSetRewardAddress(addr common.Address) ([]byte, error) {
	return RewardManagerABI.Pack("setRewardAddress", addr)
}

// UnpackSetRewardAddressInput attempts to unpack [input] into the common.Address type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackSetRewardAddressInput(input []byte) (common.Address, error) {
	res, err := RewardManagerABI.UnpackInput("setRewardAddress", input)
	if err != nil {
		return common.Address{}, err
	}
	unpacked := *abi.ConvertType(res[0], new(common.Address)).(*common.Address)
	return unpacked, nil
}

func setRewardAddress(accessibleState PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = deductGas(suppliedGas, SetRewardAddressGasCost); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}
	// attempts to unpack [input] into the arguments to the SetRewardAddressInput.
	// Assumes that [input] does not include selector
	// You can use unpacked [inputStruct] variable in your code
	inputStruct, err := UnpackSetRewardAddressInput(input)
	if err != nil {
		return nil, remainingGas, err
	}

	// Allow list is enabled and SetRewardAddress is a state-changer function.
	// This part of the code restricts the function to be called only by enabled/admin addresses in the allow list.
	// You can modify/delete this code if you don't want this function to be restricted by the allow list.
	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := getAllowListStatus(stateDB, RewardManagerAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotSetRewardAddress, caller)
	}
	// allow list code ends here.

	if err := StoreRewardAddress(stateDB, inputStruct); err != nil {
		return nil, remainingGas, err
	}
	// this function does not return an output, leave this one as is
	packedOutput := []byte{}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

func currentRewardAddress(accessibleState PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = deductGas(suppliedGas, CurrentRewardAddressGasCost); err != nil {
		return nil, 0, err
	}

	// no input provided for this function
	stateDB := accessibleState.GetStateDB()
	output, _ := GetStoredRewardAddress(stateDB)
	packedOutput, err := PackCurrentRewardAddressOutput(output)
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// PackDisableRewards packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackDisableRewards() ([]byte, error) {
	return RewardManagerABI.Pack("disableRewards")
}

func disableRewards(accessibleState PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = deductGas(suppliedGas, DisableRewardsGasCost); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}
	// no input provided for this function

	// Allow list is enabled and DisableRewards is a state-changer function.
	// This part of the code restricts the function to be called only by enabled/admin addresses in the allow list.
	// You can modify/delete this code if you don't want this function to be restricted by the allow list.
	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := getAllowListStatus(stateDB, RewardManagerAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotDisableRewards, caller)
	}
	// allow list code ends here.
	DisableFeeRewards(stateDB)
	// this function does not return an output, leave this one as is
	packedOutput := []byte{}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// createRewardManagerPrecompile returns a StatefulPrecompiledContract with getters and setters for the precompile.
// Access to the getters/setters is controlled by an allow list for [precompileAddr].
func createRewardManagerPrecompile(precompileAddr common.Address) StatefulPrecompiledContract {
	var functions []*statefulPrecompileFunction
	functions = append(functions, createAllowListFunctions(precompileAddr)...)

	methodAllowFeeRecipients, ok := RewardManagerABI.Methods["allowFeeRecipients"]
	if !ok {
		panic("given method does not exist in the ABI")
	}
	functions = append(functions, newStatefulPrecompileFunction(methodAllowFeeRecipients.ID, allowFeeRecipients))

	methodAreFeeRecipientsAllowed, ok := RewardManagerABI.Methods["areFeeRecipientsAllowed"]
	if !ok {
		panic("given method does not exist in the ABI")
	}
	functions = append(functions, newStatefulPrecompileFunction(methodAreFeeRecipientsAllowed.ID, areFeeRecipientsAllowed))

	methodCurrentRewardAddress, ok := RewardManagerABI.Methods["currentRewardAddress"]
	if !ok {
		panic("given method does not exist in the ABI")
	}
	functions = append(functions, newStatefulPrecompileFunction(methodCurrentRewardAddress.ID, currentRewardAddress))

	methodDisableRewards, ok := RewardManagerABI.Methods["disableRewards"]
	if !ok {
		panic("given method does not exist in the ABI")
	}
	functions = append(functions, newStatefulPrecompileFunction(methodDisableRewards.ID, disableRewards))

	methodSetRewardAddress, ok := RewardManagerABI.Methods["setRewardAddress"]
	if !ok {
		panic("given method does not exist in the ABI")
	}
	functions = append(functions, newStatefulPrecompileFunction(methodSetRewardAddress.ID, setRewardAddress))

	// Construct the contract with no fallback function.
	contract := newStatefulPrecompileWithFunctionSelectors(nil, functions)
	return contract
}
