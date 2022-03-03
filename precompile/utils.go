// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"fmt"
	"regexp"

	"github.com/ethereum/go-ethereum/crypto"
)

var functionSignatureRegex = regexp.MustCompile(`[\w]+\(((([\w]+)?)|((([\w]+),)+([\w]+)))\)`)

// CalculateFunctionSelector returns the 4 byte function selector that results from [functionSignature]
// Ex. the function setBalance(addr address, balance uint256) should be passed in as the string:
// "setBalance(address,uint256)"
func CalculateFunctionSelector(functionSignature string) []byte {
	if !functionSignatureRegex.MatchString(functionSignature) {
		panic(fmt.Errorf("invalid function signature: %q", functionSignature))
	}
	hash := crypto.Keccak256([]byte(functionSignature))
	return hash[:4]
}

// createConstantRequiredGasFunc returns a required gas function that always returns [requiredGas]
// on any input.
func createConstantRequiredGasFunc(requiredGas uint64) func([]byte) uint64 {
	return func(b []byte) uint64 { return requiredGas }
}
