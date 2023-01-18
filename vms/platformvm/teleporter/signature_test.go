// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

const pChainHeight uint64 = 1337

var (
	_ utils.Sortable[*testValidator] = (*testValidator)(nil)

	errTest       = errors.New("non-nil error")
	sourceChainID = ids.GenerateTestID()
	subnetID      = ids.GenerateTestID()

	testVdrs []*testValidator
)

type testValidator struct {
	nodeID ids.NodeID
	sk     *bls.SecretKey
	vdr    *Validator
}

func (v *testValidator) Less(o *testValidator) bool {
	return v.vdr.Less(o.vdr)
}

func newTestValidator() *testValidator {
	sk, err := bls.NewSecretKey()
	if err != nil {
		panic(err)
	}

	nodeID := ids.GenerateTestNodeID()
	pk := bls.PublicFromSecretKey(sk)
	return &testValidator{
		nodeID: nodeID,
		sk:     sk,
		vdr: &Validator{
			PublicKey:      pk,
			PublicKeyBytes: bls.PublicKeyToBytes(pk),
			Weight:         3,
			NodeIDs:        []ids.NodeID{nodeID},
		},
	}
}

func init() {
	testVdrs = []*testValidator{
		newTestValidator(),
		newTestValidator(),
		newTestValidator(),
	}
	utils.Sort(testVdrs)
}

func TestSignatureVerification(t *testing.T) {
	vdrs := map[ids.NodeID]*validators.GetValidatorOutput{
		testVdrs[0].nodeID: {
			NodeID:    testVdrs[0].nodeID,
			PublicKey: testVdrs[0].vdr.PublicKey,
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
	}

	tests := []struct {
		name      string
		stateF    func(*gomock.Controller) validators.State
		quorumNum uint64
		quorumDen uint64
		msgF      func(*require.Assertions) *Message
		err       error
	}{
		{
			name: "can't get subnetID",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, errTest)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					nil,
				)
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			err: errTest,
		},
		{
			name: "can't get validator set",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, errTest)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					nil,
				)
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			err: errTest,
		},
		{
			name: "weight overflow",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(map[ids.NodeID]*validators.GetValidatorOutput{
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
			msgF: func(*require.Assertions) *Message {
				return &Message{
					UnsignedMessage: UnsignedMessage{
						SourceChainID: sourceChainID,
					},
					Signature: &BitSetSignature{
						Signers: make([]byte, 8),
					},
				}
			},
			err: ErrWeightOverflow,
		},
		{
			name: "invalid bit set index",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   make([]byte, 1),
						Signature: [bls.SignatureLen]byte{},
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrInvalidBitSet,
		},
		{
			name: "unknown index",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(3) // vdr oob

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: [bls.SignatureLen]byte{},
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrUnknownValidator,
		},
		{
			name: "insufficient weight",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 1,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				// [signers] has weight from [vdr[0], vdr[1]],
				// which is 6, which is less than 9
				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig := bls.Sign(testVdrs[0].sk, unsignedBytes)
				vdr1Sig := bls.Sign(testVdrs[1].sk, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr0Sig, vdr1Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrInsufficientWeight,
		},
		{
			name: "can't parse sig",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: [bls.SignatureLen]byte{},
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrParseSignature,
		},
		{
			name: "no validators",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig := bls.Sign(testVdrs[0].sk, unsignedBytes)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(vdr0Sig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   nil,
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: bls.ErrNoPublicKeys,
		},
		{
			name: "invalid signature (substitute)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig := bls.Sign(testVdrs[0].sk, unsignedBytes)
				// Give sig from vdr[2] even though the bit vector says it
				// should be from vdr[1]
				vdr2Sig := bls.Sign(testVdrs[2].sk, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr0Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrInvalidSignature,
		},
		{
			name: "invalid signature (missing one)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig := bls.Sign(testVdrs[0].sk, unsignedBytes)
				// Don't give the sig from vdr[1]
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(vdr0Sig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrInvalidSignature,
		},
		{
			name: "invalid signature (extra one)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0)
				signers.Add(1)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig := bls.Sign(testVdrs[0].sk, unsignedBytes)
				vdr1Sig := bls.Sign(testVdrs[1].sk, unsignedBytes)
				// Give sig from vdr[2] even though the bit vector doesn't have
				// it
				vdr2Sig := bls.Sign(testVdrs[2].sk, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr0Sig, vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrInvalidSignature,
		},
		{
			name: "valid signature",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				// [signers] has weight from [vdr[1], vdr[2]],
				// which is 6, which is greater than 4.5
				signers := set.NewBits()
				signers.Add(1)
				signers.Add(2)

				unsignedBytes := unsignedMsg.Bytes()
				vdr1Sig := bls.Sign(testVdrs[1].sk, unsignedBytes)
				vdr2Sig := bls.Sign(testVdrs[2].sk, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: nil,
		},
		{
			name: "valid signature (boundary)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 2,
			quorumDen: 3,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				// [signers] has weight from [vdr[1], vdr[2]],
				// which is 6, which meets the minimum 6
				signers := set.NewBits()
				signers.Add(1)
				signers.Add(2)

				unsignedBytes := unsignedMsg.Bytes()
				vdr1Sig := bls.Sign(testVdrs[1].sk, unsignedBytes)
				vdr2Sig := bls.Sign(testVdrs[2].sk, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: nil,
		},
		{
			name: "valid signature (missing key)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(map[ids.NodeID]*validators.GetValidatorOutput{
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
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				// [signers] has weight from [vdr2, vdr3],
				// which is 6, which is greater than 3
				signers := set.NewBits()
				// Note: the bits are shifted because vdr[0]'s key was zeroed
				signers.Add(0) // vdr[1]
				signers.Add(1) // vdr[2]

				unsignedBytes := unsignedMsg.Bytes()
				vdr1Sig := bls.Sign(testVdrs[1].sk, unsignedBytes)
				vdr2Sig := bls.Sign(testVdrs[2].sk, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{vdr1Sig, vdr2Sig})
				require.NoError(err)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: nil,
		},
		{
			name: "valid signature (duplicate key)",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(map[ids.NodeID]*validators.GetValidatorOutput{
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
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					sourceChainID,
					ids.Empty,
					[]byte{1, 2, 3},
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
				vdr2Sig := bls.Sign(testVdrs[2].sk, unsignedBytes)
				aggSigBytes := [bls.SignatureLen]byte{}
				copy(aggSigBytes[:], bls.SignatureToBytes(vdr2Sig))

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: aggSigBytes,
					},
				)
				require.NoError(err)
				return msg
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			msg := tt.msgF(require)
			pChainState := tt.stateF(ctrl)

			err := msg.Signature.Verify(
				context.Background(),
				&msg.UnsignedMessage,
				pChainState,
				pChainHeight,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.err)
		})
	}
}
