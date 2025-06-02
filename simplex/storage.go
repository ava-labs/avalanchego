package simplex

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"simplex"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
)

var _ simplex.Storage = (*Storage)(nil)

type Storage struct {
	height      atomic.Uint64
	db          *versiondb.Database
	vm          block.ChainVM
	d           QCDeserializer
	genesisData []byte
}


func createStorage(config *Config, verifier BLSVerifier) (*Storage, error) {
	storage, err := NewStorage(config.DB, QCDeserializer(verifier), config.VM, config.GenesisData)
	if err != nil {
		return nil, err
	}

	return storage, nil
}

func (s *Storage) Height() uint64 {
	return s.height.Load()
}

// Retrieve returns the block and finalization certificate at [seq].
// If [seq] is not found, returns false.
func (s *Storage) Retrieve(seq uint64) (simplex.VerifiedBlock, simplex.FinalizationCertificate, bool) {
	if seq == 0 {
		return &VerifiedBlock{innerBlock: s.genesisData, metadata: simplex.ProtocolMetadata{}}, simplex.FinalizationCertificate{}, true
	}

	id, err := s.vm.GetBlockIDAtHeight(context.Background(), seq+1)
	if err != nil {
		return nil, simplex.FinalizationCertificate{}, false
	}

	block, err := s.vm.GetBlock(context.Background(), id)
	if err != nil {
		return nil, simplex.FinalizationCertificate{}, false
	}

	if seq == 0 {
		return &VerifiedBlock{innerBlock: block.Bytes()}, simplex.FinalizationCertificate{}, true
	}

	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, seq)
	fCertBytes, err := s.db.Get(seqBuff)
	if err != nil {
		return nil, simplex.FinalizationCertificate{}, false
	}

	fCert, err := fCertFromBytes(fCertBytes, s.d)
	if err != nil {
		panic(err)
	}

	vb := &VerifiedBlock{innerBlock: block.Bytes(), metadata: fCert.Finalization.ProtocolMetadata}

	return vb, fCert, true
}
func (s *Storage) Index(block simplex.VerifiedBlock, fCert simplex.FinalizationCertificate) {
	currentHeight := s.height.Load()
	newHeight := currentHeight + 1
	s.height.Store(newHeight)

	heightBuff, seqBuff := encodeHeightAndSeq(currentHeight)

	if err := s.db.Put([]byte("height"), heightBuff); err != nil {
		panic(err)
	}

	var fCertBuff []byte

	if currentHeight > 0 {
		fCertBuff = fCertToBytes(fCert)
	}

	if err := s.db.Put(seqBuff, fCertBuff); err != nil {
		panic(err)
	}

	if err := block.(*VerifiedBlock).accept(context.Background()); err != nil {
		panic(err)
	}

	if s.onIndex != nil {
		s.onIndex(fCert.Finalization.Seq, fCert.Finalization.Round)
	}
}

func encodeHeightAndSeq(seq uint64) ([]byte, []byte) {
	heightBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBuff, seq+1)
	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, seq)
	return heightBuff, seqBuff
}

func NewStorage(db database.Database, d QCDeserializer, vm block.ChainVM, genesisData []byte) (*Storage, error) {
	var s Storage

	vdb := versiondb.New(prefixdb.New([]byte("simplex"), db))

	height, err := initHeight(vdb)
	if err != nil {
		return nil, err
	}

	s.height.Store(height)
	s.db = vdb
	s.d = d
	s.vm = vm
	s.genesisData = genesisData

	return &s, nil
}

func initHeight(vdb *versiondb.Database) (uint64, error) {
	var height uint64
	heightBytes, err := vdb.Get([]byte("height"))
	if !errors.Is(err, database.ErrNotFound) {
		return 0, fmt.Errorf("failed retrieving height from DB: %w", err)
	}
	if err == nil {
		height = binary.BigEndian.Uint64(heightBytes)
	} else {
		buff := make([]byte, 8)
		binary.BigEndian.PutUint64(buff, 1)
		err = vdb.Put([]byte("height"), buff)
		height = 1
	}
	return height, err
}

func fCertToBytes(fCert simplex.FinalizationCertificate) []byte {
	blockHeaderBytes := fCert.Finalization.Bytes()
	qcBytes := fCert.QC.Bytes()
	buff := make([]byte, len(blockHeaderBytes)+len(qcBytes))
	copy(buff, blockHeaderBytes)
	copy(buff[len(blockHeaderBytes):], qcBytes)
	return buff
}

func fCertFromBytes(bytes []byte, d QCDeserializer) (simplex.FinalizationCertificate, error) {
	var fCert simplex.FinalizationCertificate
	if err := fCert.Finalization.FromBytes(bytes[:89]); err != nil {
		return simplex.FinalizationCertificate{}, err
	}

	qc, err := d.DeserializeQuorumCertificate(bytes[89:])
	if err != nil {
		return simplex.FinalizationCertificate{}, err
	}

	fCert.QC = qc
	return fCert, nil
}
