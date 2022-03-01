// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// CalculateFunctionSelector returns the 4 byte function selector that results from [functionSignature]
// Ex. the function setBalance(addr address, balance uint256) should be passed in as the string:
// "setBalance(address,uint256)"
func CalculateFunctionSelector(functionSignature string) []byte {
	// TODO replace with more complete regex for valid function signatures.
	if containsSpaces := strings.Contains(functionSignature, " "); containsSpaces {
		panic(fmt.Errorf("function signature %q should not contain any spaces", functionSignature))
	}
	hash := crypto.Keccak256([]byte(functionSignature))
	return hash[:4]
}

// createConstantRequiredGasFunc returns a required gas function that always returns [requiredGas]
// on any input.
func createConstantRequiredGasFunc(requiredGas uint64) func([]byte) uint64 {
	return func(b []byte) uint64 { return requiredGas }
}
