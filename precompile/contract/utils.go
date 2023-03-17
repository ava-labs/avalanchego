// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Gas costs for stateful precompiles
const (
	WriteGasCostPerSlot = 20_000
	ReadGasCostPerSlot  = 5_000
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
		return 0, vmerrs.ErrOutOfGas
	}
	return suppliedGas - requiredGas, nil
}

// PackOrderedHashesWithSelector packs the function selector and ordered list of hashes into [dst]
// byte slice.
// assumes that [dst] has sufficient room for [functionSelector] and [hashes].
func PackOrderedHashesWithSelector(dst []byte, functionSelector []byte, hashes []common.Hash) error {
	copy(dst[:len(functionSelector)], functionSelector)
	return PackOrderedHashes(dst[len(functionSelector):], hashes)
}

// PackOrderedHashes packs the ordered list of [hashes] into the [dst] byte buffer.
// assumes that [dst] has sufficient space to pack [hashes] or else this function will panic.
func PackOrderedHashes(dst []byte, hashes []common.Hash) error {
	if len(dst) != len(hashes)*common.HashLength {
		return fmt.Errorf("destination byte buffer has insufficient length (%d) for %d hashes", len(dst), len(hashes))
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
	return nil
}

// PackedHash returns packed the byte slice with common.HashLength from [packed]
// at the given [index].
// Assumes that [packed] is composed entirely of packed 32 byte segments.
func PackedHash(packed []byte, index int) []byte {
	start := common.HashLength * index
	end := start + common.HashLength
	return packed[start:end]
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
