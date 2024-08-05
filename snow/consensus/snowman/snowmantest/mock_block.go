// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/snow/consensus/snowman (interfaces: Block)
//
// Generated by this command:
//
//	mockgen -package=snowmantest -destination=snow/consensus/snowman/snowmantest/mock_block.go github.com/ava-labs/avalanchego/snow/consensus/snowman Block
//

// Package snowmantest is a generated GoMock package.
package snowmantest

import (
	context "context"
	reflect "reflect"
	time "time"

	ids "github.com/ava-labs/avalanchego/ids"
	gomock "go.uber.org/mock/gomock"
)

// MockBlock is a mock of Block interface.
type MockBlock struct {
	ctrl     *gomock.Controller
	recorder *MockBlockMockRecorder
}

// MockBlockMockRecorder is the mock recorder for MockBlock.
type MockBlockMockRecorder struct {
	mock *MockBlock
}

// NewMockBlock creates a new mock instance.
func NewMockBlock(ctrl *gomock.Controller) *MockBlock {
	mock := &MockBlock{ctrl: ctrl}
	mock.recorder = &MockBlockMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBlock) EXPECT() *MockBlockMockRecorder {
	return m.recorder
}

// Accept mocks base method.
func (m *MockBlock) Accept(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Accept", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Accept indicates an expected call of Accept.
func (mr *MockBlockMockRecorder) Accept(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Accept", reflect.TypeOf((*MockBlock)(nil).Accept), arg0)
}

// Bytes mocks base method.
func (m *MockBlock) Bytes() []byte {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Bytes")
	ret0, _ := ret[0].([]byte)
	return ret0
}

// Bytes indicates an expected call of Bytes.
func (mr *MockBlockMockRecorder) Bytes() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Bytes", reflect.TypeOf((*MockBlock)(nil).Bytes))
}

// Height mocks base method.
func (m *MockBlock) Height() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Height")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// Height indicates an expected call of Height.
func (mr *MockBlockMockRecorder) Height() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Height", reflect.TypeOf((*MockBlock)(nil).Height))
}

// ID mocks base method.
func (m *MockBlock) ID() ids.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(ids.ID)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MockBlockMockRecorder) ID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*MockBlock)(nil).ID))
}

// Parent mocks base method.
func (m *MockBlock) Parent() ids.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Parent")
	ret0, _ := ret[0].(ids.ID)
	return ret0
}

// Parent indicates an expected call of Parent.
func (mr *MockBlockMockRecorder) Parent() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Parent", reflect.TypeOf((*MockBlock)(nil).Parent))
}

// Reject mocks base method.
func (m *MockBlock) Reject(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Reject", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Reject indicates an expected call of Reject.
func (mr *MockBlockMockRecorder) Reject(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Reject", reflect.TypeOf((*MockBlock)(nil).Reject), arg0)
}

// Timestamp mocks base method.
func (m *MockBlock) Timestamp() time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Timestamp")
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// Timestamp indicates an expected call of Timestamp.
func (mr *MockBlockMockRecorder) Timestamp() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Timestamp", reflect.TypeOf((*MockBlock)(nil).Timestamp))
}

// Verify mocks base method.
func (m *MockBlock) Verify(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockBlockMockRecorder) Verify(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockBlock)(nil).Verify), arg0)
}
