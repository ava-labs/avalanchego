// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package metervm

import "github.com/ava-labs/avalanchego/snow/engine/snowman/block"

func (vm *blockVM) StateSyncEnabled() (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	/*start :=*/
	_ = vm.clock.Time()
	enabled, err := fsVM.StateSyncEnabled()
	/*end :=*/ _ = vm.clock.Time()

	// TODO ABENEGIA: introduce Summary metrics

	return enabled, err
}

func (vm *blockVM) StateSyncGetLastSummary() ([]byte, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	/*start :=*/
	_ = vm.clock.Time()
	stateSummaryFrontier, err := fsVM.StateSyncGetLastSummary()
	/*end :=*/ _ = vm.clock.Time()

	// TODO ABENEGIA: introduce Summary metrics

	return stateSummaryFrontier, err
}

func (vm *blockVM) StateSyncIsSummaryAccepted(summary []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	/*start :=*/
	_ = vm.clock.Time()
	accepted, err := fsVM.StateSyncIsSummaryAccepted(summary)
	/*end :=*/ _ = vm.clock.Time()

	// TODO ABENEGIA: introduce Summary metrics

	return accepted, err
}

func (vm *blockVM) StateSync(accepted [][]byte) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}

	/*start :=*/
	_ = vm.clock.Time()
	err := fsVM.StateSync(accepted)
	/*end :=*/ _ = vm.clock.Time()

	// TODO ABENEGIA: introduce Summary metrics

	return err
}
