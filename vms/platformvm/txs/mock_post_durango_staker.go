// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/vms/platformvm/txs (interfaces: Staker)

// Package txs is a generated GoMock package.
package txs

import (
	reflect "reflect"
	time "time"

	ids "github.com/ava-labs/avalanchego/ids"
	bls "github.com/ava-labs/avalanchego/utils/crypto/bls"
	gomock "go.uber.org/mock/gomock"
)

// MockPostDurangoStaker is a mock of Staker interface.
type MockPostDurangoStaker struct {
	ctrl     *gomock.Controller
	recorder *MockPostDurangoStakerMockRecorder
}

// MockPostDurangoStakerMockRecorder is the mock recorder for MockStaker.
type MockPostDurangoStakerMockRecorder struct {
	mock *MockPostDurangoStaker
}

// NewMockPostDurangoStaker creates a new mock instance.
func NewMockPostDurangoStaker(ctrl *gomock.Controller) *MockPostDurangoStaker {
	mock := &MockPostDurangoStaker{ctrl: ctrl}
	mock.recorder = &MockPostDurangoStakerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPostDurangoStaker) EXPECT() *MockPostDurangoStakerMockRecorder {
	return m.recorder
}

// CurrentPriority mocks base method.
func (m *MockPostDurangoStaker) CurrentPriority() Priority {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentPriority")
	ret0, _ := ret[0].(Priority)
	return ret0
}

// CurrentPriority indicates an expected call of CurrentPriority.
func (mr *MockPostDurangoStakerMockRecorder) CurrentPriority() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentPriority", reflect.TypeOf((*MockPostDurangoStaker)(nil).CurrentPriority))
}

// Duration mocks base method.
func (m *MockPostDurangoStaker) Duration() time.Duration {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Duration")
	ret0, _ := ret[0].(time.Duration)
	return ret0
}

// EndTime indicates an expected call of EndTime.
func (mr *MockPostDurangoStakerMockRecorder) Duration() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Duration", reflect.TypeOf((*MockPostDurangoStaker)(nil).Duration))
}

// NodeID mocks base method.
func (m *MockPostDurangoStaker) NodeID() ids.NodeID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NodeID")
	ret0, _ := ret[0].(ids.NodeID)
	return ret0
}

// NodeID indicates an expected call of NodeID.
func (mr *MockPostDurangoStakerMockRecorder) NodeID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NodeID", reflect.TypeOf((*MockPostDurangoStaker)(nil).NodeID))
}

// PublicKey mocks base method.
func (m *MockPostDurangoStaker) PublicKey() (*bls.PublicKey, bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PublicKey")
	ret0, _ := ret[0].(*bls.PublicKey)
	ret1, _ := ret[1].(bool)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// PublicKey indicates an expected call of PublicKey.
func (mr *MockPostDurangoStakerMockRecorder) PublicKey() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PublicKey", reflect.TypeOf((*MockPostDurangoStaker)(nil).PublicKey))
}

// SubnetID mocks base method.
func (m *MockPostDurangoStaker) SubnetID() ids.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SubnetID")
	ret0, _ := ret[0].(ids.ID)
	return ret0
}

// SubnetID indicates an expected call of SubnetID.
func (mr *MockPostDurangoStakerMockRecorder) SubnetID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SubnetID", reflect.TypeOf((*MockPostDurangoStaker)(nil).SubnetID))
}

// Weight mocks base method.
func (m *MockPostDurangoStaker) Weight() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Weight")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// Weight indicates an expected call of Weight.
func (mr *MockPostDurangoStakerMockRecorder) Weight() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Weight", reflect.TypeOf((*MockPostDurangoStaker)(nil).Weight))
}
