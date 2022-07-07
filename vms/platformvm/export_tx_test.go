// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestNewExportTx(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	type test struct {
		description        string
		destinationChainID ids.ID
		sourceKeys         []*crypto.PrivateKeySECP256K1R
		timestamp          time.Time
		shouldErr          bool
		shouldVerify       bool
	}

	sourceKey := keys[0]

	tests := []test{
		{
			description:        "P->X export",
			destinationChainID: xChainID,
			sourceKeys:         []*crypto.PrivateKeySECP256K1R{sourceKey},
			timestamp:          defaultValidateStartTime,
			shouldErr:          false,
			shouldVerify:       true,
		},
		{
			description:        "P->C export",
			destinationChainID: cChainID,
			sourceKeys:         []*crypto.PrivateKeySECP256K1R{sourceKey},
			timestamp:          vm.ApricotPhase5Time,
			shouldErr:          false,
			shouldVerify:       true,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			assert := assert.New(t)
			tx, err := vm.txBuilder.NewExportTx(defaultBalance-defaultTxFee, tt.destinationChainID, to, tt.sourceKeys, ids.ShortEmpty)
			if tt.shouldErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			fakeState, err := state.NewDiff(
				vm.preferred,
				vm.stateVersions,
			)
			if err != nil {
				t.Fatal(err)
			}
			fakeState.SetTimestamp(tt.timestamp)

			fakeBlockID := ids.GenerateTestID()
			vm.stateVersions.SetState(fakeBlockID, fakeState)

			verifier := mempoolTxVerifier{
				vm:            vm,
				parentID:      fakeBlockID,
				stateVersions: vm.stateVersions,
				tx:            tx,
			}
			err = tx.Unsigned.Visit(&verifier)
			if tt.shouldVerify {
				assert.NoError(err)
			} else {
				assert.Error(err)
			}
		})
	}
}
