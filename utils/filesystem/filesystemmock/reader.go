// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/utils/filesystem (interfaces: Reader)
//
// Generated by this command:
//
//	mockgen -package=filesystemmock -destination=filesystemmock/reader.go -mock_names=Reader=Reader . Reader
//

// Package filesystemmock is a generated GoMock package.
package filesystemmock

import (
	fs "io/fs"
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
)

// Reader is a mock of Reader interface.
type Reader struct {
	ctrl     *gomock.Controller
	recorder *ReaderMockRecorder
}

// ReaderMockRecorder is the mock recorder for Reader.
type ReaderMockRecorder struct {
	mock *Reader
}

// NewReader creates a new mock instance.
func NewReader(ctrl *gomock.Controller) *Reader {
	mock := &Reader{ctrl: ctrl}
	mock.recorder = &ReaderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Reader) EXPECT() *ReaderMockRecorder {
	return m.recorder
}

// ReadDir mocks base method.
func (m *Reader) ReadDir(arg0 string) ([]fs.DirEntry, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadDir", arg0)
	ret0, _ := ret[0].([]fs.DirEntry)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ReadDir indicates an expected call of ReadDir.
func (mr *ReaderMockRecorder) ReadDir(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadDir", reflect.TypeOf((*Reader)(nil).ReadDir), arg0)
}
