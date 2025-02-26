// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"simplex"
	"simplex/wal"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"go.uber.org/zap"
)

func CreateSimplexInstance(config *Config) (*simplex.Epoch, *BlockBuilder, *WALInterceptor, error) {
	signer := BLSSigner{
		NetworkID: config.Ctx.SubnetID,
		ChainID:   config.Ctx.ChainID,
		NodeID:    config.Ctx.NodeID,
		SignBLS:   config.SignBLS,
	}

	verifier := createVerifier(config)

	comm := NewComm(config.Ctx.NodeID[:], config.Ctx.SubnetID, config.Ctx.ChainID, config.Ctx.Log, config.Validators, config.Sender, config.OutboundMsgBuilder)

	h := sha256.New()
	h.Write(config.Ctx.NodeID[:])
	h.Write(config.Ctx.ChainID[:])
	walDigest := h.Sum(nil)
	walFileName := fmt.Sprintf("%x.wal", walDigest[:10])

	var epochWAL simplex.WriteAheadLog
	var err error

	epochWAL, err = wal.New(walFileName)
	if err != nil {
		return nil, nil, nil, err
	}

	walInterceptor := &WALInterceptor{
		WriteAheadLog: epochWAL,
	}

	storage, err := createStorage(config, err, verifier)
	if err != nil {
		return nil, nil, nil, err
	}

	bb := &BlockBuilder{
		Logger: config.Ctx.Log,
		VM:     config.VM,
	}

	conf := simplex.EpochConfig{
		BlockBuilder:        bb,
		Signer:              &signer,
		ID:                  config.Ctx.NodeID[:],
		Verifier:            verifier,
		Logger:              config.Ctx.Log,
		QCDeserializer:      QCDeserializer(verifier),
		SignatureAggregator: SignatureAggregator(verifier),
		Comm:                comm,
		WAL:                 walInterceptor,
		Storage:             storage,
		StartTime:           time.Now(),
		MaxProposalWait:     time.Second * 2,
		BlockDeserializer: &blockDeserializer{
			vm: config.VM,
		},
	}

	simplex, err := simplex.NewEpoch(conf)
	if err != nil {
		return nil, nil, nil, err
	}

	return simplex, bb, walInterceptor, nil
}

func createStorage(config *Config, err error, verifier BLSVerifier) (*Storage, error) {
	storage, err := NewStorage(config.DB, QCDeserializer(verifier), config.VM, config.GenesisData)
	if err != nil {
		return nil, err
	}

	return storage, nil
}

func createVerifier(config *Config) BLSVerifier {
	verifier := BLSVerifier{
		nodeID2PK: make(map[ids.NodeID]bls.PublicKey),
		networkID: config.Ctx.SubnetID,
		chainID:   config.Ctx.ChainID,
	}

	nodes := config.Validators.GetValidatorIDs(config.Ctx.SubnetID)
	for _, node := range nodes {
		validator, ok := config.Validators.GetValidator(config.Ctx.SubnetID, node)
		if !ok {
			continue
		}

		verifier.nodeID2PK[node] = *validator.PublicKey
	}
	return verifier
}

type Engine struct {
	ctx *snow.ConsensusContext
	common.AllGetsServer
	e *simplex.Epoch
	block.ChainVM
	blockTracker blockTracker

	// simplex digest to VM digest cache
	lock        sync.RWMutex
	digestCache map[simplex.Digest]ids.ID
}

func CreateEngine(config *Config) (*Engine, error) {
	e, bb, wal, err := CreateSimplexInstance(config)
	if err != nil {
		return nil, err
	}

	engine := &Engine{
		digestCache:   make(map[simplex.Digest]ids.ID),
		ctx:           config.Ctx,
		e:             e,
		ChainVM:       config.VM,
		AllGetsServer: config.GetServer,
	}

	wal.Intercept = func(digest simplex.Digest) {
		engine.lock.Lock()
		defer engine.lock.Unlock()

		if id, ok := engine.digestCache[digest]; ok {
			config.VM.SetPreference(context.Background(), id)
		}
	}

	bb.e = engine

	engine.blockTracker.init()

	return engine, nil
}

func (e *Engine) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}

func (e *Engine) GetStateSummaryFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs set.Set[ids.ID]) error {
	return nil
}

func (e *Engine) GetAcceptedStateSummaryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (e *Engine) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	return nil
}

func (e *Engine) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
	return nil
}

func (e *Engine) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (e *Engine) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) error {
	return nil
}

func (e *Engine) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64) error {
	return nil
}

func (e *Engine) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) error {
	return nil
}

func (e *Engine) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return nil
}

func (e *Engine) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

func (e *Engine) Gossip(ctx context.Context) error {
	return nil
}

func (e *Engine) Shutdown(ctx context.Context) error {
	e.e.Stop()
	return nil
}

