package proposervm

import (
	"bytes"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	stateBlkVersion           uint16 = 0
	ErrStateBlkFailedParsing         = errors.New("could not parse state proposer block")
	blockPrefix                      = []byte("block")
	notableIDPrefix                  = []byte("proGenID")
	proGenIDKey                      = []byte("proGenIDKey")
	ErrGenesisNotFound               = errors.New("proposer genesis block not found")
	preferredIDKey                   = []byte("preferredIDKey")
	ErrPreferredIDNotFound           = errors.New("preferred ID not found")
	lastAcceptedIDKey                = []byte("lastAcceptedIDKey")
	ErrLastAcceptedIDNotFound        = errors.New("last accepted ID not found")
)

type innerState struct {
	vm *VM

	baseDB *versiondb.Database

	knownProBlocks map[ids.ID]*ProposerBlock
	proBlkDB       *prefixdb.Database

	proGenID       ids.ID
	preferredID    ids.ID
	lastAcceptedID ids.ID
	notableIDsDB   *prefixdb.Database
}

func newState(vm *VM) *innerState {
	res := innerState{
		vm:           vm,
		baseDB:       nil,
		proBlkDB:     nil,
		notableIDsDB: nil,
	}
	res.wipeCache()
	return &res
}

func (is *innerState) init(db database.Database) {
	is.baseDB = versiondb.New(db)
	is.proBlkDB = prefixdb.New(blockPrefix, db)
	is.notableIDsDB = prefixdb.New(notableIDPrefix, db)
}

func (is *innerState) wipeCache() {
	is.knownProBlocks = make(map[ids.ID]*ProposerBlock)
	is.proGenID = ids.Empty
	is.preferredID = ids.Empty
	is.lastAcceptedID = ids.Empty
}

func (is *innerState) cacheProBlk(blk *ProposerBlock) {
	is.knownProBlocks[blk.ID()] = blk
}

func (is *innerState) storeProBlk(blk *ProposerBlock) error {
	is.cacheProBlk(blk)

	defer is.baseDB.Abort()

	stPrBlk := stateProBlk{
		version: stateBlkVersion,
		ProBlk:  blk.Bytes(),
		status:  blk.Status(),
	}
	bytes, err := stPrBlk.marshal()
	if err != nil {
		return err
	}
	id := blk.ID()
	if err := is.proBlkDB.Put(id[:], bytes); err != nil {
		is.wipeFromCacheProBlk(id)
		return err
	}

	batch, err := is.baseDB.CommitBatch()
	if err != nil {
		is.wipeFromCacheProBlk(blk.ID())
		return err
	}

	return batch.Write()
}

func (is *innerState) wipeFromCacheProBlk(id ids.ID) {
	delete(is.knownProBlocks, id)

	switch id {
	case is.proGenID:
		is.proGenID = ids.Empty
	case is.preferredID:
		is.preferredID = ids.Empty
	case is.lastAcceptedID:
		is.lastAcceptedID = ids.Empty
	}
}

func (is *innerState) getProBlock(id ids.ID) (*ProposerBlock, error) {
	if proBlk, ok := is.knownProBlocks[id]; ok {
		return proBlk, nil
	}

	stProBytes, err := is.proBlkDB.Get(id[:])
	if err != nil {
		return nil, ErrProBlkNotFound
	}

	proBlk, err := is.vm.parseProposerBlock(stProBytes)
	if err != nil {
		return nil, err
	}

	is.cacheProBlk(&proBlk)
	return &proBlk, nil
}

func (is *innerState) storeProGenID(id ids.ID) error {
	defer is.baseDB.Abort()
	currentGenID := is.proGenID

	if err := is.notableIDsDB.Put(proGenIDKey, id[:]); err != nil {
		is.proGenID = currentGenID
		return err
	}
	batch, err := is.baseDB.CommitBatch()
	if err != nil {
		is.proGenID = currentGenID
		return err
	}

	if err := batch.Write(); err != nil {
		is.proGenID = currentGenID
		return err
	}

	is.proGenID = id
	return nil
}

func (is *innerState) getProGenesisBlk() (*ProposerBlock, error) {
	if !bytes.Equal(is.proGenID[:], ids.Empty[:]) {
		return is.getProBlock(is.proGenID)
	}

	key := proGenIDKey
	proGenAvail, err := is.notableIDsDB.Has(key)
	if err != nil {
		return nil, err // could not query DB
	}
	if !proGenAvail {
		return nil, ErrGenesisNotFound
	}
	proGenBytes, err := is.notableIDsDB.Get(key)
	if err != nil {
		return nil, err
	}
	copy(is.proGenID[:], proGenBytes)
	return is.getProBlock(is.proGenID)
}
