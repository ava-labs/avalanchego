// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package bind

import (
	"testing"
)

var bindFailedTests = []struct {
	name     string
	contract string
	bytecode []string
	abi      []string
	errorMsg string
	fsigs    []map[string]string
	libs     map[string]string
	aliases  map[string]string
	types    []string
}{
	{
		`AnonOutputChecker`, ``,
		[]string{``},
		[]string{`
			[
				{"type":"function","name":"anonOutput","constant":true,"inputs":[],"outputs":[{"name":"","type":"string"}]}
			]
		`},
		"ABI outputs for AnonOutput require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs",
		nil,
		nil,
		nil,
		nil,
	},

	{
		`AnonOutputsChecker`, ``,
		[]string{``},
		[]string{`
			[
				{"type":"function","name":"anonOutputs","constant":true,"inputs":[],"outputs":[{"name":"","type":"string"},{"name":"","type":"string"}]}
			]
		`},
		"ABI outputs for AnonOutputs require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs",
		nil,
		nil,
		nil,
		nil,
	},

	{
		`MixedOutputsChecker`, ``,
		[]string{``},
		[]string{`
			[
				{"type":"function","name":"mixedOutputs","constant":true,"inputs":[],"outputs":[{"name":"","type":"string"},{"name":"str","type":"string"}]}
			]
		`},
		"ABI outputs for MixedOutputs require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs",
		nil,
		nil,
		nil,
		nil,
	},
}

func TestPrecompileBindings(t *testing.T) {
	golangBindingsFailure(t)
}

func golangBindingsFailure(t *testing.T) {
	// Generate the test suite for all the contracts
	for i, tt := range bindFailedTests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the binding
			_, err := Bind([]string{tt.name}, tt.abi, tt.bytecode, tt.fsigs, "bindtest", LangGo, tt.libs, tt.aliases, true)
			if err == nil {
				t.Fatalf("test %d: no error occurred but was expected", i)
			}

			if tt.errorMsg != err.Error() {
				t.Fatalf("test %d: expected Err %s but got actual Err: %s", i, tt.errorMsg, err.Error())
			}
		})
	}
}