func (e *Engine) Notify(ctx context.Context, msg common.Message) error {
	return nil
}

func (e *Engine) Start(context.Context, uint32) error {
	e.ctx.Log.Info("Starting the engine")
	err := e.e.Start()

	if err == nil {
		e.startTicker()
	}

	return err
}

func (e *Engine) startTicker() {
	ticker := time.NewTicker(time.Millisecond * 500)
	go func() {
		for {
			tick := <-ticker.C
			e.e.AdvanceTime(tick)
		}
	}()
}

func (e *Engine) HealthCheck(context.Context) (interface{}, error) {
	return json.MarshalIndent(e.e.Metadata(), "\t", "\t")
}

func (e *Engine) SimplexMessage(from ids.NodeID, msg *p2p.SimplexMessage) error {
	simplexMessage := e.simplexMessageToSimplexMessage(msg)
	if simplexMessage == nil {
		e.ctx.Log.Warn("Failed converting p2p.SimplexMessage to simplex.Message")
		return nil
	}
	e.e.Logger.Info("Received simplex message", zap.Any("message", simplexMessage))
	err := e.e.HandleMessage(simplexMessage, from[:])
	if err != nil {
		e.ctx.Log.Error("Failed to handle simplex message", zap.Error(err))
	}
	return err
}

func (e *Engine) simplexMessageToSimplexMessage(message *p2p.SimplexMessage) *simplex.Message {
	switch {
	case message.GetBlock() != nil:
		return e.convertBlockMessage(message)
	case message.GetVote() != nil:
		return convertVoteMessage(message)
	case message.GetEmptyVote() != nil:
		return convertEmptyVoteMessage(message)
	case message.GetFinalization() != nil:
		return convertFinalizationMessage(message)
	case message.GetNotarization() != nil:
		return convertNotarizationMessage(message, e.e.EpochConfig.QCDeserializer)
	case message.GetEmptyNotarization() != nil:
		return convertEmptyNotarizationMessage(message, e.e.EpochConfig.QCDeserializer)
	case message.GetFinalizationCertificate() != nil:
		return convertFinalizationCertificateMessage(message, e.e.EpochConfig.QCDeserializer)
	default:
		e.ctx.Log.Warn("Unknown message type", zap.Any("message", message))
		return nil
	}
}

func convertFinalizationCertificateMessage(message *p2p.SimplexMessage, deserializer simplex.QCDeserializer) *simplex.Message {
	fc := message.GetFinalizationCertificate()
	qc, err := deserializer.DeserializeQuorumCertificate(fc.QuorumCertificate)
	if err != nil {
		panic(err) // TODO: fix this
	}

	metadata := fc.Finalization.Vote.Metadata

	msg := &simplex.Message{
		FinalizationCertificate: &simplex.FinalizationCertificate{
			QC: qc,
			Finalization: simplex.ToBeSignedFinalization{
				BlockHeader: metadataToBlockHeader(metadata),
			},
		},
	}

	copy(msg.FinalizationCertificate.Finalization.Digest[:], fc.Finalization.Vote.Digest)
	copy(msg.FinalizationCertificate.Finalization.Prev[:], metadata.Prev)

	return msg
}

func convertNotarizationMessage(message *p2p.SimplexMessage, deserializer simplex.QCDeserializer) *simplex.Message {
	notarization := message.GetNotarization()
	qc, err := deserializer.DeserializeQuorumCertificate(notarization.QuorumCertificate)
	if err != nil {
		panic(err) // TODO: fix this
	}

	metadata := notarization.Vote.Metadata

	msg := &simplex.Message{
		Notarization: &simplex.Notarization{
			Vote: simplex.ToBeSignedVote{
				BlockHeader: metadataToBlockHeader(metadata),
			},
		},
	}

	copy(msg.Notarization.Vote.Digest[:], notarization.Vote.Digest)
	copy(msg.Notarization.Vote.Prev[:], metadata.Prev)
	msg.Notarization.QC = qc

	return msg
}

func convertEmptyNotarizationMessage(message *p2p.SimplexMessage, deserializer simplex.QCDeserializer) *simplex.Message {
	emptyNotarization := message.GetEmptyNotarization()
	qc, err := deserializer.DeserializeQuorumCertificate(emptyNotarization.QuorumCertificate)
	if err != nil {
		panic(err) // TODO: fix this
	}

	metadata := emptyNotarization.EmptyVote.Metadata

	msg := &simplex.Message{
		EmptyNotarization: &simplex.EmptyNotarization{
			Vote: simplex.ToBeSignedEmptyVote{
				ProtocolMetadata: simplex.ProtocolMetadata{
					Round: metadata.Round,
					Epoch: metadata.Epoch,
					Seq:   metadata.Seq,
				},
			},
		},
	}

	copy(msg.EmptyNotarization.Vote.Prev[:], metadata.Prev)
	msg.EmptyNotarization.QC = qc

	return msg
}

