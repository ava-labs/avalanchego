// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package proposervm

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

const stateSyncVersion = 0

var (
	stateSyncCodec           codec.Manager
	errWrongStateSyncVersion = errors.New("wrong state sync key version")
)

func init() {
	lc := linearcodec.New(reflectcodec.DefaultTagName, math.MaxUint32)
	stateSyncCodec = codec.NewManager(math.MaxInt32)

	err := stateSyncCodec.RegisterCodec(stateSyncVersion, lc)
	if err != nil {
		panic(err)
	}
}

type ProVMSummaryKey struct {
	ProBlkID ids.ID            `serialize:"true"`
	InnerKey InnerVMSummaryKey `serialize:"true"`
}

type InnerVMSummaryKey struct {
	InnerBlkID   ids.ID `serialize:"true"`
	InnerVMBytes []byte `serialize:"true"`
}

func (vm *VM) StateSyncEnabled() (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncEnabled()
}

func (vm *VM) StateSyncGetLastSummary() (block.Summary, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.Summary{}, block.ErrStateSyncableVMNotImplemented
	}

	vmSummary, err := fsVM.StateSyncGetLastSummary()
	if err != nil {
		return block.Summary{}, err
	}

	// Extract innerBlkID from summary key
	innerKey := InnerVMSummaryKey{}
	parsedVersion, err := stateSyncCodec.Unmarshal(vmSummary.Key, &innerKey)
	if err != nil {
		return block.Summary{}, err
	}
	if parsedVersion != stateSyncVersion {
		return block.Summary{}, errWrongStateSyncVersion
	}

	// retrieve proposer Block wrapping innerBlock
	proBlkID, err := vm.GetBlockID(innerKey.InnerBlkID)
	if err != nil {
		return block.Summary{}, err
	}

	// recreate key
	proKey := ProVMSummaryKey{
		ProBlkID: proBlkID,
		InnerKey: innerKey,
	}
	proKeyBytes, err := stateSyncCodec.Marshal(stateSyncVersion, &proKey)
	if err != nil {
		return block.Summary{}, err
	}

	return block.Summary{
		Key:   proKeyBytes,
		State: vmSummary.State,
	}, err
}

func (vm *VM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	// Extract innerKey from summary key
	proKey := ProVMSummaryKey{}
	parsedVersion, err := stateSyncCodec.Unmarshal(key, &proKey)
	if err != nil {
		return false, err
	}
	if parsedVersion != stateSyncVersion {
		return false, errWrongStateSyncVersion
	}

	innerKey, err := stateSyncCodec.Marshal(stateSyncVersion, proKey.InnerKey)
	if err != nil {
		return false, err
	}

	// propagate request to innerVm
	return fsVM.StateSyncIsSummaryAccepted(innerKey)
}

func (vm *VM) StateSync(accepted []block.Summary) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}

	// retrieve innerKey for each summary and propagate all to innerVM
	innerSummaries := make([]block.Summary, 0, len(accepted))
	for _, summ := range accepted {
		proKey := ProVMSummaryKey{}
		parsedVersion, err := stateSyncCodec.Unmarshal(summ.Key, &proKey)
		if err != nil {
			return err
		}
		if parsedVersion != stateSyncVersion {
			return errWrongStateSyncVersion
		}

		innerKey, err := stateSyncCodec.Marshal(stateSyncVersion, proKey.InnerKey)
		if err != nil {
			return err
		}

		innerSummaries = append(innerSummaries, block.Summary{
			Key:   innerKey,
			State: summ.State,
		})
	}

	return fsVM.StateSync(innerSummaries)
}

func (vm *VM) StateSyncLastAccepted() (ids.ID, uint64, error) {
	lastAccepted, err := vm.State.GetLastAccepted()
	if err != nil {
		return ids.Empty, 0, err
	}
	block, err := vm.getBlock(lastAccepted)
	if err != nil {
		return ids.Empty, 0, err
	}
	return lastAccepted, block.Height(), nil
}
