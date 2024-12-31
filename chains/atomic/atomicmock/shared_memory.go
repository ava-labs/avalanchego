// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/chains/atomic (interfaces: SharedMemory)
//
// Generated by this command:
//
//	mockgen -package=atomicmock -destination=atomicmock/shared_memory.go -mock_names=SharedMemory=SharedMemory . SharedMemory
//

// Package atomicmock is a generated GoMock package.
package atomicmock

import (
	reflect "reflect"

	atomic "github.com/ava-labs/avalanchego/chains/atomic"
	database "github.com/ava-labs/avalanchego/database"
	ids "github.com/ava-labs/avalanchego/ids"
	gomock "go.uber.org/mock/gomock"
)

// SharedMemory is a mock of SharedMemory interface.
type SharedMemory struct {
	ctrl     *gomock.Controller
	recorder *SharedMemoryMockRecorder
}

// SharedMemoryMockRecorder is the mock recorder for SharedMemory.
type SharedMemoryMockRecorder struct {
	mock *SharedMemory
}

// NewSharedMemory creates a new mock instance.
func NewSharedMemory(ctrl *gomock.Controller) *SharedMemory {
	mock := &SharedMemory{ctrl: ctrl}
	mock.recorder = &SharedMemoryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *SharedMemory) EXPECT() *SharedMemoryMockRecorder {
	return m.recorder
}

// Apply mocks base method.
func (m *SharedMemory) Apply(arg0 map[ids.ID]*atomic.Requests, arg1 ...database.Batch) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0}
	for _, a := range arg1 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "Apply", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// Apply indicates an expected call of Apply.
func (mr *SharedMemoryMockRecorder) Apply(arg0 any, arg1 ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0}, arg1...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Apply", reflect.TypeOf((*SharedMemory)(nil).Apply), varargs...)
}

// Get mocks base method.
func (m *SharedMemory) Get(arg0 ids.ID, arg1 [][]byte) ([][]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1)
	ret0, _ := ret[0].([][]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *SharedMemoryMockRecorder) Get(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*SharedMemory)(nil).Get), arg0, arg1)
}

// Indexed mocks base method.
func (m *SharedMemory) Indexed(arg0 ids.ID, arg1 [][]byte, arg2, arg3 []byte, arg4 int) ([][]byte, []byte, []byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Indexed", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].([][]byte)
	ret1, _ := ret[1].([]byte)
	ret2, _ := ret[2].([]byte)
	ret3, _ := ret[3].(error)
	return ret0, ret1, ret2, ret3
}

// Indexed indicates an expected call of Indexed.
func (mr *SharedMemoryMockRecorder) Indexed(arg0, arg1, arg2, arg3, arg4 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Indexed", reflect.TypeOf((*SharedMemory)(nil).Indexed), arg0, arg1, arg2, arg3, arg4)
}
