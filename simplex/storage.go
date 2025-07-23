package simplex

import (
	"context"
	"encoding/binary"

	"sync/atomic"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ simplex.Storage = (*Storage)(nil)
var simplexPrefix = []byte("simplex")
var BlockHeaderLen = 89 // TODO: import from simplex package(not currently exported)

type Storage struct {
	// height represents the number of blocks indexed in storage.
	height atomic.Uint64

	// db is the underlying database used to store finalizations.
	db database.Database

	// genesisBlock is the genesis block data. It is stored as the first block in the storage.
	genesisBlock *Block

	// deserializer is used to deserialize quorum certificates from bytes.
	deserializer *QCDeserializer

	// blockTracker is used to manage blocks that have been indexed.
	blockTracker *blockTracker
	
	vm block.ChainVM
}

// newStorage creates a new Storage instance.
// It creates a new prefixed database to store finalizations according to their sequence numbers.
// The VM is assumed to be initialized before calling this function.
func newStorage(ctx context.Context, config *Config, qcDeserializer *QCDeserializer, blockTracker *blockTracker) (*Storage, error) {
	lastAccepted, err := config.VM.LastAccepted(ctx)
	if err != nil {
		return nil, err
	}

	lastAcceptedBlock, err := config.VM.GetBlock(ctx, lastAccepted)
	if err != nil {
		return nil, err
	}

	genesisBlock, err := getGenesisBlock(ctx, config, lastAcceptedBlock)
	if err != nil {
		return nil, err
	}

	s := &Storage{
		db:           prefixdb.New(simplexPrefix, config.DB,),
		genesisBlock: genesisBlock,
		vm:           config.VM,
		deserializer: qcDeserializer,
		blockTracker: blockTracker,
	}
	s.height.Store(lastAcceptedBlock.Height() + 1)

	return s, nil
}

func (s *Storage) Height() uint64 {
	return s.height.Load()
}

// Retrieve returns the block and finalization at [seq].
// If [seq] is not found, returns false.
func (s *Storage) Retrieve(seq uint64) (simplex.VerifiedBlock, simplex.Finalization, bool) {
	if seq == 0 {
		return s.genesisBlock, simplex.Finalization{}, true
	}

	block, err := getBlockAtHeight(context.Background(), s.vm, seq)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, simplex.Finalization{}, false
		}
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
	if currentHeight != block.BlockHeader().ProtocolMetadata.Seq {
		panic("current height does not match block sequence number")
	}
	
	s.height.Add(1)

	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, currentHeight)

	finalizationBytes := finalizationToBytes(finalization)
	if err := s.db.Put(seqBuff, finalizationBytes); err != nil {
		panic(err)
	}

	s.blockTracker.indexBlock(context.Background(), block.BlockHeader().Digest)
}

// getGenesisBlock returns the genesis block wrapped as a Block instance.
func getGenesisBlock(ctx context.Context, config *Config, lastAcceptedBlock snowman.Block) (*Block, error) {
	genesis := &Block{
		metadata: simplex.ProtocolMetadata{
			Version: 0,
			Epoch:   0,
			Seq:     0,
			Round:   0,
		},
	}

	// set the genesis's vmBlock
	if lastAcceptedBlock.Height() == 0 {
		genesis.vmBlock = lastAcceptedBlock
	} else {
		snowmanGenesis, err := getBlockAtHeight(ctx, config.VM, 0)
		if err != nil {
			return nil, err
		}
		genesis.vmBlock = snowmanGenesis
	}

	// set the digest
	bytes, err := genesis.Bytes()
	if err != nil {
		return nil, err
	}
	genesis.digest = computeDigest(bytes)

	return genesis, nil
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

	finalization, err := finalizationFromBytes(finalizationBytes, s.deserializer)
	if err != nil {
		return simplex.Finalization{}, false, err
	}

	return finalization, true, nil
}

func getBlockAtHeight(ctx context.Context, vm block.ChainVM, height uint64) (snowman.Block, error) {
	id, err := vm.GetBlockIDAtHeight(context.Background(), height)
	if err != nil {
		return nil, err
	}

	block, err := vm.GetBlock(context.Background(), id)
	if err != nil {
		return nil, err
	}

	return block, nil
}

// finalizationToBytes serializes the simplex.Finalization into bytes.
func finalizationToBytes(finalization simplex.Finalization) []byte {
	blockHeaderBytes := finalization.Finalization.Bytes()
	qcBytes := finalization.QC.Bytes()
	buff := make([]byte, len(blockHeaderBytes)+len(qcBytes))
	copy(buff, blockHeaderBytes)
	copy(buff[len(blockHeaderBytes):], qcBytes)
	return buff
}

// finalizationFromBytes deserialized the bytes into a simplex.Finalization.
func finalizationFromBytes(bytes []byte, d *QCDeserializer) (simplex.Finalization, error) {
	var finalization simplex.Finalization
	if err := finalization.Finalization.FromBytes(bytes[:BlockHeaderLen]); err != nil {
		return simplex.Finalization{}, err
	}

	qc, err := d.DeserializeQuorumCertificate(bytes[BlockHeaderLen:])
	if err != nil {
		return simplex.Finalization{}, err
	}

	finalization.QC = qc
	return finalization, nil
}
