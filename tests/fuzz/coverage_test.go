// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// TestTxCoverage fails if a tx type in [platform.TxVisitor] is neither in
// txTypes, which issue draws from, nor a known todo. A new tx type then fails
// this test until it is classified, so the fuzzer's coverage cannot fall
// behind the P-Chain. It logs the todo types as a standing reminder.
func TestTxCoverage(t *testing.T) {
	todo := map[string]string{
		"RegisterL1ValidatorTx":  "needs a signed Warp message",
		"SetL1ValidatorWeightTx": "needs a signed Warp message",
	}

	drawn := make(map[string]bool, len(txTypes))
	for _, proto := range txTypes {
		drawn[txName(proto)] = true
	}

	visitor := reflect.TypeFor[platform.TxVisitor]()
	for i := range visitor.NumMethod() {
		name := visitor.Method(i).Name
		reason, isTodo := todo[name]
		switch {
		case drawn[name] && isTodo:
			t.Errorf("tx type %s is in txTypes and in todo: remove it from todo", name)
		case !drawn[name] && !isTodo:
			t.Errorf("tx type %s is not in txTypes and not a known todo: add it to one", name)
		case isTodo:
			t.Logf("TODO: issue does not draw %s (%s)", name, reason)
		}
	}
}
