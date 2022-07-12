// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"fmt"
	"regexp"

	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
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

// deductGas checks if [suppliedGas] is sufficient against [requiredGas] and deducts [requiredGas] from [suppliedGas].
func deductGas(suppliedGas uint64, requiredGas uint64) (uint64, error) {
	if suppliedGas < requiredGas {
		return 0, vmerrs.ErrOutOfGas
	}
	return suppliedGas - requiredGas, nil
}

// packOrderedHashesWithSelector packs the function selector and ordered list of hashes into [dst]
// byte slice.
// assumes that [dst] has sufficient room for [functionSelector] and [hashes].
func packOrderedHashesWithSelector(dst []byte, functionSelector []byte, hashes []common.Hash) {
	copy(dst[:len(functionSelector)], functionSelector)
	packOrderedHashes(dst[len(functionSelector):], hashes)
}

// packOrderedHashes packs the ordered list of [hashes] into the [dst] byte buffer.
// assumes that [dst] has sufficient space to pack [hashes] or else this function will panic.
func packOrderedHashes(dst []byte, hashes []common.Hash) {
	if len(dst) != len(hashes)*common.HashLength {
		panic(fmt.Sprintf("destination byte buffer has insufficient length (%d) for %d hashes", len(dst), len(hashes)))
	}

	var (
		start = 0
		end   = common.HashLength
	)
	for _, hash := range hashes {
		copy(dst[start:end], hash.Bytes())
		start += common.HashLength
		end += common.HashLength
	}
}

// returnPackedHash returns packed the byte slice with common.HashLength from [packed]
// at the given [index].
// Assumes that [packed] is composed entirely of packed 32 byte segments.
func returnPackedHash(packed []byte, index int) []byte {
	start := common.HashLength * index
	end := start + common.HashLength
	return packed[start:end]
}
