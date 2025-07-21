package simplex

import (
	"context"
	"encoding/binary"

	"sync/atomic"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ simplex.Storage = (*Storage)(nil)
var BlockHeaderLen = 89 // TODO: import from simplex package(not currently exported)

type Storage struct {
	// height represents the number of blocks indexed in storage.
	height atomic.Uint64

	// db is the underlying database used to store blocks and finalizations.
	db database.Database

	// genesisBlock is the genesis block data. It is stored as the first block in the storage.
	genesisBlock *Block

	// deserializer is used to deserialize quorum certificates from bytes.
	deserializer QCDeserializer

	vm block.ChainVM

	blockTracker *blockTracker
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

	genesisBlock, err := getGenesisBlock(ctx, config, lastAcceptedBlock)
	if err != nil {
		return nil, err
	}

	s := &Storage{
		db:           config.DB,
		genesisBlock: genesisBlock,
		vm:           config.VM,
		deserializer: QCDeserializer(verifier),
	}
	s.height.Store(lastAcceptedBlock.Height() + 1)

	return s, nil
}

func (s *Storage) Height() uint64 {
	return s.height.Load()
}

func getGenesisBlock(ctx context.Context, config *Config, lastAcceptedBlock snowman.Block) (*Block, error) {
	genesis := &Block{
		metadata: simplex.ProtocolMetadata{
			Version: 0, // todo: add to constants
			Seq:     0,
			Round:   0,
			Epoch:   0,
		},
	}

	if lastAcceptedBlock.Height() == 0 {
		genesis.vmBlock = lastAcceptedBlock
	} else {
		snowmanGenesis, err := config.VM.ParseBlock(ctx, config.GenesisBytes)
		if err != nil {
			return nil, err
		}
		genesis.vmBlock = snowmanGenesis
	}

	bytes, err := genesis.Bytes()
	if err != nil {
		return nil, err
	}
	genesis.digest = computeDigest(bytes)

	return genesis, nil
}

// Retrieve returns the block and finalization at [seq].
// If [seq] is not found, returns false.
func (s *Storage) Retrieve(seq uint64) (simplex.VerifiedBlock, simplex.Finalization, bool) {
	if seq == 0 {
		return s.genesisBlock, simplex.Finalization{}, true
	}

	id, err := s.vm.GetBlockIDAtHeight(context.Background(), seq)
	if err != nil {
		return nil, simplex.Finalization{}, false
	}

	block, err := s.vm.GetBlock(context.Background(), id)
	if err != nil {
		return nil, simplex.Finalization{}, false
	}

	finalization, found, err := s.retrieveFinalization(seq)
	if !found || err != nil {
		return nil, simplex.Finalization{}, false
	}

	vb := &Block{vmBlock: block, metadata: finalization.Finalization.ProtocolMetadata}

	return vb, finalization, true
}

// Index indexes the finalization in the storage.
// It stores the finalization bytes at the current height and increments the height.
func (s *Storage) Index(block simplex.VerifiedBlock, finalization simplex.Finalization) {
	if block.BlockHeader().ProtocolMetadata.Seq == 0 {
		// This should never happen
		return
	}

	currentHeight := s.height.Load()
	s.height.Add(1)

	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, currentHeight)

	finalizationBytes := finalizationToBytes(finalization)
	if err := s.db.Put(seqBuff, finalizationBytes); err != nil {
		panic(err)
	}

	s.blockTracker.indexBlock(context.Background(), block.BlockHeader().Digest)
}

// retrieveFinalization retrieves the finalization at [seq].
// If the finalization is not found, it returns false.
func (s *Storage) retrieveFinalization(seq uint64) (simplex.Finalization, bool, error) {
	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, seq)
	finalizationBytes, err := s.db.Get(seqBuff)
	if err != nil {
		if err == database.ErrNotFound {
			return simplex.Finalization{}, false, nil
		}
		return simplex.Finalization{}, false, err
	}

	fCert, err := finalizationFromBytes(finalizationBytes, s.deserializer)
	if err != nil {
		return simplex.Finalization{}, false, err
	}

	return fCert, true, nil
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
	if err := fCert.Finalization.FromBytes(bytes[:BlockHeaderLen]); err != nil {
		return simplex.Finalization{}, err
	}

	qc, err := d.DeserializeQuorumCertificate(bytes[BlockHeaderLen:])
	if err != nil {
		return simplex.Finalization{}, err
	}

	fCert.QC = qc
	return fCert, nil
}
