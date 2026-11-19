// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

var emptyDigest = common.Digest{}

var (
	errAlreadyAccepted         = errors.New("tx already accepted")
	errAlreadyInChain          = errors.New("tx already in chain")
	errDuplicateTxInBlock      = errors.New("duplicate tx in block")
	errDoubleBlockVerification = errors.New("block has already been verified")
	errParentNotFound          = errors.New("parent block not accepted or verified")
)

type Mempool struct {
	lock     *sync.Mutex
	config   *FuzzConfig
	txsReady chan struct{}
	logger   common.Logger

	// txID -> TX
	unacceptedTxs map[txID]*TX

	// blocks that have been verified but not accepted
	verifiedButNotAcceptedBlocks map[common.Digest]*Block

	// all the blocks that have been accepted
	acceptedBlocks map[common.Digest]*Block

	// fast lookup of accepted txs
	acceptedTXs map[txID]struct{}
}

func NewMempool(l common.Logger, config *FuzzConfig) *Mempool {
	return &Mempool{
		unacceptedTxs:                make(map[txID]*TX),
		verifiedButNotAcceptedBlocks: make(map[common.Digest]*Block),
		acceptedTXs:                  make(map[txID]struct{}),
		acceptedBlocks:               make(map[common.Digest]*Block),
		lock:                         &sync.Mutex{},
		txsReady:                     make(chan struct{}, 1),
		logger:                       l,
		config:                       config,
	}
}

func (m *Mempool) AddPendingTXs(txs ...*TX) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		m.unacceptedTxs[tx.ID] = tx
	}
}

// NotifyTxsReady signals that there are pending transactions in the mempool.
func (m *Mempool) NotifyTxsReady() {
	select {
	case m.txsReady <- struct{}{}:
	default:
	}
}

// waitForPendingTxs waits until there are pending transactions in the mempool or the context is canceled
func (m *Mempool) waitForPendingTxs(ctx context.Context) {
	for {
		// Check if txs are available
		m.lock.Lock()
		m.logger.Debug("Checking for pending txs in mempool", zap.Int("unacceptedTxs", len(m.unacceptedTxs)))
		hasTxs := len(m.unacceptedTxs) > 0
		m.lock.Unlock()

		if hasTxs {
			return
		}

		m.logger.Debug("No pending txs in mempool, waiting for txs to be added")

		// No txs available, wait for notification or cancellation
		select {
		case <-m.txsReady:
			m.logger.Debug("Received notification of pending txs in mempool")
		case <-ctx.Done():
			return
		}
	}
}

// VerifyBlock verifies the block and its transactions. Errors if any tx is invalid or if there are duplicate txs in the block.
func (m *Mempool) VerifyBlock(ctx context.Context, b *Block) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	// Ensure the block has not already been verified or accepted
	if _, exists := m.verifiedButNotAcceptedBlocks[b.digest]; exists {
		m.logger.Error("Block has already been verified", zap.Error(errDoubleBlockVerification), zap.Stringer("Digest", b.digest))
		return fmt.Errorf("%w: %s", errDoubleBlockVerification, b.digest)
	}

	if _, exists := m.acceptedBlocks[b.digest]; exists {
		m.logger.Error("Block has already been accepted", zap.Error(errDoubleBlockVerification), zap.Stringer("Digest", b.digest))
		return fmt.Errorf("%w: %s", errDoubleBlockVerification, b.digest)
	}

	// Ensure the parent block is accepted or verified
	if parentInChain := m.isParentAcceptedOrVerified(b); !parentInChain {
		m.logger.Error("Parent has not been accepted or verified", zap.Error(errParentNotFound), zap.Stringer("Digest", b.digest))
		return fmt.Errorf("%w: parent digest %s, block digest %s", errParentNotFound, b.metadata.Prev, b.digest)
	}

	// Assert there are no duplicate txs in the block
	txIDSet := make(map[txID]struct{})
	for _, tx := range b.txs {
		if _, exists := txIDSet[tx.ID]; exists {
			return errDuplicateTxInBlock
		}
		txIDSet[tx.ID] = struct{}{}
	}

	// Verify each transaction
	for _, tx := range b.txs {
		if err := m.verifyTx(ctx, tx, b.metadata.Prev); err != nil {
			return err
		}
	}

	// Update state - don't delete from unverifiedTXs yet, as multiple nodes may build blocks with the same txs
	// txs will be deleted when the block is accepted
	m.verifiedButNotAcceptedBlocks[b.digest] = b

	return nil
}

func (m *Mempool) isParentAcceptedOrVerified(block *Block) bool {
	// Genesis block case
	if block.metadata.Prev == emptyDigest {
		return true
	}

	_, exists := m.acceptedBlocks[block.metadata.Prev]
	if exists {
		return true
	}

	_, exists = m.verifiedButNotAcceptedBlocks[block.metadata.Prev]
	return exists
}

// verifyTx verifies a single transaction against the mempool state and the block's chain.
func (m *Mempool) verifyTx(ctx context.Context, tx *TX, blockParent common.Digest) error {
	if _, exists := m.acceptedTXs[tx.ID]; exists {
		return fmt.Errorf("%w: %s", errAlreadyAccepted, tx.ID)
	}

	if m.isTxInChain(tx.ID, blockParent) {
		return errAlreadyInChain
	}

	if err := tx.Verify(ctx); err != nil {
		return err
	}
	return nil
}

