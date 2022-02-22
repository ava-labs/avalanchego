// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// AllowListPrecompile is a singleton to be used by the EVM as a stateful precompile contract
var AllowListPrecompile StatefulPrecompiledContract = NewAllowListPrecompile()

type ModifyStatus common.Hash

// Enum constants for valid ModifyStatus
var (
	None     ModifyStatus = ModifyStatus(common.Hash{})  // No role assigned - this is equivalent to common.Hash{} and deletes the key from the DB when set
	Deployer ModifyStatus = ModifyStatus(common.Hash{1}) // Deployers are allowed to create new contracts
	Admin    ModifyStatus = ModifyStatus(common.Hash{2}) // Admin - allowed to modify both the admin and deployer list as well as deploy contracts
)

const (
	modifyStatusPrecompileInputLength = common.AddressLength + common.HashLength
)

type AllowListConfig struct {
	BlockTimestamp *big.Int `json:"blockTimestamp"`

	AllowListAdmins []common.Address `json:"adminAddresses"`
}

func (c *AllowListConfig) PrecompileAddress() common.Address { return ModifyAllowListAddress }

func (c *AllowListConfig) Timestamp() *big.Int { return c.BlockTimestamp }

func (c *AllowListConfig) Configure(state StateDB) {
	for _, adminAddr := range c.AllowListAdmins {
		state.SetState(ModifyAllowListAddress, CreateAddressKey(adminAddr), common.Hash(Admin))
	}
}

// valid returns nil if the status is a valid status.
func (s ModifyStatus) Valid() bool {
	switch s {
	case None, Deployer, Admin:
		return true
	default:
		return false
	}
}

// IsAdmin
func (s ModifyStatus) HasAdminPrivileges() bool {
	switch s {
	case Admin:
		return true
	default:
		return false
	}
}

// IsDeployer
func (s ModifyStatus) HasDeployPrivileges() bool {
	switch s {
	case Deployer, Admin:
		return true
	default:
		return false
	}
}

// GetAllowListStatus checks the state storage of [AllowListPrecompileAddress] and returns
// the ModifyStatus of [address]
func GetAllowListStatus(state StateDB, address common.Address) ModifyStatus {
	stateSlot := CreateAddressKey(address)
	res := state.GetState(ModifyAllowListAddress, stateSlot)
	return ModifyStatus(res)
}

// PackModifyAllowList packs the arguments [address] and [status] into a single byte slice as input to the modify allow list
// precompile
func PackModifyAllowList(address common.Address, status ModifyStatus) ([]byte, error) {
	// If [allow], set the last byte to 1 to indicate that [address] should be added to the whitelist
	if !status.Valid() {
		return nil, fmt.Errorf("unexpected status: %d", status)
	}

	input := make([]byte, modifyStatusPrecompileInputLength)
	copy(input, address[:])
	copy(input[20:], status[:])
	return input, nil
}

// UnpackModifyAllowList attempts to unpack [input] into the arguments to the allow list precompile
func UnpackModifyAllowList(input []byte) (common.Address, ModifyStatus, error) {
	if len(input) != modifyStatusPrecompileInputLength {
		return common.Address{}, None, fmt.Errorf("unexpected address length: %d not %d", len(input), modifyStatusPrecompileInputLength)
	}

	address := common.BytesToAddress(input[:common.AddressLength])
	status := ModifyStatus(common.BytesToHash(input[common.AddressLength:]))

	if !status.Valid() {
		return common.Address{}, None, fmt.Errorf("unexpected value for modify status: %v", status)
	}

	return address, status, nil
}

// SetAllowListStatus sets the permissions of [address] to [status]
func SetAllowListStatus(stateDB StateDB, address common.Address, status ModifyStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid status used as input for allow list precompile: %v", status)
	}
	// Generate the state key for [address]
	addressKey := CreateAddressKey(address)
	// Update the type of [status] for the call to SetState
	role := common.Hash(status)
	log.Info("modify allow list", "address", address, "role", role)
	// Assign [role] to the address
	stateDB.SetState(ModifyAllowListAddress, addressKey, role)
	return nil
}

func NewAllowListPrecompile() StatefulPrecompiledContract {
	return &allowListPrecompile{}
}

type allowListPrecompile struct{}

func (al *allowListPrecompile) Run(evm PrecompileAccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if suppliedGas < ModifyAllowListGasCost {
		return nil, 0, fmt.Errorf("running allow list exceeds gas allowance (%d) < (%d)", ModifyAllowListGasCost, suppliedGas)
	}

	remainingGas = suppliedGas - ModifyAllowListGasCost

	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := GetAllowListStatus(evm.GetStateDB(), callerAddr)
	if !callerStatus.HasAdminPrivileges() {
		log.Info("EVM received attempt to modify the allow list from a non-allowed address", "callerAddr", callerAddr)
		return nil, remainingGas, fmt.Errorf("cannot ")
	}

	// Unpack the precompile's input into the necessary arguments
	address, status, err := UnpackModifyAllowList(input)
	if err != nil {
		log.Info("modify allow list reverted", "err", err)
		return nil, remainingGas, fmt.Errorf("failed to unpack modify allow list input: %w", err)
	}

	if err := SetAllowListStatus(evm.GetStateDB(), address, status); err != nil {
		return nil, remainingGas, err
	}

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

func (al *allowListPrecompile) RequiredGas(input []byte) uint64 { return ModifyAllowListGasCost }
