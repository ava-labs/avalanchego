// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/snow/engine/snowman/block (interfaces: BuildBlockWithContextChainVM)
//
// Generated by this command:
//
//	mockgen -package=blockmock -destination=blockmock/build_block_with_context_chain_vm.go -mock_names=BuildBlockWithContextChainVM=BuildBlockWithContextChainVM . BuildBlockWithContextChainVM
//

// Package blockmock is a generated GoMock package.
package blockmock

import (
	context "context"
	reflect "reflect"

	snowman "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	block "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	gomock "go.uber.org/mock/gomock"
)

// BuildBlockWithContextChainVM is a mock of BuildBlockWithContextChainVM interface.
type BuildBlockWithContextChainVM struct {
	ctrl     *gomock.Controller
	recorder *BuildBlockWithContextChainVMMockRecorder
	isgomock struct{}
}

// BuildBlockWithContextChainVMMockRecorder is the mock recorder for BuildBlockWithContextChainVM.
type BuildBlockWithContextChainVMMockRecorder struct {
	mock *BuildBlockWithContextChainVM
}

// NewBuildBlockWithContextChainVM creates a new mock instance.
func NewBuildBlockWithContextChainVM(ctrl *gomock.Controller) *BuildBlockWithContextChainVM {
	mock := &BuildBlockWithContextChainVM{ctrl: ctrl}
	mock.recorder = &BuildBlockWithContextChainVMMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *BuildBlockWithContextChainVM) EXPECT() *BuildBlockWithContextChainVMMockRecorder {
	return m.recorder
}

// BuildBlockWithContext mocks base method.
func (m *BuildBlockWithContextChainVM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BuildBlockWithContext", ctx, blockCtx)
	ret0, _ := ret[0].(snowman.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BuildBlockWithContext indicates an expected call of BuildBlockWithContext.
func (mr *BuildBlockWithContextChainVMMockRecorder) BuildBlockWithContext(ctx, blockCtx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BuildBlockWithContext", reflect.TypeOf((*BuildBlockWithContextChainVM)(nil).BuildBlockWithContext), ctx, blockCtx)
}
