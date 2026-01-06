// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
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

package precompilebind

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
)

var bindTests = []struct {
	name            string
	contract        string
	abi             string
	imports         string
	tester          string
	errMsg          string
	expectAllowlist bool
}{
	{
		"AnonOutputChecker",
		"",
		`
			[
				{"type":"function","name":"anonOutput","constant":true,"inputs":[],"outputs":[{"name":"","type":"string"}]}
			]
		`,
		"",
		"",
		"ABI outputs for anonOutput require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs",
		false,
	},
	{
		"AnonOutputsChecker",
		"",
		`
			[
				{"type":"function","name":"anonOutputs","constant":true,"inputs":[],"outputs":[{"name":"","type":"string"},{"name":"","type":"string"}]}
			]
		`,
		"",
		"",
		"ABI outputs for anonOutputs require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs",
		false,
	},
	{
		"MixedOutputsChecker",
		"",
		`
			[
				{"type":"function","name":"mixedOutputs","constant":true,"inputs":[],"outputs":[{"name":"","type":"string"},{"name":"str","type":"string"}]}
			]
		`,
		"",
		"",
		"ABI outputs for mixedOutputs require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs",
		false,
	},
	// Test that module is generated correctly
	{
		`EmptyContract`,
		`contract EmptyContract {}`,
		"[]",
		"",
		"",
		"no ABI methods found",
		false,
	},
	// Test that named and anonymous inputs are handled correctly
	{
		`InputChecker`, ``,
		`
				[
					{"type":"function","name":"noInput","constant":true,"inputs":[],"outputs":[]},
					{"type":"function","name":"namedInput","constant":true,"inputs":[{"name":"str","type":"string"}],"outputs":[]},
					{"type":"function","name":"namedInputs","constant":true,"inputs":[{"name":"str1","type":"string"},{"name":"str2","type":"string"}],"outputs":[]}
				]
			`,
		`
		"github.com/stretchr/testify/require"
		`,
		`
		testInput := "test"
		packedInput, err := PackNamedInput(testInput)
		require.NoError(t, err)
		// remove the first 4 bytes of the packed input
		packedInput = packedInput[4:]
		unpackedInput, err := UnpackNamedInputInput(packedInput)
		require.NoError(t, err)
		require.Equal(t, testInput, unpackedInput)

		testInputStruct := NamedInputsInput{
			Str1: "test1",
			Str2: "test2",
		}
		packedInputStruct, err := PackNamedInputs(testInputStruct)
		require.NoError(t, err)
		// remove the first 4 bytes of the packed input
		packedInputStruct = packedInputStruct[4:]
		unpackedInputStruct, err := UnpackNamedInputsInput(packedInputStruct)
		require.NoError(t, err)
		require.Equal(t, unpackedInputStruct, testInputStruct)
		`,
		"",
		false,
	},
	// Test that named and anonymous outputs are handled correctly
	{
		`OutputChecker`, ``,
		`
				[
					{"type":"function","name":"noOutput","constant":true,"inputs":[],"outputs":[]},
					{"type":"function","name":"namedOutput","constant":true,"inputs":[],"outputs":[{"name":"str","type":"string"}]},
					{"type":"function","name":"namedOutputs","constant":true,"inputs":[],"outputs":[{"name":"str1","type":"string"},{"name":"str2","type":"string"}]}
				]
			`,
		`
			"github.com/stretchr/testify/require"
			`,
		`
			testOutput := "test"
			packedOutput, err := PackNamedOutputOutput(testOutput)
			require.NoError(t, err)
			unpackedOutput, err := UnpackNamedOutputOutput(packedOutput)
			require.NoError(t, err)
			require.Equal(t, testOutput, unpackedOutput)

			testNamedOutputs := NamedOutputsOutput{
				Str1: "test1",
				Str2: "test2",
			}
			packedNamedOutputs, err := PackNamedOutputsOutput(testNamedOutputs)
			require.NoError(t, err)
			unpackedNamedOutputs, err := UnpackNamedOutputsOutput(packedNamedOutputs)
			require.NoError(t, err)
			require.Equal(t, testNamedOutputs, unpackedNamedOutputs)
			`,
		"",
		false,
	},
	{
		`Tupler`,
		`
			interface Tupler {
				function tuple() constant returns (string a, int b, bytes32 c);
			}
		`,
		`[{"constant":true,"inputs":[],"name":"tuple","outputs":[{"name":"a","type":"string"},{"name":"b","type":"int256"},{"name":"c","type":"bytes32"}],"type":"function"}]`,
		`
			"math/big"
			"github.com/stretchr/testify/require"
		`,
		`
			testOutput := TupleOutput{"Hi", big.NewInt(123), [32]byte{1, 2, 3}}
			packedOutput, err := PackTupleOutput(testOutput)
			require.NoError(t, err)
			unpackedOutput, err := UnpackTupleOutput(packedOutput)
			require.NoError(t, err)
			require.Equal(t, testOutput, unpackedOutput)
		`,
		"",
		false,
	},
	{
		`Slicer`,
		`
			interface Slicer {
				function echoAddresses(address[] input) constant returns (address[] output);
				function echoInts(int[] input) constant returns (int[] output);
				function echoFancyInts(uint8[23] input) constant returns (uint8[23] output);
				function echoBools(bool[] input) constant returns (bool[] output);
			}
		`,
		`[{"constant":true,"inputs":[{"name":"input","type":"address[]"}],"name":"echoAddresses","outputs":[{"name":"output","type":"address[]"}],"type":"function"},{"constant":true,"inputs":[{"name":"input","type":"uint8[23]"}],"name":"echoFancyInts","outputs":[{"name":"output","type":"uint8[23]"}],"type":"function"},{"constant":true,"inputs":[{"name":"input","type":"int256[]"}],"name":"echoInts","outputs":[{"name":"output","type":"int256[]"}],"type":"function"},{"constant":true,"inputs":[{"name":"input","type":"bool[]"}],"name":"echoBools","outputs":[{"name":"output","type":"bool[]"}],"type":"function"}]`,
		`
					"math/big"
		 			"github.com/stretchr/testify/require"
					"github.com/ava-labs/libevm/common"
		`,
		`
			testArgs := []common.Address{common.HexToAddress("1"), common.HexToAddress("2"), common.HexToAddress("3")}
			packedOutput, err := PackEchoAddressesOutput(testArgs)
			require.NoError(t, err)
			unpackedOutput, err := UnpackEchoAddressesOutput(packedOutput)
			require.NoError(t, err)
			require.Equal(t, testArgs, unpackedOutput)
			packedInput, err := PackEchoAddresses(testArgs)
			// remove the first 4 bytes of the packed input
			packedInput = packedInput[4:]
			require.NoError(t, err)
			unpackedInput, err := UnpackEchoAddressesInput(packedInput)
			require.NoError(t, err)
			require.Equal(t, testArgs, unpackedInput)

			testArgs2 := []*big.Int{common.Big1, common.Big2, common.Big3}
			packedOutput2, err := PackEchoIntsOutput(testArgs2)
			require.NoError(t, err)
			unpackedOutput2, err := UnpackEchoIntsOutput(packedOutput2)
			require.NoError(t, err)
			require.Equal(t, testArgs2, unpackedOutput2)
			packedInput2, err := PackEchoInts(testArgs2)
			// remove the first 4 bytes of the packed input
			packedInput2 = packedInput2[4:]
			require.NoError(t, err)
			unpackedInput2, err := UnpackEchoIntsInput(packedInput2)
			require.NoError(t, err)
			require.Equal(t, testArgs2, unpackedInput2)

			testArgs3 := [23]uint8{1, 2, 3}
			packedOutput3, err := PackEchoFancyIntsOutput(testArgs3)
			require.NoError(t, err)
			unpackedOutput3, err := UnpackEchoFancyIntsOutput(packedOutput3)
			require.NoError(t, err)
			require.Equal(t, testArgs3, unpackedOutput3)
			packedInput3, err := PackEchoFancyInts(testArgs3)
			// remove the first 4 bytes of the packed input
			packedInput3 = packedInput3[4:]
			require.NoError(t, err)
			unpackedInput3, err := UnpackEchoFancyIntsInput(packedInput3)
			require.NoError(t, err)
			require.Equal(t, testArgs3, unpackedInput3)

			testArgs4 := []bool{true, false, true}
			packedOutput4, err := PackEchoBoolsOutput(testArgs4)
			require.NoError(t, err)
			unpackedOutput4, err := UnpackEchoBoolsOutput(packedOutput4)
			require.NoError(t, err)
			require.Equal(t, testArgs4, unpackedOutput4)
			packedInput4, err := PackEchoBools(testArgs4)
			// remove the first 4 bytes of the packed input
			packedInput4 = packedInput4[4:]
			require.NoError(t, err)
			unpackedInput4, err := UnpackEchoBoolsInput(packedInput4)
			require.NoError(t, err)
			require.Equal(t, testArgs4, unpackedInput4)
		`,
		"",
		false,
	},
	{
		`Fallback`,
		`
			interface Fallback {
				fallback() external payable;

				receive() external payable;
				function testFunction(uint t) external;
			}
			`,
		`[{"stateMutability":"payable","type":"fallback"},{"inputs":[{"internalType":"uint256","name":"t","type":"uint256"}],"name":"testFunction","outputs":[],"stateMutability":"nonpayable","type":"function"},{"stateMutability":"payable","type":"receive"}]`,
		`
			"github.com/stretchr/testify/require"
			"math/big"
			`,
		`
			packedInput, err := PackTestFunction(big.NewInt(5))
			require.NoError(t, err)
			// remove the first 4 bytes of the packed input
			packedInput = packedInput[4:]
			unpackedInput, err := UnpackTestFunctionInput(packedInput)
			require.NoError(t, err)
			require.Equal(t, big.NewInt(5), unpackedInput)
			`,
		"",
		false,
	},
	{
		`Structs`,
		`
		interface Struct {
			struct A {
				bytes32 B;
			}
			function F() external view returns (A[] memory a, uint256[] memory c, bool[] memory d);
			function G() external view returns (A[] memory a);
		}
		`,
		`[{"inputs":[],"name":"F","outputs":[{"components":[{"internalType":"bytes32","name":"B","type":"bytes32"}],"internalType":"struct Structs.A[]","name":"a","type":"tuple[]"},{"internalType":"uint256[]","name":"c","type":"uint256[]"},{"internalType":"bool[]","name":"d","type":"bool[]"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"G","outputs":[{"components":[{"internalType":"bytes32","name":"B","type":"bytes32"}],"internalType":"struct Structs.A[]","name":"a","type":"tuple[]"}],"stateMutability":"view","type":"function"}]`,
		`
			"github.com/stretchr/testify/require"
			"math/big"
			`,
		`
			testOutput := FOutput{
				A: []StructsA{
					{
						B: [32]byte{1},
					},
				},
				C: []*big.Int{big.NewInt(2)},
				D: []bool{true,false},
			}
			packedOutput, err := PackFOutput(testOutput)
			require.NoError(t, err)
			unpackedInput, err := UnpackFOutput(packedOutput)
			require.NoError(t, err)
			require.Equal(t, testOutput, unpackedInput)
			`,
		"",
		false,
	},
	{
		`Underscorer`,
		`
		interface Underscorer {
			function UnderscoredOutput() external returns (int _int, string _string);
		}
		`,
		`[{"inputs":[],"name":"UnderscoredOutput","outputs":[{"internalType":"int256","name":"_int","type":"int256"},{"internalType":"string","name":"_string","type":"string"}],"stateMutability":"nonpayable","type":"function"}]`,
		`
			"github.com/stretchr/testify/require"
			"math/big"
		`,
		`
			testOutput := UnderscoredOutputOutput{
				Int: big.NewInt(5),
				String: "hello",
			}
			packedOutput, err := PackUnderscoredOutputOutput(testOutput)
			require.NoError(t, err)
			unpackedInput, err := UnpackUnderscoredOutputOutput(packedOutput)
			require.NoError(t, err)
			require.Equal(t, testOutput, unpackedInput)
		`,
		"",
		false,
	},
	{
		`OutputCollision`,
		`
		interface Collision {
			function LowerLowerCollision() external returns (int _res, int res, int res_);
		`,
		`[{"inputs":[],"name":"LowerLowerCollision","outputs":[{"internalType":"int256","name":"_res","type":"int256"},{"internalType":"int256","name":"res","type":"int256"},{"internalType":"int256","name":"res_","type":"int256"}],"stateMutability":"nonpayable","type":"function"}]`,
		"",
		"",
		"normalized output name is duplicated",
		false,
	},

	{
		`InputCollision`,
		`
		interface Collision {
			function LowerUpperCollision(int _res, int Res) external;
		}
		`,
		`[{"inputs":[{"internalType":"int256","name":"_res","type":"int256"},{"internalType":"int256","name":"Res","type":"int256"}],"name":"LowerUpperCollision","outputs":[],"stateMutability":"nonpayable","type":"function"}]`, "",
		"",
		"normalized input name is duplicated",
		false,
	},
	{
		`DeeplyNestedArray`,
		`
		interface DeeplyNestedArray {
			function storeDeepUintArray(uint64[3][4][5] arr) external public;
			function retrieveDeepArray() public external view returns (uint64[3][4][5] arr);
		}
		`,
		`[{"inputs":[],"name":"retrieveDeepArray","outputs":[{"internalType":"uint64[3][4][5]","name":"arr","type":"uint64[3][4][5]"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint64[3][4][5]","name":"arr","type":"uint64[3][4][5]"}],"name":"storeDeepUintArray","outputs":[],"stateMutability":"nonpayable","type":"function"}]`, `
			"github.com/stretchr/testify/require"
		`,
		`
			testArr := [5][4][3]uint64{
				{
					{1, 2, 3},
					{4, 5, 6},
					{7, 8, 9},
					{10, 11, 12},
				},
				{
					{13, 14, 15},
					{16, 17, 18},
					{19, 20, 21},
					{22, 23, 24},
				},
			}
			packedInput, err := PackStoreDeepUintArray(testArr)
			require.NoError(t, err)
			// remove the first 4 bytes of the packed input
			packedInput = packedInput[4:]
			unpackedInput, err := UnpackStoreDeepUintArrayInput(packedInput)
			require.NoError(t, err)
			require.Equal(t, testArr, unpackedInput)

			packedOutput, err := PackRetrieveDeepArrayOutput(testArr)
			require.NoError(t, err)
			unpackedOutput, err := UnpackRetrieveDeepArrayOutput(packedOutput)
			require.NoError(t, err)
			require.Equal(t, testArr, unpackedOutput)
		`,
		"",
		false,
	},
	{
		"RangeKeyword",
		`
		interface keywordcontract {
			function functionWithKeywordParameter(uint8 func, uint8 range) external pure;
		}
		`,
		`[{"inputs":[{"internalType":"uint8","name":"func","type":"uint8"},{"internalType":"uint8","name":"range","type":"uint8"}],"name":"functionWithKeywordParameter","outputs":[],"stateMutability":"pure","type":"function"}]`,
		"",
		"",
		"input name func is a keyword",
		false,
	},
	{
		`HelloWorld`,
		`interface IHelloWorld is IAllowList {
			// sayHello returns the stored greeting string
			function sayHello() external view returns (string calldata result);

			// setGreeting  stores the greeting string
			function setGreeting(string calldata response) external;
		}
		`,
		`[{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"readAllowList","outputs":[{"internalType":"uint256","name":"role","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"sayHello","outputs":[{"internalType":"string","name":"result","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"setAdmin","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"setManager","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"setEnabled","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"string","name":"response","type":"string"}],"name":"setGreeting","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"setNone","outputs":[],"stateMutability":"nonpayable","type":"function"}]`,
		`"github.com/stretchr/testify/require"
		 "math/big"
		 "github.com/ava-labs/libevm/common"
		 "github.com/ava-labs/libevm/core/rawdb"
		 "github.com/ava-labs/libevm/core/state"
		 "github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
		 "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
		`,
		`
			testGreeting := "test"
			packedGreeting, err := PackSetGreeting(testGreeting)
			require.NoError(t, err)
			// remove the first 4 bytes of the packed greeting
			packedGreeting = packedGreeting[4:]
			unpackedGreeting, err := UnpackSetGreetingInput(packedGreeting)
			require.NoError(t, err)
			require.Equal(t, testGreeting, unpackedGreeting)

			// test that the allow list is generated correctly
			statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
			require.NoError(t, err)
			wrappedStateDB := extstate.New(statedb)
			address := common.BigToAddress(big.NewInt(1))
			SetHelloWorldAllowListStatus(wrappedStateDB, address, allowlist.EnabledRole)
			role := GetHelloWorldAllowListStatus(wrappedStateDB, address)
			require.Equal(t, role, allowlist.EnabledRole)
		`,
		"",
		true,
	},
	{
		`HelloWorldNoAL`,
		`interface IHelloWorld{
			// sayHello returns the stored greeting string
			function sayHello() external view returns (string calldata result);

			// setGreeting  stores the greeting string
			function setGreeting(string calldata response) external;
		}
		`,
		// This ABI does not contain readAllowlist and setEnabled.
		`[{"inputs":[],"name":"sayHello","outputs":[{"internalType":"string","name":"result","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"setAdmin","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"string","name":"response","type":"string"}],"name":"setGreeting","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"setNone","outputs":[],"stateMutability":"nonpayable","type":"function"}]`,
		`"github.com/stretchr/testify/require"`,
		`
			testGreeting := "test"
			packedGreeting, err := PackSetGreeting(testGreeting)
			require.NoError(t, err)
			// remove the first 4 bytes of the packed greeting
			packedGreeting = packedGreeting[4:]
			unpackedGreeting, err := UnpackSetGreetingInput(packedGreeting)
			require.NoError(t, err)
			require.Equal(t, testGreeting, unpackedGreeting)
		`,
		"",
		false,
	},
	{
		`IEventer`,
		`
		interface IEventer {
			event test(address indexed addressTest, uint indexed intTest, bytes bytesTest);
			event empty();
			event indexed(address addr, int8 indexed num);
			event mixed(address indexed addr, int8 num);
			event dynamic(string indexed idxStr, bytes indexed idxDat, string str, bytes dat);
			event unnamed(uint8 indexed, uint8 indexed);
			function eventTest() external view returns (string memory result);
		}
		`,
		`[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"addressTest","type":"address"},{"indexed":true,"internalType":"uint8","name":"intTest","type":"uint8"},{"indexed":false,"internalType":"bytes","name":"bytesTest","type":"bytes"}],"name":"test","type":"event"},{"inputs":[],"name":"eventTest","outputs":[{"internalType":"string","name":"result","type":"string"}],"stateMutability":"view","type":"function"},{"type":"event","name":"empty","inputs":[]},{"type":"event","name":"indexed","inputs":[{"name":"addr","type":"address","indexed":true},{"name":"num","type":"int8","indexed":true}]},{"type":"event","name":"mixed","inputs":[{"name":"addr","type":"address","indexed":true},{"name":"num","type":"int8"}]},{"type":"event","name":"dynamic","inputs":[{"name":"idxStr","type":"string","indexed":true},{"name":"idxDat","type":"bytes","indexed":true},{"name":"str","type":"string"},{"name":"dat","type":"bytes"}]},{"type":"event","name":"unnamed","inputs":[{"name":"","type":"uint8","indexed":true},{"name":"","type":"uint8","indexed":true}]}]`,
		`"github.com/stretchr/testify/require"
		"github.com/ava-labs/libevm/common"
		"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
		`,
		`
			testAddr := common.Address{1}
			testInt := int8(5)
			testUint := uint8(5)
			testBytes := []byte{1, 2, 3}

			testEventData := TestEventData{
				BytesTest: testBytes,
			}
			topics, data, err := PackTestEvent(testAddr, testUint, testEventData)
			require.NoError(t, err)
			eventID := IEventerABI.Events["test"].ID
			require.Equal(t, eventID, topics[0])
			unpacked, err := UnpackTestEventData(data)
			require.NoError(t, err)
			require.Equal(t, testBytes, unpacked.BytesTest)
			gasCost := GetTestEventGasCost(testEventData)
			require.Equal(t, contract.LogGas + 3 * contract.LogTopicGas + contract.LogDataGas, gasCost)

			topics, data, err = PackEmptyEvent()
			require.NoError(t, err)
			eventID = IEventerABI.Events["empty"].ID
			require.Len(t, topics, 1)
			require.Equal(t, eventID, topics[0])
			require.Equal(t, 0, len(data))
			require.Equal(t, contract.LogGas, GetEmptyEventGasCost())

			topics, data, err = PackIndexedEvent(testAddr, testInt)
			require.NoError(t, err)
			eventID = IEventerABI.Events["indexed"].ID
			require.Len(t, topics, 3)
			require.Equal(t, eventID, topics[0])
			require.Equal(t, common.BytesToHash(testAddr[:]), topics[1])
			require.Equal(t, 0, len(data))
			require.Equal(t, contract.LogGas + 3 * contract.LogTopicGas, GetIndexedEventGasCost())

			testMixedData := MixedEventData{
				Num: testInt,
			}
			topics, data, err = PackMixedEvent(testAddr, testMixedData)
			require.NoError(t, err)
			eventID = IEventerABI.Events["mixed"].ID
			require.Len(t, topics, 2)
			require.Equal(t, eventID, topics[0])
			require.Equal(t, common.BytesToHash(testAddr[:]), topics[1])
			unpackedMixedData, err := UnpackMixedEventData(data)
			require.NoError(t, err)
			require.Equal(t, testMixedData, unpackedMixedData)
			require.Equal(t, contract.LogGas + 2 * contract.LogTopicGas + contract.LogDataGas, GetMixedEventGasCost(testMixedData))

			testDynamicData := DynamicEventData{
				Str:    "test",
				Dat:    testBytes,
			}
			topics, data, err = PackDynamicEvent("test", testBytes, testDynamicData)
			require.NoError(t, err)
			eventID = IEventerABI.Events["dynamic"].ID
			require.Len(t, topics, 3)
			require.Equal(t, eventID, topics[0])
			unpackedDynamicData, err := UnpackDynamicEventData(data)
			require.NoError(t, err)
			require.Equal(t, testDynamicData, unpackedDynamicData)
			require.Equal(t, contract.LogGas + 3 * contract.LogTopicGas + 2 * contract.LogDataGas, GetDynamicEventGasCost(testDynamicData))

			topics, data, err = PackUnnamedEvent(testUint, testUint)
			require.NoError(t, err)
			eventID = IEventerABI.Events["unnamed"].ID
			require.Len(t, topics, 3)
			require.Equal(t, eventID, topics[0])
			require.Equal(t, 0, len(data))
			require.Equal(t, contract.LogGas + 3 * contract.LogTopicGas, GetUnnamedEventGasCost())
	`,
		"",
		false,
	},
	{
		`IEventerAnonymous`,
		`
		interface IEventer {
			event Anonymous(address indexed, uint indexed, bytes) anonymous;
			function eventTest() external view returns (string memory result);
		}
		`,
		`[{"anonymous":true,"inputs":[{"indexed":true,"internalType":"address","name":"","type":"address"},{"indexed":true,"internalType":"uint256","name":"","type":"uint256"},{"indexed":false,"internalType":"bytes","name":"","type":"bytes"}],"name":"Anonymous","type":"event"},{"inputs":[],"name":"eventTest","outputs":[{"internalType":"string","name":"result","type":"string"}],"stateMutability":"view","type":"function"}]`,
		``,
		``,
		errNoAnonymousEvent.Error(),
		false,
	},
}

