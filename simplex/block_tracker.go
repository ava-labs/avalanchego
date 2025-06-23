package simplex

import (
	"bytes"
	"context"
	"sync"

	"github.com/ava-labs/simplex"
)


type blockRejection struct {
	digest simplex.Digest
	reject func(context.Context) error
}

// blockTracker maps rounds to blocks verified in that round that may be rejected
type blockTracker struct {
	lock            sync.Mutex
	round2Rejection map[uint64][]blockRejection
}

func (bt *blockTracker) init() {
	bt.round2Rejection = make(map[uint64][]blockRejection)
}

func (bt *blockTracker) rejectSiblingsAndUncles(round uint64, acceptedDigest simplex.Digest) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bt.disposeOfUncles(round)
	bt.disposeOfSiblings(round, acceptedDigest)
	// Completely get rid of the round, to make sure that the accepted block is not rejected in future rounds.
	delete(bt.round2Rejection, round)
}

func (bt *blockTracker) disposeOfSiblings(round uint64, acceptedDigest simplex.Digest) {
	for _, rejection := range bt.round2Rejection[round] {
		if !bytes.Equal(rejection.digest[:], acceptedDigest[:]) {
			rejection.reject(context.Background())
		}
	}
}

func (bt *blockTracker) disposeOfUncles(round uint64) {
	for r, rejections := range bt.round2Rejection {
		if r < round {
			for _, rejection := range rejections {
				rejection.reject(context.Background())
			}
			delete(bt.round2Rejection, r)
		}
	}
}

func (bt *blockTracker) trackBlock(round uint64, digest simplex.Digest, reject func(context.Context) error) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bt.round2Rejection[round] = append(bt.round2Rejection[round], blockRejection{
		digest: digest,
		reject: reject,
	})
}
