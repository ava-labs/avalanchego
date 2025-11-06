// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/tests"
)

func TestPrestateWithDiffModeANTTracer(t *testing.T) {
	testPrestateDiffTracer("prestateTracer", "prestate_tracer_ant", t)
}

// testPrestateDiffTracer is adapted from the original testPrestateDiffTracer in
// eth/tracers/internal/tracetest/prestate_test.go.
func testPrestateDiffTracer(tracerName string, dirPath string, t *testing.T) {
	files, err := os.ReadDir(filepath.Join("testdata", dirPath))
	require.NoError(t, err, "failed to retrieve tracer test suite")
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		t.Run(camel(strings.TrimSuffix(file.Name(), ".json")), func(t *testing.T) {
			t.Parallel()

			var (
				test = new(testcase)
				tx   = new(types.Transaction)
			)
			// Call tracer test found, read if from disk
			blob, err := os.ReadFile(filepath.Join("testdata", dirPath, file.Name()))
			require.NoError(t, err, "failed to read testcase")
			require.NoError(t, json.Unmarshal(blob, test), "failed to parse testcase")
			require.NoError(t, tx.UnmarshalBinary(common.FromHex(test.Input)), "failed to parse testcase input")

			// Configure a blockchain with the given prestate
			var (
				signer  = types.MakeSigner(test.Genesis.Config, new(big.Int).SetUint64(uint64(test.Context.Number)), uint64(test.Context.Time))
				context = vm.BlockContext{
					CanTransfer: core.CanTransfer,
					Transfer:    core.Transfer,
					Coinbase:    test.Context.Miner,
					BlockNumber: new(big.Int).SetUint64(uint64(test.Context.Number)),
					Time:        uint64(test.Context.Time),
					Difficulty:  (*big.Int)(test.Context.Difficulty),
					GasLimit:    uint64(test.Context.GasLimit),
					BaseFee:     test.Genesis.BaseFee,
					Header: &types.Header{
						Number: new(big.Int).SetUint64(uint64(test.Context.Number)),
						Time:   uint64(test.Context.Time),
					},
				}
				state = tests.MakePreState(rawdb.NewMemoryDatabase(), test.Genesis.Alloc, false, rawdb.HashScheme)
			)
			defer state.Close()

			tracer, err := tracers.DefaultDirectory.New(tracerName, new(tracers.Context), test.TracerConfig)
			require.NoError(t, err, "failed to create call tracer")
			msg, err := core.TransactionToMessage(tx, signer, context.BaseFee)
			require.NoError(t, err, "failed to prepare transaction for tracing")
			evm := vm.NewEVM(context, core.NewEVMTxContext(msg), state.StateDB, test.Genesis.Config, vm.Config{Tracer: tracer})
			st := core.NewStateTransition(evm, msg, new(core.GasPool).AddGas(tx.Gas()))
			_, err = st.TransitionDb()
			require.NoError(t, err, "failed to execute transaction")
			// Retrieve the trace result and compare against the expected
			res, err := tracer.GetResult()
			require.NoError(t, err, "failed to retrieve trace result")
			want, err := json.Marshal(test.Result)
			require.NoError(t, err, "failed to marshal test result")
			require.Equal(t, string(want), string(res), "trace mismatch")
		})
	}
}

// testcase defines a single test to check the stateDiff tracer against.
type testcase struct {
	Genesis      *core.Genesis   `json:"genesis"`
	Context      *callContext    `json:"context"`
	Input        string          `json:"input"`
	TracerConfig json.RawMessage `json:"tracerConfig"`
	Result       interface{}     `json:"result"`
}

type callContext struct {
	Number     math.HexOrDecimal64   `json:"number"`
	Difficulty *math.HexOrDecimal256 `json:"difficulty"`
	Time       math.HexOrDecimal64   `json:"timestamp"`
	GasLimit   math.HexOrDecimal64   `json:"gasLimit"`
	Miner      common.Address        `json:"miner"`
}

// camel converts a snake cased input string into a camel cased output.
func camel(str string) string {
	pieces := strings.Split(str, "_")
	for i := 1; i < len(pieces); i++ {
		pieces[i] = string(unicode.ToUpper(rune(pieces[i][0]))) + pieces[i][1:]
	}
	return strings.Join(pieces, "")
}
