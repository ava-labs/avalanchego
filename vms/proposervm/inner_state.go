package proposervm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

type innerState struct {
	vm             *VM
	proBlkDB       *versiondb.Database
	knownProBlocks map[ids.ID]*ProposerBlock
	wrpdToProID    map[ids.ID]ids.ID
}

func newState(vm *VM) *innerState {
	res := innerState{
		vm:             vm,
		proBlkDB:       nil,
		knownProBlocks: make(map[ids.ID]*ProposerBlock),
		wrpdToProID:    make(map[ids.ID]ids.ID),
	}
	return &res
}

func (is *innerState) init(db database.Database) {
	is.proBlkDB = versiondb.New(db)
}

func (is *innerState) cacheProBlk(blk *ProposerBlock) {
	// TODO: handle update/create
	is.knownProBlocks[blk.ID()] = blk
	is.wrpdToProID[blk.Block.ID()] = blk.ID()
}

func (is *innerState) storeBlk(blk *ProposerBlock) error {
	err := is.proBlkDB.Put(blk.id[:], blk.bytes)
	return err
}

func (is *innerState) getBlock(id ids.ID) (*ProposerBlock, error) {
	if proBlk, ok := is.knownProBlocks[id]; ok {
		return proBlk, nil
	}

	proBytes, err := is.proBlkDB.Get(id[:])
	if err != nil {
		return nil, ErrProBlkNotFound
	}

	var mPb marshallingProposerBLock
	if _, err := cdc.Unmarshal(proBytes, &mPb); err != nil {
		return nil, fmt.Errorf("couldn't unmarshal proposerBlockHeader: %s", err)
	}

	sb, err := is.vm.ChainVM.ParseBlock(mPb.WrpdBytes)
	if err != nil {
		return nil, err
	}

	proBlk := NewProBlock(is.vm, mPb.Header, sb, proBytes)
	is.cacheProBlk(&proBlk)

	return &proBlk, nil
}

func (is *innerState) getBlockFromWrappedBlkID(wrappedID ids.ID) (*ProposerBlock, error) {
	proID, ok := is.wrpdToProID[wrappedID]
	if !ok {
		return nil, ErrProBlkNotFound
	}

	return is.knownProBlocks[proID], nil
}

func (is *innerState) wipeCache() {
	is.knownProBlocks = make(map[ids.ID]*ProposerBlock)
	is.wrpdToProID = make(map[ids.ID]ids.ID)
}
