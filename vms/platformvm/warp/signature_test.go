// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
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
	sk     bls.Signer
	vdr    *Validator
}

func (v *testValidator) Compare(o *testValidator) int {
	return v.vdr.Compare(o.vdr)
}

func newTestValidator() *testValidator {
	sk, err := localsigner.New()
	if err != nil {
		panic(err)
	}

	nodeID := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	return &testValidator{
		nodeID: nodeID,
		sk:     sk,
		vdr: &Validator{
			PublicKey:      pk,
			PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
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

func TestNumSigners(t *testing.T) {
	tests := map[string]struct {
		generateSignature func() *BitSetSignature
		count             int
		err               error
	}{
		"empty signers": {
			generateSignature: func() *BitSetSignature {
				return &BitSetSignature{}
			},
		},
		"invalid signers": {
			generateSignature: func() *BitSetSignature {
				return &BitSetSignature{
					Signers: make([]byte, 1),
				}
			},
			err: ErrInvalidBitSet,
		},
		"no signers": {
			generateSignature: func() *BitSetSignature {
				signers := set.NewBits()
				return &BitSetSignature{
					Signers: signers.Bytes(),
				}
			},
		},
		"1 signer": {
			generateSignature: func() *BitSetSignature {
				signers := set.NewBits()
				signers.Add(2)
				return &BitSetSignature{
					Signers: signers.Bytes(),
				}
			},
			count: 1,
		},
		"multiple signers": {
			generateSignature: func() *BitSetSignature {
				signers := set.NewBits()
				signers.Add(2)
				signers.Add(11)
				signers.Add(55)
				signers.Add(93)
				return &BitSetSignature{
					Signers: signers.Bytes(),
				}
			},
			count: 4,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			sig := tt.generateSignature()
			count, err := sig.NumSigners()
			require.Equal(tt.count, count)
			require.ErrorIs(err, tt.err)
		})
	}
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
		name         string
		networkID    uint32
		stateF       func(*gomock.Controller) validators.State
		quorumNum    uint64
		quorumDen    uint64
		msgF         func(*require.Assertions) *Message
		verifyErr    error
		canonicalErr error
	}{
		{
			name:      "can't get subnetID",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, errTest)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
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
			canonicalErr: errTest,
		},
		{
			name:      "can't get validator set",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, errTest)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
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
			canonicalErr: errTest,
		},
		{
			name:      "weight overflow",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
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
						NetworkID:     constants.UnitTestID,
						SourceChainID: sourceChainID,
					},
					Signature: &BitSetSignature{
						Signers: make([]byte, 8),
					},
				}
			},
			canonicalErr: ErrWeightOverflow,
		},
		{
			name:      "invalid bit set index",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
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
			verifyErr: ErrInvalidBitSet,
		},
		{
			name:      "unknown index",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
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
			verifyErr: ErrUnknownValidator,
		},
		{
			name:      "insufficient weight",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 1,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: ErrInsufficientWeight,
		},
		{
			name:      "can't parse sig",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
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
			verifyErr: ErrParseSignature,
		},
		{
			name:      "no validators",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
				)
				require.NoError(err)

				unsignedBytes := unsignedMsg.Bytes()
				vdr0Sig, err := testVdrs[0].sk.Sign(unsignedBytes)
				require.NoError(err)
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
			verifyErr: bls.ErrNoPublicKeys,
		},
		{
			name:      "invalid signature (substitute)",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: ErrInvalidSignature,
		},
		{
			name:      "invalid signature (missing one)",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: ErrInvalidSignature,
		},
		{
			name:      "invalid signature (extra one)",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 3,
			quorumDen: 5,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: ErrInvalidSignature,
		},
		{
			name:      "valid signature",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: nil,
		},
		{
			name:      "valid signature (boundary)",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), sourceChainID).Return(subnetID, nil)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(vdrs, nil)
				return state
			},
			quorumNum: 2,
			quorumDen: 3,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: nil,
		},
		{
			name:      "valid signature (missing key)",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
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
					constants.UnitTestID,
					sourceChainID,
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
				vdr1Sig, err := testVdrs[1].sk.Sign(unsignedBytes)
				require.NoError(err)
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
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
			verifyErr: nil,
		},
		{
			name:      "valid signature (duplicate key)",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
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
					constants.UnitTestID,
					sourceChainID,
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
				vdr2Sig, err := testVdrs[2].sk.Sign(unsignedBytes)
				require.NoError(err)
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
			verifyErr: nil,
		},
		{
			name:      "incorrect networkID",
			networkID: constants.UnitTestID,
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
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
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID+1,
					sourceChainID,
					[]byte{1, 2, 3},
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
			verifyErr: ErrWrongNetworkID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			msg := tt.msgF(require)
			pChainState := tt.stateF(ctrl)

			validators, err := GetCanonicalValidatorSetFromChainID(
				context.Background(),
				pChainState,
				pChainHeight,
				msg.SourceChainID,
			)
			require.ErrorIs(err, tt.canonicalErr)
			if tt.canonicalErr != nil {
				return
			}

			err = msg.Signature.Verify(
				&msg.UnsignedMessage,
				tt.networkID,
				validators,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.verifyErr)
		})
	}
}
