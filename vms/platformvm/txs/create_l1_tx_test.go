// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

func TestCreateL1TxSyntacticVerify(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	var (
		ctx        = snowtest.Context(t, ids.GenerateTestID())
		validBaseTx = BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
			},
		}
		validVMID          = ids.GenerateTestID()
		invalidAddress     = make(types.JSONByteSlice, MaxSubnetAddressLength+1)
		validValidators    = []*ConvertSubnetToL1Validator{
			{
				NodeID:                utils.RandomBytes(ids.NodeIDLen),
				Weight:                1,
				Balance:               1,
				Signer:                *pop,
				RemainingBalanceOwner: message.PChainOwner{},
				DeactivationOwner:     message.PChainOwner{},
			},
		}
	)

	tests := []struct {
		name        string
		tx          *CreateL1Tx
		expectedErr error
	}{
		{
			name:        "nil tx",
			tx:          nil,
			expectedErr: ErrNilTx,
		},
		{
			name: "already verified",
			// The tx includes invalid data to verify that a cached result is
			// returned.
			tx: &CreateL1Tx{
				BaseTx: BaseTx{
					SyntacticallyVerified: true,
				},
				VMID:           ids.Empty,
				ManagerAddress: invalidAddress,
				Validators:     nil,
			},
			expectedErr: nil,
		},
		{
			name: "invalid VMID",
			tx: &CreateL1Tx{
				BaseTx:     validBaseTx,
				VMID:       ids.Empty,
				Validators: validValidators,
			},
			expectedErr: errInvalidVMID,
		},
		{
			name: "name too long",
			tx: &CreateL1Tx{
				BaseTx:     validBaseTx,
				ChainName:  string(make([]byte, MaxNameLen+1)),
				VMID:       validVMID,
				Validators: validValidators,
			},
			expectedErr: errNameTooLong,
		},
		{
			name: "address too long",
			tx: &CreateL1Tx{
				BaseTx:         validBaseTx,
				VMID:           validVMID,
				ManagerAddress: invalidAddress,
				Validators:     validValidators,
			},
			expectedErr: ErrAddressTooLong,
		},
		{
			name: "no validators",
			tx: &CreateL1Tx{
				BaseTx:     validBaseTx,
				VMID:       validVMID,
				Validators: nil,
			},
			expectedErr: ErrConvertMustIncludeValidators,
		},
		{
			name: "validators not sorted",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID: []byte{
							0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00,
						},
					},
					{
						NodeID: []byte{
							0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00,
						},
					},
				},
			},
			expectedErr: ErrConvertValidatorsNotSortedAndUnique,
		},
		{
			name: "duplicate validators",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID: []byte{
							0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00,
						},
					},
					{
						NodeID: []byte{
							0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00,
						},
					},
				},
			},
			expectedErr: ErrConvertValidatorsNotSortedAndUnique,
		},
		{
			name: "fxIDs not sorted",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				FxIDs: []ids.ID{
					{0x02},
					{0x01},
				},
				Validators: validValidators,
			},
			expectedErr: errFxIDsNotSortedAndUnique,
		},
		{
			name: "duplicate fxIDs",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				FxIDs: []ids.ID{
					{0x01},
					{0x01},
				},
				Validators: validValidators,
			},
			expectedErr: errFxIDsNotSortedAndUnique,
		},
		{
			name: "genesis too long",
			tx: &CreateL1Tx{
				BaseTx:      validBaseTx,
				VMID:        validVMID,
				GenesisData: make([]byte, MaxGenesisLen+1),
				Validators:  validValidators,
			},
			expectedErr: errGenesisTooLong,
		},
		{
			name: "invalid BaseTx",
			tx: &CreateL1Tx{
				BaseTx:     BaseTx{},
				VMID:       validVMID,
				Validators: validValidators,
			},
			expectedErr: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid validator weight",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen),
						Weight:                0,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
			},
			expectedErr: ErrZeroWeight,
		},
		{
			name: "invalid validator nodeID length",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen + 1),
						Weight:                1,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
			},
			expectedErr: hashing.ErrInvalidHashLen,
		},
		{
			name: "invalid validator nodeID",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                ids.EmptyNodeID[:],
						Weight:                1,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
			},
			expectedErr: errEmptyNodeID,
		},
		{
			name: "invalid validator pop",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen),
						Weight:                1,
						Signer:                signer.ProofOfPossession{},
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
			},
			expectedErr: bls.ErrFailedPublicKeyDecompress,
		},
		{
			name: "invalid validator remaining balance owner",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID: utils.RandomBytes(ids.NodeIDLen),
						Weight: 1,
						Signer: *pop,
						RemainingBalanceOwner: message.PChainOwner{
							Threshold: 1,
						},
						DeactivationOwner: message.PChainOwner{},
					},
				},
			},
			expectedErr: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "invalid validator deactivation owner",
			tx: &CreateL1Tx{
				BaseTx: validBaseTx,
				VMID:   validVMID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen),
						Weight:                1,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner: message.PChainOwner{
							Threshold: 1,
						},
					},
				},
			},
			expectedErr: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "passes verification",
			tx: &CreateL1Tx{
				BaseTx:     validBaseTx,
				VMID:       validVMID,
				Validators: validValidators,
			},
			expectedErr: nil,
		},
		{
			name: "passes verification with all fields",
			tx: &CreateL1Tx{
				BaseTx:      validBaseTx,
				ChainName:   "Test L1",
				VMID:        validVMID,
				FxIDs:       []ids.ID{{0x01}, {0x02}},
				GenesisData: make([]byte, units.KiB),
				ManagerChainID: ids.GenerateTestID(),
				ManagerAddress: make([]byte, 20),
				Validators:  validValidators,
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.tx.SyntacticVerify(ctx)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			require.True(test.tx.SyntacticallyVerified)
		})
	}
}

func TestCreateL1TxBlockchainID(t *testing.T) {
	subnetID := ids.ID{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
	}

	// Compute the expected blockchainID manually per the ACP spec:
	// SHA256(subnetID[32] || 0x00[1] || chainIndex[4]) = SHA256(37 bytes)
	packer := wrappers.Packer{Bytes: make([]byte, ids.IDLen+1+wrappers.IntLen)}
	packer.PackFixedBytes(subnetID[:])
	packer.PackByte(0x00)
	packer.PackInt(0)
	expected := ids.ID(hashing.ComputeHash256Array(packer.Bytes))

	tx := &CreateL1Tx{}
	blockchainID := tx.BlockchainID(subnetID)

	require := require.New(t)
	require.Equal(expected, blockchainID)

	// Verify it differs from both the subnetID and validationID derivation.
	// validationID = subnetID.Append(0) = SHA256(subnetID[32] || chainIndex[4])
	// which is 36 bytes — the missing 0x00 byte makes them different.
	validationID := subnetID.Append(0)
	require.NotEqual(ids.ID(validationID), blockchainID)
	require.NotEqual(subnetID, blockchainID)
}
