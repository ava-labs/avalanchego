// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm/conflicts"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/utils"
)

func TestTxStatus(t *testing.T) {
	_, s := newSerializer(t, nil)
	stx, err := vertex.Wrap(0, []byte{0}, nil)
	assert.NoError(t, err)
	tr := &conflicts.TestTransition{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
		EpochV:  1,
	}

	tx := tx{
		serializer: s,
		tx:         stx,
		tr:         tr,
	}

	if status := tx.Status(); status != choices.Rejected {
		t.Fatalf("wrong tx status returned")
	}
}

func TestTxInvalidDueToRestrictions(t *testing.T) {
	vm, s := newSerializer(t, nil)

	tr0 := &conflicts.TestTransition{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
		EpochV:  0,
		BytesV:  utils.RandomBytes(32),
	}
	vm.GetF = func(id ids.ID) (conflicts.Transition, error) {
		switch id {
		case tr0.ID():
			return tr0, nil
		default:
			return nil, errUnknownVertex
		}
	}

	tr1 := &conflicts.TestTransition{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
		BytesV:  utils.RandomBytes(32),
	}
	stx1, err := vertex.Wrap(1, tr1.Bytes(), []ids.ID{tr0.ID()})
	assert.NoError(t, err)
	tx1 := tx{
		serializer: s,
		tx:         stx1,
		tr:         tr1,
	}

	err = tx1.Verify()
	assert.Error(t, err, "tx1 should have failed verification")
}