// recursively check if the tx has already been included in any ancestor block to prevent double spends
func (m *Mempool) isTxInChain(txID txID, parentDigest common.Digest) bool {
	block, exists := m.verifiedButNotAcceptedBlocks[parentDigest]
	if !exists {
		return false
	}

	if block.containsTX(txID) {
		return true
	}

	return m.isTxInChain(txID, block.metadata.Prev)
}

// AcceptBlock accepts the block and updates the mempool
// state to clean up transactions, remove sibling/uncle blocks,
// and move any non-conflicting transactions from purged sibling/uncle blocks back to unaccepted
func (m *Mempool) AcceptBlock(b *Block) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.acceptedBlocks[b.digest] = b

	for _, tx := range b.txs {
		m.acceptedTXs[tx.ID] = struct{}{}
		delete(m.unacceptedTxs, tx.ID)
	}

	// delete any verified but not accepted blocks that are siblings or uncles and move not conflicting txs back to unaccepted
	delete(m.verifiedButNotAcceptedBlocks, b.digest)

	for _, verifiedBlock := range m.verifiedButNotAcceptedBlocks {
		// any block that shares a parent(excluding our block) should be purged
		if verifiedBlock.metadata.Prev == b.metadata.Prev {
			delete(m.verifiedButNotAcceptedBlocks, verifiedBlock.digest)
			m.purgeBlockAndChildren(verifiedBlock)
		}
	}

	if len(m.unacceptedTxs) > 0 {
		m.logger.Debug("After accepting block, moved txs back to unaccepted due to sibling/uncle blocks being purged", zap.Int("num unaccepted txs", len(m.unacceptedTxs)))
		m.NotifyTxsReady()
	}
}

// purgeBlockAndChildren goes through any blocks that build off of this one and move their txs
// back to unaccepted. It also moves this blocks transactions to unaccepted.
func (m *Mempool) purgeBlockAndChildren(block *Block) {
	m.moveTxsToUnaccepted(block)

	for digest, verifiedBlock := range m.verifiedButNotAcceptedBlocks {
		if verifiedBlock.metadata.Prev == block.digest {
			delete(m.verifiedButNotAcceptedBlocks, digest)
			m.purgeBlockAndChildren(verifiedBlock)
		}
	}
}

func (m *Mempool) moveTxsToUnaccepted(block *Block) {
	for _, tx := range block.txs {
		if _, exists := m.acceptedTXs[tx.ID]; !exists {
			m.unacceptedTxs[tx.ID] = tx
		}
	}
}

func (m *Mempool) BuildBlock(ctx context.Context, md common.ProtocolMetadata, bl common.Blacklist) (common.VerifiedBlock, bool) {
	m.waitForPendingTxs(ctx)

	// Pack the block once we have pending txs
	txs := m.packBlock(ctx, m.config.TxsPerBlock, md.Prev)
	if ctx.Err() != nil {
		return nil, false
	}

	// sort transactions
	slices.SortFunc(txs, func(a *TX, b *TX) int {
		return bytes.Compare(a.ID[:], b.ID[:])
	})

	block := NewBlock(md, bl, m, txs)
	m.logger.Debug("Built block with txs", zap.String("block digest", block.digest.String()), zap.Int("num txs", len(block.txs)), zap.Uint64("round", md.Round), zap.Uint64("seq", md.Seq))
	// in the future we can create a malicious block but we need to ensure the number of crashed nodes in under the threshold f(since we cant tolerate more than f malicious nodes)
	err := m.VerifyBlock(ctx, block)
	if err != nil {
		m.logger.Error("Failed to verify built block", zap.String("block digest", block.digest.String()), zap.Error(err))
		return nil, false
	}

	return block, true
}

func (m *Mempool) packBlock(ctx context.Context, maxTxs int, parentDigest common.Digest) []*TX {
	m.lock.Lock()
	defer m.lock.Unlock()

	txs := make([]*TX, 0, maxTxs)
	for _, tx := range m.unacceptedTxs {
		if err := m.verifyTx(ctx, tx, parentDigest); err != nil {
			m.logger.Debug("Skipping tx during block packing due to failed verification", zap.Stringer("txID", tx), zap.Error(err))
			continue
		}
		txs = append(txs, tx)
		if len(txs) >= maxTxs {
			break
		}
	}

	return txs
}

func (m *Mempool) WaitForPendingBlock(ctx context.Context) {
	m.waitForPendingTxs(ctx)
}

// IsTxAccepted returns true if the transaction has been accepted
func (m *Mempool) IsTxAccepted(txID txID) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, accepted := m.acceptedTXs[txID]
	return accepted
}

// IsTxPending returns true if the transaction is still pending (unaccepted)
func (m *Mempool) IsTxPending(txID txID) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, pending := m.unacceptedTxs[txID]
	return pending
}

// Clear resets the mempool state to simulate a node restart.
// We do not remove accepted & unaccepted transactions/blocks from the mempool(since we don't have tx gossip)
// but we do clear verified blocks since we are expected to re-verify after a restart.
func (m *Mempool) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()

	// move all the transactions from verified to unaccepted, since we are clearing the mempool but the transactions are still valid and can be re-included in future blocks
	for _, block := range m.verifiedButNotAcceptedBlocks {
		for _, tx := range block.txs {
			if _, accepted := m.acceptedTXs[tx.ID]; !accepted {
				m.unacceptedTxs[tx.ID] = tx
			}
		}
	}

	m.verifiedButNotAcceptedBlocks = make(map[common.Digest]*Block)
}