func metadataToBlockHeader(metadata *p2p.ProtocolMetadata) simplex.BlockHeader {
	return simplex.BlockHeader{
		ProtocolMetadata: simplex.ProtocolMetadata{
			Round: metadata.Round,
			Epoch: metadata.Epoch,
			Seq:   metadata.Seq,
		},
	}
}

func convertVoteMessage(message *p2p.SimplexMessage) *simplex.Message {
	vote := message.GetVote()
	v := &simplex.Vote{
		Signature: simplex.Signature{
			Signer: vote.Signature.Signer,
			Value:  vote.Signature.Value,
		},
		Vote: simplex.ToBeSignedVote{
			BlockHeader: metadataToBlockHeader(vote.Vote.Metadata),
		},
	}
	copy(v.Vote.Digest[:], vote.Vote.Digest)      // TODO: check for nil
	copy(v.Vote.Prev[:], vote.Vote.Metadata.Prev) // TODO: check for nil
	return &simplex.Message{
		VoteMessage: v,
	}
}

func convertFinalizationMessage(message *p2p.SimplexMessage) *simplex.Message {
	finalization := message.GetFinalization()
	msg := &simplex.Message{
		Finalization: &simplex.Finalization{
			Signature: simplex.Signature{
				Signer: finalization.Signature.Signer,
				Value:  finalization.Signature.Value,
			},
			Finalization: simplex.ToBeSignedFinalization{
				BlockHeader: metadataToBlockHeader(finalization.Vote.Metadata),
			},
		},
	}

	copy(msg.Finalization.Finalization.Prev[:], finalization.Vote.Metadata.Prev)
	copy(msg.Finalization.Finalization.Digest[:], finalization.Vote.Digest)

	return msg
}

func convertEmptyVoteMessage(message *p2p.SimplexMessage) *simplex.Message {
	vote := message.GetEmptyVote()
	v := convertEmptyVote(&p2p.EmptyVote{
		Vote:      vote.Vote,
		Signature: vote.Signature,
	})
	return &simplex.Message{
		EmptyVoteMessage: &simplex.EmptyVote{
			Vote: simplex.ToBeSignedEmptyVote{
				ProtocolMetadata: v.Vote.ProtocolMetadata,
			},
			Signature: v.Signature,
		},
	}
}

func convertEmptyVote(vote *p2p.EmptyVote) *simplex.EmptyVote {
	v := &simplex.EmptyVote{
		Signature: simplex.Signature{
			Signer: vote.Signature.Signer,
			Value:  vote.Signature.Value,
		},
		Vote: simplex.ToBeSignedEmptyVote{
			ProtocolMetadata: metadataToBlockHeader(vote.Vote).ProtocolMetadata,
		},
	}
	copy(v.Vote.Prev[:], vote.Vote.Prev) // TODO: check for nil
	return v
}

func convertVote(vote *p2p.Vote) *simplex.Vote {
	v := &simplex.Vote{
		Signature: simplex.Signature{
			Signer: vote.Signature.Signer,
			Value:  vote.Signature.Value,
		},
		Vote: simplex.ToBeSignedVote{
			BlockHeader: metadataToBlockHeader(vote.Vote.Metadata),
		},
	}
	copy(v.Vote.Digest[:], vote.Vote.Digest)      // TODO: check for nil
	copy(v.Vote.Prev[:], vote.Vote.Metadata.Prev) // TODO: check for nil
	return v
}

func (e *Engine) removeDigestToIDMapping(digest simplex.Digest) {
	e.lock.Lock()
	defer e.lock.Unlock()

	delete(e.digestCache, digest)
}

func (e *Engine) observeDigestToIDMapping(digest simplex.Digest, blockID ids.ID) {
	e.lock.Lock()
	defer e.lock.Unlock()

	// the block ID is input to the entire simplex block, therefore
	// if the leader equivocates the simplex digest will be different.
	e.digestCache[digest] = blockID
}

func (e *Engine) convertBlockMessage(message *p2p.SimplexMessage) *simplex.Message {
	e.e.Logger.Info("converting block message", zap.Int("size", len(message.GetBlock().Block)))
	var block Block
	if err := block.verifiedBlock.FromBytes(message.GetBlock().Block); err != nil {
		e.ctx.Log.Warn("failed to parse block", zap.Error(err))
		return nil
	}

	e.e.Logger.Info("converted block message", zap.Int("inner block size", len(block.verifiedBlock.innerBlock)))

	block.vm = e.ChainVM
	block.e = e

	return &simplex.Message{
		BlockMessage: &simplex.BlockMessage{
			Block: &block,
			Vote:  *convertVote(message.GetBlock().Vote),
		},
	}
}
