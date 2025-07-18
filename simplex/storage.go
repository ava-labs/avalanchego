package simplex

import (
	"context"
	"encoding/binary"

	"sync/atomic"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ simplex.Storage = (*Storage)(nil)
var heightKey = []byte("height")

type Storage struct {
	// height represents the number of blocks indexed in storage.
	height atomic.Uint64

	// db is the underlying database used to store blocks and finalizations.
	db *versiondb.Database

	// genesisData is the genesis block data. It is stored as the first block in the storage.
	genesisData []byte

	// deserializer is used to deserialize quorum certificates from bytes.
	deserializer QCDeserializer

	vm block.ChainVM
}

// newStorage creates a new Storage instance.
// it writes the genesis block to the database at height 0, seq 0.
// The DB should already be prefixed. 
// Assumes the VM is already initialized and the genesis block is set.
func newStorage(ctx context.Context, config *Config, verifier BLSVerifier) (*Storage, error) {
	lastAccepted, err := config.VM.LastAccepted(ctx)
	lastAcceptedBlock, err := config.VM.GetBlock(ctx, lastAccepted)
	if err != nil {
		return nil, err
	}

	s := &Storage{
		db:           db,
		genesisData:  config.GenesisData,
		vm:           config.VM,
		deserializer: QCDeserializer(verifier),
	}
	s.height.Store(lastAcceptedBlock.Height())


	s.Index(s.getGenesisBlock(), simplex.Finalization{})
	return s, nil
}

func (s *Storage) getGenesisBlock() simplex.VerifiedBlock {
	return &VerifiedBlock{innerBlock: s.genesisData, metadata: simplex.ProtocolMetadata{}}
}

func (s *Storage) Height() uint64 {
	return s.height.Load()
}

// Retrieve returns the block and finalization at [seq].
// If [seq] is not found, returns false.
func (s *Storage) Retrieve(seq uint64) (simplex.VerifiedBlock, simplex.Finalization, bool) {
	id, err := s.vm.GetBlockIDAtHeight(context.Background(), seq)
	if err != nil {
		return nil, simplex.Finalization{}, false
	}

	block, err := s.vm.GetBlock(context.Background(), id)
	if err != nil {
		return nil, simplex.Finalization{}, false
	}

	finalization, found := s.retrieveFinalization(seq)
	if !found {
		return nil, simplex.Finalization{}, false
	}

	vb := &VerifiedBlock{innerBlock: block.Bytes(), metadata: finalization.Finalization.ProtocolMetadata}

	return vb, finalization, true
}

// Index indexes the finalization in the storage.
// It stores the finalization bytes at the current height and increments the height.
// TODO: where do we actually store the block? also isn't it weird to pass in a VerifiedBlock here
// since we haven't verified it yet?
func (s *Storage) Index(block simplex.VerifiedBlock, finalization simplex.Finalization) {
	currentHeight := s.height.Load()
	s.height.Add(1)

	heightBuff, seqBuff := make([]byte, 8), make([]byte, 8)
	binary.BigEndian.PutUint64(heightBuff, currentHeight)
	binary.BigEndian.PutUint64(seqBuff, currentHeight)

	var finalizationBuf []byte

	// The genesis block does not have a finalization
	if currentHeight > 0 {
		finalizationBuf = finalizationToBytes(finalization)
	}

	// Store the current height in the database.
	if err := s.db.Put(heightKey, heightBuff); err != nil {
		panic(err)
	}

	if err := s.db.Put(seqBuff, finalizationBuf); err != nil {
		panic(err)
	}

	// TODO: can we call verify on the genesis block?
	if err := block.(*VerifiedBlock).accept(context.Background()); err != nil {
		panic(err)
	}
}

// retrieveFinalization retrieves the finalization at [seq].
// If the finalization is not found, it returns false.
func (s *Storage) retrieveFinalization(seq uint64) (simplex.Finalization, bool) {
	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, seq)
	finalizationBytes, err := s.db.Get(seqBuff)
	if err != nil {
		return simplex.Finalization{}, false
	}

	fCert, err := finalizationFromBytes(finalizationBytes, s.deserializer)
	if err != nil {
		panic(err)
	}

	return fCert, true
}

func finalizationToBytes(finalization simplex.Finalization) []byte {
	blockHeaderBytes := finalization.Finalization.Bytes()
	qcBytes := finalization.QC.Bytes()
	buff := make([]byte, len(blockHeaderBytes)+len(qcBytes))
	copy(buff, blockHeaderBytes)
	copy(buff[len(blockHeaderBytes):], qcBytes)
	return buff
}

func finalizationFromBytes(bytes []byte, d QCDeserializer) (simplex.Finalization, error) {
	var fCert simplex.Finalization
	if err := fCert.Finalization.FromBytes(bytes[:simplex.MetadataLen]); err != nil {
		return simplex.Finalization{}, err
	}

	qc, err := d.DeserializeQuorumCertificate(bytes[simplex.MetadataLen:])
	if err != nil {
		return simplex.Finalization{}, err
	}

	fCert.QC = qc
	return fCert, nil
}
