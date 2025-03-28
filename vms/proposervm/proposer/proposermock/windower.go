// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/vms/proposervm/proposer (interfaces: Windower)
//
// Generated by this command:
//
//	mockgen -package=proposermock -destination=proposermock/windower.go -mock_names=Windower=Windower . Windower
//

// Package proposermock is a generated GoMock package.
package proposermock

import (
	context "context"
	reflect "reflect"
	time "time"

	ids "github.com/ava-labs/avalanchego/ids"
	gomock "go.uber.org/mock/gomock"
)

// Windower is a mock of Windower interface.
type Windower struct {
	ctrl     *gomock.Controller
	recorder *WindowerMockRecorder
	isgomock struct{}
}

// WindowerMockRecorder is the mock recorder for Windower.
type WindowerMockRecorder struct {
	mock *Windower
}

// NewWindower creates a new mock instance.
func NewWindower(ctrl *gomock.Controller) *Windower {
	mock := &Windower{ctrl: ctrl}
	mock.recorder = &WindowerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Windower) EXPECT() *WindowerMockRecorder {
	return m.recorder
}

// Delay mocks base method.
func (m *Windower) Delay(ctx context.Context, blockHeight, pChainHeight uint64, validatorID ids.NodeID, maxWindows int) (time.Duration, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Delay", ctx, blockHeight, pChainHeight, validatorID, maxWindows)
	ret0, _ := ret[0].(time.Duration)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Delay indicates an expected call of Delay.
func (mr *WindowerMockRecorder) Delay(ctx, blockHeight, pChainHeight, validatorID, maxWindows any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Delay", reflect.TypeOf((*Windower)(nil).Delay), ctx, blockHeight, pChainHeight, validatorID, maxWindows)
}

// ExpectedProposer mocks base method.
func (m *Windower) ExpectedProposer(ctx context.Context, blockHeight, pChainHeight, slot uint64) (ids.NodeID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ExpectedProposer", ctx, blockHeight, pChainHeight, slot)
	ret0, _ := ret[0].(ids.NodeID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ExpectedProposer indicates an expected call of ExpectedProposer.
func (mr *WindowerMockRecorder) ExpectedProposer(ctx, blockHeight, pChainHeight, slot any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ExpectedProposer", reflect.TypeOf((*Windower)(nil).ExpectedProposer), ctx, blockHeight, pChainHeight, slot)
}

// MinDelayForProposer mocks base method.
func (m *Windower) MinDelayForProposer(ctx context.Context, blockHeight, pChainHeight uint64, nodeID ids.NodeID, startSlot uint64) (time.Duration, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MinDelayForProposer", ctx, blockHeight, pChainHeight, nodeID, startSlot)
	ret0, _ := ret[0].(time.Duration)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// MinDelayForProposer indicates an expected call of MinDelayForProposer.
func (mr *WindowerMockRecorder) MinDelayForProposer(ctx, blockHeight, pChainHeight, nodeID, startSlot any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MinDelayForProposer", reflect.TypeOf((*Windower)(nil).MinDelayForProposer), ctx, blockHeight, pChainHeight, nodeID, startSlot)
}

// Proposers mocks base method.
func (m *Windower) Proposers(ctx context.Context, blockHeight, pChainHeight uint64, maxWindows int) ([]ids.NodeID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Proposers", ctx, blockHeight, pChainHeight, maxWindows)
	ret0, _ := ret[0].([]ids.NodeID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Proposers indicates an expected call of Proposers.
func (mr *WindowerMockRecorder) Proposers(ctx, blockHeight, pChainHeight, maxWindows any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Proposers", reflect.TypeOf((*Windower)(nil).Proposers), ctx, blockHeight, pChainHeight, maxWindows)
}
