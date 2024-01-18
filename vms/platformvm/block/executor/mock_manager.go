// Code generated by MockGen. DO NOT EDIT.
// Source: vms/platformvm/block/executor/manager.go
//
// Generated by this command:
//
//	mockgen -source=vms/platformvm/block/executor/manager.go -destination=vms/platformvm/block/executor/mock_manager.go -package=executor -exclude_interfaces=
//

// Package executor is a generated GoMock package.
package executor

import (
	reflect "reflect"

	ids "github.com/ava-labs/avalanchego/ids"
	snowman "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	set "github.com/ava-labs/avalanchego/utils/set"
	block "github.com/ava-labs/avalanchego/vms/platformvm/block"
	state "github.com/ava-labs/avalanchego/vms/platformvm/state"
	txs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	gomock "go.uber.org/mock/gomock"
)

// MockManager is a mock of Manager interface.
type MockManager struct {
	ctrl     *gomock.Controller
	recorder *MockManagerMockRecorder
}

// MockManagerMockRecorder is the mock recorder for MockManager.
type MockManagerMockRecorder struct {
	mock *MockManager
}

// NewMockManager creates a new mock instance.
func NewMockManager(ctrl *gomock.Controller) *MockManager {
	mock := &MockManager{ctrl: ctrl}
	mock.recorder = &MockManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockManager) EXPECT() *MockManagerMockRecorder {
	return m.recorder
}

// GetBlock mocks base method.
func (m *MockManager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlock", blkID)
	ret0, _ := ret[0].(snowman.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBlock indicates an expected call of GetBlock.
func (mr *MockManagerMockRecorder) GetBlock(blkID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlock", reflect.TypeOf((*MockManager)(nil).GetBlock), blkID)
}

// GetState mocks base method.
func (m *MockManager) GetState(blkID ids.ID) (state.Chain, bool) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetState", blkID)
	ret0, _ := ret[0].(state.Chain)
	ret1, _ := ret[1].(bool)
	return ret0, ret1
}

// GetState indicates an expected call of GetState.
func (mr *MockManagerMockRecorder) GetState(blkID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetState", reflect.TypeOf((*MockManager)(nil).GetState), blkID)
}

// GetStatelessBlock mocks base method.
func (m *MockManager) GetStatelessBlock(blkID ids.ID) (block.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStatelessBlock", blkID)
	ret0, _ := ret[0].(block.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetStatelessBlock indicates an expected call of GetStatelessBlock.
func (mr *MockManagerMockRecorder) GetStatelessBlock(blkID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStatelessBlock", reflect.TypeOf((*MockManager)(nil).GetStatelessBlock), blkID)
}

// LastAccepted mocks base method.
func (m *MockManager) LastAccepted() ids.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LastAccepted")
	ret0, _ := ret[0].(ids.ID)
	return ret0
}

// LastAccepted indicates an expected call of LastAccepted.
func (mr *MockManagerMockRecorder) LastAccepted() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LastAccepted", reflect.TypeOf((*MockManager)(nil).LastAccepted))
}

// NewBlock mocks base method.
func (m *MockManager) NewBlock(arg0 block.Block) snowman.Block {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewBlock", arg0)
	ret0, _ := ret[0].(snowman.Block)
	return ret0
}

// NewBlock indicates an expected call of NewBlock.
func (mr *MockManagerMockRecorder) NewBlock(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewBlock", reflect.TypeOf((*MockManager)(nil).NewBlock), arg0)
}

// Preferred mocks base method.
func (m *MockManager) Preferred() ids.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Preferred")
	ret0, _ := ret[0].(ids.ID)
	return ret0
}

// Preferred indicates an expected call of Preferred.
func (mr *MockManagerMockRecorder) Preferred() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Preferred", reflect.TypeOf((*MockManager)(nil).Preferred))
}

// SetPreference mocks base method.
func (m *MockManager) SetPreference(blkID ids.ID) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetPreference", blkID)
	ret0, _ := ret[0].(bool)
	return ret0
}

// SetPreference indicates an expected call of SetPreference.
func (mr *MockManagerMockRecorder) SetPreference(blkID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetPreference", reflect.TypeOf((*MockManager)(nil).SetPreference), blkID)
}

// VerifyTx mocks base method.
func (m *MockManager) VerifyTx(tx *txs.Tx) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VerifyTx", tx)
	ret0, _ := ret[0].(error)
	return ret0
}

// VerifyTx indicates an expected call of VerifyTx.
func (mr *MockManagerMockRecorder) VerifyTx(tx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VerifyTx", reflect.TypeOf((*MockManager)(nil).VerifyTx), tx)
}

// VerifyUniqueInputs mocks base method.
func (m *MockManager) VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VerifyUniqueInputs", blkID, inputs)
	ret0, _ := ret[0].(error)
	return ret0
}

// VerifyUniqueInputs indicates an expected call of VerifyUniqueInputs.
func (mr *MockManagerMockRecorder) VerifyUniqueInputs(blkID, inputs any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VerifyUniqueInputs", reflect.TypeOf((*MockManager)(nil).VerifyUniqueInputs), blkID, inputs)
}
