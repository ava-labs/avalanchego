// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const numSyncWorkers = 5

var (
	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeSizeExceeded  = errors.New("max code size exceeded")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
)

// Syncer fetches contract code by hash from the network and persists it to db.
type Syncer struct {
	log        logging.Logger
	client     *Client
	db         ethdb.KeyValueStore
	codeHashes <-chan common.Hash

	codeHashesPerReq int // best-effort target size, the final batch may be smaller
}

// fetchResult carries a verified fetch back to the manager. The hashes travel
// with it because batches complete out of order.
type fetchResult struct {
	hashes []common.Hash
	data   [][]byte
}

// NewSyncer returns a [Syncer] that reads code hashes from codeHashes and writes
// verified code into db, fetching from peers through c.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore, codeHashes <-chan common.Hash) *Syncer {
	return &Syncer{
		log:              log,
		client:           c,
		db:               db,
		codeHashes:       codeHashes,
		codeHashesPerReq: maxHashesPerRequest,
	}
}

// Sync runs until codeHashes is drained and closed, or ctx ends.
func (s *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(numSyncWorkers)

	var (
		retErr error
		batch  = make([]common.Hash, 0, s.codeHashesPerReq)
	)
loop:
	for {
		select {
		case <-egCtx.Done():
			retErr = egCtx.Err()
			break loop

		case codeHash, ok := <-s.codeHashes:
			if !ok {
				break loop
			}
			missing, err := s.needsFetch(codeHash)
			switch {
			case err != nil:
				retErr = err
				break loop
			case !missing:
				continue
			}
			batch = append(batch, codeHash)

			if len(batch) >= s.codeHashesPerReq {
				send := batch
				eg.Go(func() error {
					return s.fetchAndPersist(egCtx, send)
				})
				batch = make([]common.Hash, 0, s.codeHashesPerReq)
			}
		}
	}

	retErr = errors.Join(retErr, eg.Wait())
	if retErr != nil {
		return retErr
	}

	if len(batch) > 0 {
		return s.fetchAndPersist(ctx, batch)
	}
	return nil
}

// fetchAndPersist fetches code for hashes from the network and writes it to db.
func (s *Syncer) fetchAndPersist(ctx context.Context, hashes []common.Hash) error {
	data, err := getCode(ctx, s.log, s.client, hashes)
	if err != nil {
		return err
	}
	return s.persist(hashes, data)
}

// needsFetch reports whether codeHash has to come from a peer.
func (s *Syncer) needsFetch(codeHash common.Hash) (bool, error) {
	if !rawdb.HasCode(s.db, codeHash) {
		return true, nil
	}
	if err := customrawdb.DeleteCodeToFetch(s.db, codeHash); err != nil {
		return false, fmt.Errorf("failed to delete stale code marker: %w", err)
	}
	return false, nil
}

// persist writes the code and clears the to-fetch markers in one batch.
func (s *Syncer) persist(hashes []common.Hash, data [][]byte) error {
	batch := s.db.NewBatch()
	for i, codeHash := range hashes {
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to delete code to fetch marker: %w", err)
		}
		rawdb.WriteCode(batch, codeHash, data[i])
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write fetched code: %w", err)
	}
	return nil
}

// getCode requests hashes through c, verifies every returned blob against its
// hash, and scores the peer. It re-requests on any network or verification
// failure until ctx ends.
func getCode(ctx context.Context, log logging.Logger, c *Client, hashes []common.Hash) ([][]byte, error) {
	req := &syncpb.GetCodeRequest{Hashes: hashBytes(hashes)}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp := &syncpb.GetCodeResponse{}
		outcome, err := c.Send(ctx, req, resp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			continue
		}

		data := resp.GetData()
		if err := verifyCode(hashes, data); err != nil {
			outcome.Failure()
			log.Debug("invalid code response, re-requesting",
				zap.Stringer("nodeID", outcome.NodeID()),
				zap.Error(err),
			)
			continue
		}

		outcome.Success()
		return data, nil
	}
}

// verifyCode reports whether data is the code for hashes, in order.
func verifyCode(hashes []common.Hash, data [][]byte) error {
	if len(data) != len(hashes) {
		return fmt.Errorf("%w: got %d requested %d", errCodeCountMismatch, len(data), len(hashes))
	}
	for i, code := range data {
		// Not needed for correctness, since an oversized blob cannot hash to
		// what was asked for. It bounds the work a peer can force on us to one
		// keccak over MaxCodeSize rather than one over the whole message.
		if len(code) > params.MaxCodeSize {
			return fmt.Errorf("%w: hash %s size %d", errCodeSizeExceeded, hashes[i], len(code))
		}
		if got := crypto.Keccak256Hash(code); got != hashes[i] {
			return fmt.Errorf("%w at index %d: got %s requested %s", errCodeHashMismatch, i, got, hashes[i])
		}
	}
	return nil
}

func hashBytes(hashes []common.Hash) [][]byte {
	raw := make([][]byte, len(hashes))
	for i, h := range hashes {
		raw[i] = h.Bytes()
	}
	return raw
}
