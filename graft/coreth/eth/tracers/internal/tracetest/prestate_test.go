// (c) 2023, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2021 The go-ethereum Authors
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

package tracetest

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/tests"
	"github.com/ethereum/go-ethereum/common"
)

// prestateTrace is the result of a prestateTrace run.
type prestateTrace = map[common.Address]*account

type account struct {
	Balance string                      `json:"balance"`
	Code    string                      `json:"code"`
	Nonce   uint64                      `json:"nonce"`
	Storage map[common.Hash]common.Hash `json:"storage"`
}

// testcase defines a single test to check the stateDiff tracer against.
type testcase struct {
	Genesis      *core.Genesis   `json:"genesis"`
	Context      *callContext    `json:"context"`
	Input        string          `json:"input"`
	TracerConfig json.RawMessage `json:"tracerConfig"`
	Result       interface{}     `json:"result"`
}

func TestPrestateTracerLegacy(t *testing.T) {
	testPrestateDiffTracer("prestateTracerLegacy", "prestate_tracer_legacy", t)
}

func TestPrestateTracer(t *testing.T) {
	testPrestateDiffTracer("prestateTracer", "prestate_tracer", t)
}

func TestPrestateWithDiffModeTracer(t *testing.T) {
	testPrestateDiffTracer("prestateTracer", "prestate_tracer_with_diff_mode", t)
}

func TestPrestateWithDiffModeANTTracer(t *testing.T) {
	testPrestateDiffTracer("prestateTracer", "prestate_tracer_ant", t)
}

func testPrestateDiffTracer(tracerName string, dirPath string, t *testing.T) {
	files, err := os.ReadDir(filepath.Join("testdata", dirPath))
	if err != nil {
		t.Fatalf("failed to retrieve tracer test suite: %v", err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		file := file // capture range variable
		t.Run(camel(strings.TrimSuffix(file.Name(), ".json")), func(t *testing.T) {
			t.Parallel()

			var (
				test = new(testcase)
				tx   = new(types.Transaction)
			)
			// Call tracer test found, read if from disk
			if blob, err := os.ReadFile(filepath.Join("testdata", dirPath, file.Name())); err != nil {
				t.Fatalf("failed to read testcase: %v", err)
			} else if err := json.Unmarshal(blob, test); err != nil {
				t.Fatalf("failed to parse testcase: %v", err)
			}
			if err := tx.UnmarshalBinary(common.FromHex(test.Input)); err != nil {
				t.Fatalf("failed to parse testcase input: %v", err)
			}
			// Configure a blockchain with the given prestate
			var (
				blockNumber = new(big.Int).SetUint64(uint64(test.Context.Number))
				signer      = types.MakeSigner(test.Genesis.Config, blockNumber, uint64(test.Context.Time))
				origin, _   = signer.Sender(tx)
				txContext   = vm.TxContext{
					Origin:   origin,
					GasPrice: tx.GasPrice(),
				}
				context = vm.BlockContext{
					CanTransfer:       core.CanTransfer,
					CanTransferMC:     core.CanTransferMC,
					Transfer:          core.Transfer,
					TransferMultiCoin: core.TransferMultiCoin,
					Coinbase:          test.Context.Miner,
					BlockNumber:       blockNumber,
					Time:              uint64(test.Context.Time),
					Difficulty:        (*big.Int)(test.Context.Difficulty),
					GasLimit:          uint64(test.Context.GasLimit),
					BaseFee:           test.Genesis.BaseFee,
				}
				_, statedb = tests.MakePreState(rawdb.NewMemoryDatabase(), test.Genesis.Alloc, false)
			)
			tracer, err := tracers.DefaultDirectory.New(tracerName, new(tracers.Context), test.TracerConfig)
			if err != nil {
				t.Fatalf("failed to create call tracer: %v", err)
			}
			evm := vm.NewEVM(context, txContext, statedb, test.Genesis.Config, vm.Config{Tracer: tracer})
			msg, err := core.TransactionToMessage(tx, signer, nil)
			if err != nil {
				t.Fatalf("failed to prepare transaction for tracing: %v", err)
			}
			st := core.NewStateTransition(evm, msg, new(core.GasPool).AddGas(tx.Gas()))
			if _, err = st.TransitionDb(); err != nil {
				t.Fatalf("failed to execute transaction: %v", err)
			}
			// Retrieve the trace result and compare against the expected
			res, err := tracer.GetResult()
			if err != nil {
				t.Fatalf("failed to retrieve trace result: %v", err)
			}
			// The legacy javascript calltracer marshals json in js, which
			// is not deterministic (as opposed to the golang json encoder).
			if strings.HasSuffix(dirPath, "_legacy") {
				// This is a tweak to make it deterministic. Can be removed when
				// we remove the legacy tracer.
				var x prestateTrace
				json.Unmarshal(res, &x)
				res, _ = json.Marshal(x)
			}
			want, err := json.Marshal(test.Result)
			if err != nil {
				t.Fatalf("failed to marshal test: %v", err)
			}
			if string(want) != string(res) {
				t.Fatalf("trace mismatch\n have: %v\n want: %v\n", string(res), string(want))
			}
		})
	}
}