// Tests that packages generated by the binder can be successfully compiled and
// the requested tester run against it.
func TestPrecompileBind(t *testing.T) {
	// Skip the test if no Go command can be found
	gocmd := runtime.GOROOT() + "/bin/go"
	if !common.FileExist(gocmd) {
		t.Skip("go sdk not found for testing")
	}
	// Create a temporary workspace for the test suite
	ws := t.TempDir()

	pkg := filepath.Join(ws, "precompilebindtest")
	require.NoError(t, os.MkdirAll(pkg, 0o700), "failed to create package")
	// Generate the test suite for all the contracts
	for i, tt := range bindTests {
		t.Run(tt.name, func(t *testing.T) {
			types := []string{tt.name}

			// Generate the binding and create a Go source file in the workspace
			bindedFiles, err := PrecompileBind(types, tt.abi, []string{""}, nil, tt.name, bind.LangGo, nil, nil, "contract.abi", true)
			if tt.errMsg != "" {
				require.ErrorContains(t, err, tt.errMsg) //nolint:forbidigo // uses upstream code
				return
			}
			require.NoError(t, err, "test %d: failed to generate binding: %v", i, err)

			precompilePath := filepath.Join(pkg, tt.name)
			require.NoError(t, os.MkdirAll(precompilePath, 0o700), "failed to create package")
			for _, file := range bindedFiles {
				switch file.FileName {
				case ContractFileName:
					// check if the allowlist functions are generated
					if tt.expectAllowlist {
						require.Contains(t, file.Content, "allowlist.CreateAllowListFunctions(", "generated contract does not contain AllowListFunctions")
					} else {
						require.NotContains(t, file.Content, "allowlist.CreateAllowListFunctions(", "generated contract contains AllowListFunctions")
					}
				case ModuleFileName:
					// change address to a suitable one for testing
					file.Content = strings.Replace(file.Content, `common.HexToAddress("{ASUITABLEHEXADDRESS}")`, `common.HexToAddress("0x03000000000000000000000000000000000000ff")`, 1)
				}
				require.NoError(t, os.WriteFile(filepath.Join(precompilePath, file.FileName), []byte(file.Content), 0o600), "test %d: failed to write binding", i)
			}
			require.NoError(t, os.WriteFile(filepath.Join(precompilePath, "contract.abi"), []byte(tt.abi), 0o600), "test %d: failed to write binding", i)

			// Generate the test file with the injected test code
			code := fmt.Sprintf(`
			package %s

			import (
				"testing"
				%s
			)

			func Test%s(t *testing.T) {
				%s
			}
		`, tt.name, tt.imports, tt.name, tt.tester)
			require.NoError(t, os.WriteFile(filepath.Join(precompilePath, strings.ToLower(tt.name)+"_test.go"), []byte(code), 0o600), "test %d: failed to write tests", i)
		})
	}

	moder := exec.Command(gocmd, "mod", "init", "precompilebindtest")
	moder.Dir = pkg
	out, err := moder.CombinedOutput()
	require.NoError(t, err, "failed to convert binding test to modules: %v\n%s", err, out)

	pwd, _ := os.Getwd()
	replacer := exec.Command(gocmd, "mod", "edit", "-x", "-require", "github.com/ava-labs/avalanchego/graft/subnet-evm@v0.0.0", "-replace", "github.com/ava-labs/avalanchego/graft/subnet-evm="+filepath.Join(pwd, "..", "..", "..", "..")) // Repo root
	replacer.Dir = pkg
	out, err = replacer.CombinedOutput()
	require.NoError(t, err, "failed to replace binding test dependency to current source tree: %v\n%s", err, out)

	replacer = exec.Command(gocmd, "mod", "edit", "-x", "-require", "github.com/ava-labs/avalanchego/graft/evm@v0.0.0", "-replace", "github.com/ava-labs/avalanchego/graft/evm="+filepath.Join(pwd, "..", "..", "..", "..", "..", "evm"))
	replacer.Dir = pkg
	out, err = replacer.CombinedOutput()
	require.NoError(t, err, "failed to replace binding test dependency to current source tree: %v\n%s", err, out)

	tidier := exec.Command(gocmd, "mod", "tidy", "-compat=1.24")
	tidier.Dir = pkg
	out, err = tidier.CombinedOutput()
	require.NoError(t, err, "failed to tidy Go module file: %v\n%s", err, out)

	// Test the entire package and report any failures
	cmd := exec.Command(gocmd, "test", "./...", "-v", "-count", "1")
	cmd.Dir = pkg
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "failed to run binding test: %v\n%s", err, out)
}
