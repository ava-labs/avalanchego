// Code generated by MockGen. DO NOT EDIT.
// Source: vms/platformvm/block/block.go
//
// Generated by this command:
//
//	mockgen -source=vms/platformvm/block/block.go -destination=vms/platformvm/block/mock_block.go -package=block -exclude_interfaces=BanffBlock -mock_names=MockBlock=MockBlock
//

// Package block is a generated GoMock package.
package block

import (
	reflect "reflect"

	ids "github.com/ava-labs/avalanchego/ids"
	snow "github.com/ava-labs/avalanchego/snow"
	txs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
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

// InitCtx mocks base method.
func (m *MockBlock) InitCtx(ctx *snow.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "InitCtx", ctx)
}

// InitCtx indicates an expected call of InitCtx.
func (mr *MockBlockMockRecorder) InitCtx(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InitCtx", reflect.TypeOf((*MockBlock)(nil).InitCtx), ctx)
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

// Txs mocks base method.
func (m *MockBlock) Txs() []*txs.Tx {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Txs")
	ret0, _ := ret[0].([]*txs.Tx)
	return ret0
}

// Txs indicates an expected call of Txs.
func (mr *MockBlockMockRecorder) Txs() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Txs", reflect.TypeOf((*MockBlock)(nil).Txs))
}

// Visit mocks base method.
func (m *MockBlock) Visit(visitor Visitor) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Visit", visitor)
	ret0, _ := ret[0].(error)
	return ret0
}

// Visit indicates an expected call of Visit.
func (mr *MockBlockMockRecorder) Visit(visitor any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Visit", reflect.TypeOf((*MockBlock)(nil).Visit), visitor)
}

// initialize mocks base method.
func (m *MockBlock) initialize(bytes []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "initialize", bytes)
	ret0, _ := ret[0].(error)
	return ret0
}

// initialize indicates an expected call of initialize.
func (mr *MockBlockMockRecorder) initialize(bytes any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "initialize", reflect.TypeOf((*MockBlock)(nil).initialize), bytes)
}
