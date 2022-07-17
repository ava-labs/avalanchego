// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestNewExportTx(t *testing.T) {
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	type test struct {
		description        string
		destinationChainID ids.ID
		sourceKeys         []*crypto.PrivateKeySECP256K1R
		timestamp          time.Time
		shouldErr          bool
		shouldVerify       bool
	}

	sourceKey := preFundedKeys[0]

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
			timestamp:          env.config.ApricotPhase5Time,
			shouldErr:          false,
			shouldVerify:       true,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			assert := assert.New(t)
			tx, err := env.txBuilder.NewExportTx(
				defaultBalance-defaultTxFee, // Amount of tokens to export
				tt.destinationChainID,
				to,
				tt.sourceKeys,
				ids.ShortEmpty, // Change address
			)
			if tt.shouldErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			fakedState, err := state.NewDiff(lastAcceptedID, env.backend.StateVersions)
			assert.NoError(err)

			fakedState.SetTimestamp(tt.timestamp)

			fakedParent := ids.GenerateTestID()
			env.backend.StateVersions.SetState(fakedParent, fakedState)

			verifier := MempoolTxVerifier{
				Backend:  &env.backend,
				ParentID: fakedParent,
				Tx:       tx,
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
