// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi"
)

// Gas costs for stateful precompiles
const (
	WriteGasCostPerSlot = 20_000
	ReadGasCostPerSlot  = 5_000

	// Per LOG operation.
	LogGas uint64 = 375 // from params/protocol_params.go
	// Gas cost of single topic of the LOG. Should be multiplied by the number of topics.
	LogTopicGas uint64 = 375 // from params/protocol_params.go
	// Per byte cost in a LOG operation's data. Should be multiplied by the byte size of the data.
	LogDataGas uint64 = 8 // from params/protocol_params.go
)

var functionSignatureRegex = regexp.MustCompile(`\w+\((\w*|(\w+,)+\w+)\)`)

// CalculateFunctionSelector returns the 4 byte function selector that results from [functionSignature]
// Ex. the function setBalance(addr address, balance uint256) should be passed in as the string:
// "setBalance(address,uint256)"
// TODO: remove this after moving to ABI based function selectors.
func CalculateFunctionSelector(functionSignature string) []byte {
	if !functionSignatureRegex.MatchString(functionSignature) {
		panic(fmt.Errorf("invalid function signature: %q", functionSignature))
	}
	hash := crypto.Keccak256([]byte(functionSignature))
	return hash[:4]
}

// DeductGas checks if [suppliedGas] is sufficient against [requiredGas] and deducts [requiredGas] from [suppliedGas].
func DeductGas(suppliedGas uint64, requiredGas uint64) (uint64, error) {
	if suppliedGas < requiredGas {
		return 0, vm.ErrOutOfGas
	}
	return suppliedGas - requiredGas, nil
}

// ParseABI parses the given ABI string and returns the parsed ABI.
// If the ABI is invalid, it panics.
func ParseABI(rawABI string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(rawABI))
	if err != nil {
		panic(err)
	}

	return parsed
}
