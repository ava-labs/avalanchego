package proposervm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	blockPrefix = []byte("block")
	wrpdToProID = []byte("wrpdToProID")
)

type innerState struct {
	vm *VM

	baseDB *versiondb.Database

	knownProBlocks map[ids.ID]*ProposerBlock
	proBlkDB       *prefixdb.Database

	wrpdToProID   map[ids.ID]ids.ID
	wrpdToProIDDB *prefixdb.Database
}

func newState(vm *VM) *innerState {
	res := innerState{
		vm:             vm,
		baseDB:         nil,
		knownProBlocks: make(map[ids.ID]*ProposerBlock),
		proBlkDB:       nil,
		wrpdToProID:    make(map[ids.ID]ids.ID),
		wrpdToProIDDB:  nil,
	}
	return &res
}

func (is *innerState) init(db database.Database) {
	is.baseDB = versiondb.New(db)
	is.proBlkDB = prefixdb.New(blockPrefix, db)
	is.wrpdToProIDDB = prefixdb.New(wrpdToProID, db)
}

func (is *innerState) cacheProBlk(blk *ProposerBlock) {
	is.knownProBlocks[blk.ID()] = blk
	is.wrpdToProID[blk.coreBlk.ID()] = blk.ID()
}

func (is *innerState) wipeFromCacheProBlk(id ids.ID) {
	if blk, ok := is.knownProBlocks[id]; ok {
		delete(is.wrpdToProID, blk.coreBlk.ID())
		delete(is.knownProBlocks, id)
	}
}

func (is *innerState) commitBlk(blk *ProposerBlock) error {
	defer is.baseDB.Abort()
	if err := is.proBlkDB.Put(blk.id[:], blk.bytes); err != nil {
		is.wipeFromCacheProBlk(blk.ID())
		return err
	}

	wrpdID := blk.coreBlk.ID()
	proID, err := idToBytes(is.wrpdToProID[wrpdID])
	if err != nil {
		is.wipeFromCacheProBlk(blk.ID())
		return err
	}
	if err := is.wrpdToProIDDB.Put(wrpdID[:], proID); err != nil {
		is.wipeFromCacheProBlk(blk.ID())
		return err
	}

	batch, err := is.baseDB.CommitBatch()
	if err != nil {
		is.wipeFromCacheProBlk(blk.ID())
		return err
	}

	return batch.Write()
}

func (is *innerState) getProBlock(id ids.ID) (*ProposerBlock, error) {
	if proBlk, ok := is.knownProBlocks[id]; ok {
		return proBlk, nil
	}

	proBytes, err := is.proBlkDB.Get(id[:])
	if err != nil {
		return nil, ErrProBlkNotFound
	}

	var mPb marshallingProposerBLock
	if err := mPb.unmarshal(proBytes); err != nil {
		return nil, fmt.Errorf("couldn't unmarshal proposerBlockHeader: %s", err)
	}

	sb, err := is.vm.ChainVM.ParseBlock(mPb.wrpdBytes)
	if err != nil {
		return nil, err
	}

	proBlk, _ := NewProBlock(is.vm, mPb.ProposerBlockHeader, sb, proBytes, false) // not signing block, cannot err
	is.cacheProBlk(&proBlk)

	return &proBlk, nil
}

func (is *innerState) getBlockFromWrappedBlkID(wrappedID ids.ID) (*ProposerBlock, error) {
	if proID, ok := is.wrpdToProID[wrappedID]; ok {
		return is.knownProBlocks[proID], nil
	}

	proIDBytes, err := is.wrpdToProIDDB.Get(wrappedID[:])
	if err != nil {
		return nil, ErrProBlkNotFound
	}

	proID, err := bytesToID(proIDBytes)
	if err != nil {
		return nil, err
	}

	return is.getProBlock(proID)
}

func (is *innerState) wipeCache() { // useful for UTs
	is.knownProBlocks = make(map[ids.ID]*ProposerBlock)
	is.wrpdToProID = make(map[ids.ID]ids.ID)
}

func idToBytes(id ids.ID) ([]byte, error) {
	p := wrappers.Packer{Bytes: make([]byte, hashing.HashLen+4)}
	if p.PackBytes(id[:]); p.Errored() {
		return nil, fmt.Errorf("could not marshal block id")
	}
	return p.Bytes, nil
}

func bytesToID(b []byte) (id ids.ID, err error) {
	res := ids.ID{}
	p := wrappers.Packer{
		Bytes: b,
	}

	IDBytes := p.UnpackBytes()
	switch {
	case p.Errored():
		return res, fmt.Errorf("could not unmarshal block id")
	case len(IDBytes) != len(res):
		return res, fmt.Errorf("could not unmarshal block id")
	default:
		copy(res[:], IDBytes)
	}

	return res, nil
}
