// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package contract

import (
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/core/vm"
)

// Gas costs for stateful precompiles
const (
	WriteGasCostPerSlot = 20_000

	// Per LOG operation.
	LogGas uint64 = 375 // from params/protocol_params.go
	// Gas cost of single topic of the LOG. Should be multiplied by the number of topics.
	LogTopicGas uint64 = 375 // from params/protocol_params.go
	// Per byte cost in a LOG operation's data. Should be multiplied by the byte size of the data.
	LogDataGas uint64 = 8 // from params/protocol_params.go
)

// DeductMaxGas tries to consume requiredGas, and if not possible, consumes all
// available and returns [vm.ErrOutOfGas].
func DeductMaxGas(env vm.PrecompileEnvironment, requiredGas uint64) error {
	if env.UseGas(requiredGas) {
		return nil
	}
	env.UseGas(env.Gas())
	return vm.ErrOutOfGas
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
