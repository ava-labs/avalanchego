// Code generated by MockGen. DO NOT EDIT.
// Source: utils/iterator/iterator.go
//
// Generated by this command:
//
//	mockgen -source=utils/iterator/iterator.go -destination=utils/iterator/mocks/iterator.go -package=mocks -exclude_interfaces=
//

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	ids "github.com/ava-labs/avalanchego/ids"
	gomock "go.uber.org/mock/gomock"
)

// MockIterator is a mock of Iterator interface.
type MockIterator[T any] struct {
	ctrl     *gomock.Controller
	recorder *MockIteratorMockRecorder[T]
}

// MockIteratorMockRecorder is the mock recorder for MockIterator.
type MockIteratorMockRecorder[T any] struct {
	mock *MockIterator[T]
}

// NewMockIterator creates a new mock instance.
func NewMockIterator[T any](ctrl *gomock.Controller) *MockIterator[T] {
	mock := &MockIterator[T]{ctrl: ctrl}
	mock.recorder = &MockIteratorMockRecorder[T]{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockIterator[T]) EXPECT() *MockIteratorMockRecorder[T] {
	return m.recorder
}

// Next mocks base method.
func (m *MockIterator[T]) Next() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Next")
	ret0, _ := ret[0].(bool)
	return ret0
}

// Next indicates an expected call of Next.
func (mr *MockIteratorMockRecorder[T]) Next() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Next", reflect.TypeOf((*MockIterator[T])(nil).Next))
}

// Release mocks base method.
func (m *MockIterator[T]) Release() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Release")
}

// Release indicates an expected call of Release.
func (mr *MockIteratorMockRecorder[T]) Release() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Release", reflect.TypeOf((*MockIterator[T])(nil).Release))
}

// Value mocks base method.
func (m *MockIterator[T]) Value() T {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Value")
	ret0, _ := ret[0].(T)
	return ret0
}

// Value indicates an expected call of Value.
func (mr *MockIteratorMockRecorder[T]) Value() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Value", reflect.TypeOf((*MockIterator[T])(nil).Value))
}

// MockIdentifiable is a mock of Identifiable interface.
type MockIdentifiable struct {
	ctrl     *gomock.Controller
	recorder *MockIdentifiableMockRecorder
}

// MockIdentifiableMockRecorder is the mock recorder for MockIdentifiable.
type MockIdentifiableMockRecorder struct {
	mock *MockIdentifiable
}

// NewMockIdentifiable creates a new mock instance.
func NewMockIdentifiable(ctrl *gomock.Controller) *MockIdentifiable {
	mock := &MockIdentifiable{ctrl: ctrl}
	mock.recorder = &MockIdentifiableMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockIdentifiable) EXPECT() *MockIdentifiableMockRecorder {
	return m.recorder
}

// ID mocks base method.
func (m *MockIdentifiable) ID() ids.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(ids.ID)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MockIdentifiableMockRecorder) ID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*MockIdentifiable)(nil).ID))
}
