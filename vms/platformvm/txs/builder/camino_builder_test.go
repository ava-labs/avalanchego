// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestCaminoBuilderTxAddressState(t *testing.T) {
	caminoConfig := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	b := newCaminoBuilder(true, caminoConfig)

	tests := map[string]struct {
		remove      bool
		state       uint8
		address     ids.ShortID
		expectedErr error
	}{
		"KYC Role: Add": {
			remove:      false,
			state:       txs.AddressStateRoleKyc,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"KYC Role: Remove": {
			remove:      true,
			state:       txs.AddressStateRoleKyc,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"Admin Role: Add": {
			remove:      false,
			state:       txs.AddressStateRoleAdmin,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"Admin Role: Remove": {
			remove:      true,
			state:       txs.AddressStateRoleAdmin,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"Empty Address": {
			remove:      false,
			state:       txs.AddressStateRoleKyc,
			address:     ids.ShortEmpty,
			expectedErr: txs.ErrEmptyAddress,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := b.NewAddAddressStateTx(
				tt.address,
				tt.remove,
				tt.state,
				caminoPreFundedKeys,
				ids.ShortEmpty,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoBuilderNewAddValidatorTxNodeSig(t *testing.T) {
	nodeKey1, nodeID1 := nodeid.GenerateCaminoNodeKeyAndID()
	nodeKey2, _ := nodeid.GenerateCaminoNodeKeyAndID()

	tests := map[string]struct {
		caminoConfig genesis.Camino
		nodeID       ids.NodeID
		nodeKey      *crypto.PrivateKeySECP256K1R
		expectedErr  error
	}{
		"Happy path, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey1,
			expectedErr: nil,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey2,
			expectedErr: errNodeKeyMissing,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit true, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey2,
			expectedErr: errNodeKeyMissing,
		},
		// No need to add tests with VerifyNodeSignature set to false
		// because the error will rise from the execution
	}
	for name, tt := range tests {
		t.Run("AddValidatorTx: "+name, func(t *testing.T) {
			b := newCaminoBuilder(true, tt.caminoConfig)

			_, err := b.NewAddValidatorTx(
				defaultCaminoValidatorWeight,
				uint64(defaultValidateStartTime.Unix()+1),
				uint64(defaultValidateEndTime.Unix()),
				tt.nodeID,
				ids.ShortEmpty,
				reward.PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], tt.nodeKey},
				ids.ShortEmpty,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("AddSubnetValidatorTx: "+name, func(t *testing.T) {
			b := newCaminoBuilder(true, tt.caminoConfig)

			_, err := b.NewAddSubnetValidatorTx(
				defaultCaminoValidatorWeight,
				uint64(defaultValidateStartTime.Unix()+1),
				uint64(defaultValidateEndTime.Unix()),
				tt.nodeID,
				testSubnet1.ID(),
				[]*crypto.PrivateKeySECP256K1R{testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], tt.nodeKey},
				ids.ShortEmpty,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
