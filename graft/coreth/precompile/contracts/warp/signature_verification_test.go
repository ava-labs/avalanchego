// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type signatureTest struct {
	name         string
	stateF       func(*gomock.Controller) validators.State
	quorumNum    uint64
	quorumDen    uint64
	msgF         func(*require.Assertions) *avalancheWarp.Message
	verifyErr    error
	canonicalErr error
}

// This test copies the test coverage from https://github.com/ava-labs/avalanchego/blob/0117ab96/vms/platformvm/warp/signature_test.go#L137.
// These tests are only expected to fail if there is a breaking change in AvalancheGo that unexpectedly changes behavior.
func TestSignatureVerification(t *testing.T) {
	tests := []signatureTest{
		{
			name: "can't get subnetID",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, errTest)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			canonicalErr: errTest,
		},
		{
			name: "can't get validator set",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(nil, errTest)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			canonicalErr: errTest,
		},
		{
			name: "weight overflow",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(map[ids.NodeID]*validators.GetValidatorOutput{
					testVdrs[0].nodeID: {
						NodeID:    testVdrs[0].nodeID,
						PublicKey: testVdrs[0].vdr.PublicKey,
						Weight:    math.MaxUint64,
					},
					testVdrs[1].nodeID: {
						NodeID:    testVdrs[1].nodeID,
						PublicKey: testVdrs[1].vdr.PublicKey,
						Weight:    math.MaxUint64,
					},
				}, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers: make([]byte, 8),
					},
				)
				require.NoError(err)
				return msg
			},
			canonicalErr: avalancheWarp.ErrWeightOverflow,
		},
		{
			name: "invalid bit set index",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   make([]byte, 1),
						Signature: [bls.SignatureLen]byte{},
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrInvalidBitSet,
		},
		{
			name: "unknown index",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(3) // vdr oob

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: [bls.SignatureLen]byte{},
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrUnknownValidator,
		},
		{
			name: "insufficient weight",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 1,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				// [signers] has weight from [vdr[0], vdr[1]],
				// which is 6, which is less than 9
				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig, err := testVdrs[0].sk.Sign(unsignedBytes)
				require.NoError(err)
				vdr1Sig, err := testVdrs[1].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr0Sig, vdr1Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "can't parse sig",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: [bls.SignatureLen]byte{},
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrParseSignature,
		},
		{
			name: "no validators",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(nil, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig, err := testVdrs[0].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(vdr0Sig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   nil,
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: bls.ErrNoPublicKeys,
		},
		{
			name: "invalid signature (substitute)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig, err := testVdrs[0].sk.Sign(unsignedBytes)
				require.NoError(err)
				// Give sig from vdr[2] even though the bit vector says it
				// should be from vdr[1]
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr0Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrInvalidSignature,
		},
		{
			name: "invalid signature (missing one)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig, err := testVdrs[0].sk.Sign(unsignedBytes)
				require.NoError(err)
				// Don't give the sig from vdr[1]
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(vdr0Sig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrInvalidSignature,
		},
		{
			name: "invalid signature (extra one)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig, err := testVdrs[0].sk.Sign(unsignedBytes)
				require.NoError(err)
				vdr1Sig, err := testVdrs[1].sk.Sign(unsignedBytes)
				require.NoError(err)
				// Give sig from vdr[2] even though the bit vector doesn't have
				// it
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr0Sig, vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: avalancheWarp.ErrInvalidSignature,
		},
		{
			name: "valid signature",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				// [signers] has weight from [vdr[1], vdr[2]],
				// which is 6, which is greater than 4.5
				signers := set.NewBits()
				signers.Add(1)
				signers.Add(2)

				unsignedBytes := unsignedMsg.Bytes()
				vdr1Sig, err := testVdrs[1].sk.Sign(unsignedBytes)
				require.NoError(err)
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: nil,
		},
		{
			name: "valid signature (boundary)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 2,
			quorumDen: 3,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				// [signers] has weight from [vdr[1], vdr[2]],
				// which is 6, which meets the minimum 6
				signers := set.NewBits()
				signers.Add(1)
				signers.Add(2)

				unsignedBytes := unsignedMsg.Bytes()
				vdr1Sig, err := testVdrs[1].sk.Sign(unsignedBytes)
				require.NoError(err)
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: nil,
		},
		{
			name: "valid signature (missing key)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(map[ids.NodeID]*validators.GetValidatorOutput{
					testVdrs[0].nodeID: {
						NodeID:    testVdrs[0].nodeID,
						PublicKey: nil,
						Weight:    testVdrs[0].vdr.Weight,
					},
					testVdrs[1].nodeID: {
						NodeID:    testVdrs[1].nodeID,
						PublicKey: testVdrs[1].vdr.PublicKey,
						Weight:    testVdrs[1].vdr.Weight,
					},
					testVdrs[2].nodeID: {
						NodeID:    testVdrs[2].nodeID,
						PublicKey: testVdrs[2].vdr.PublicKey,
						Weight:    testVdrs[2].vdr.Weight,
					},
				}, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 3,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				// [signers] has weight from [vdr2, vdr3],
				// which is 6, which is greater than 3
				signers := set.NewBits()
				// Note: the bits are shifted because vdr[0]'s key was zeroed
				signers.Add(0) // vdr[1]
				signers.Add(1) // vdr[2]

				unsignedBytes := unsignedMsg.Bytes()
				vdr1Sig, err := testVdrs[1].sk.Sign(unsignedBytes)
				require.NoError(err)
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: nil,
		},
		{
			name: "valid signature (duplicate key)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(sourceSubnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, sourceSubnetID).Return(map[ids.NodeID]*validators.GetValidatorOutput{
					testVdrs[0].nodeID: {
						NodeID:    testVdrs[0].nodeID,
						PublicKey: nil,
						Weight:    testVdrs[0].vdr.Weight,
					},
					testVdrs[1].nodeID: {
						NodeID:    testVdrs[1].nodeID,
						PublicKey: testVdrs[2].vdr.PublicKey,
						Weight:    testVdrs[1].vdr.Weight,
					},
					testVdrs[2].nodeID: {
						NodeID:    testVdrs[2].nodeID,
						PublicKey: testVdrs[2].vdr.PublicKey,
						Weight:    testVdrs[2].vdr.Weight,
					},
				}, nil)
				return state
			},
			quorumNum: 2,
			quorumDen: 3,
			msgF: func(require *require.Assertions) *avalancheWarp.Message {
				unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
					networkID,
					sourceChainID,
					addressedPayloadBytes,
				)
				require.NoError(err)

				// [signers] has weight from [vdr2, vdr3],
				// which is 6, which meets the minimum 6
				signers := set.NewBits()
				// Note: the bits are shifted because vdr[0]'s key was zeroed
				// Note: vdr[1] and vdr[2] were combined because of a shared pk
				signers.Add(0) // vdr[1] + vdr[2]

				unsignedBytes := unsignedMsg.Bytes()
				// Because vdr[1] and vdr[2] share a key, only one of them sign.
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(vdr2Sig))

				msg, err := avalancheWarp.NewMessage(
					unsignedMsg,
					&avalancheWarp.BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			verifyErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			msg := tt.msgF(require)
			pChainState := tt.stateF(ctrl)

			validatorSet, err := avalancheWarp.GetCanonicalValidatorSetFromChainID(
				context.Background(),
				pChainState,
				pChainHeight,
				msg.UnsignedMessage.SourceChainID,
			)
			require.ErrorIs(err, tt.canonicalErr)
			if err != nil {
				return
			}
			err = msg.Signature.Verify(
				&msg.UnsignedMessage,
				networkID,
				validatorSet,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.verifyErr)
		})
	}
}
