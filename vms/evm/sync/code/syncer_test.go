// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

type (
	codeResponder = handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]
	codeRecorder  = synctest.RecordingResponder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]
)

type SUT struct {
	*Syncer

	// Used to support [SUT.restart].
	log    *loggingtest.Logger
	client *Client

	// db views the syncer's store without [SUT.flakyDB]'s fault injection.
	db       ethdb.KeyValueStore
	flakyDB  *saetest.FlakyDB
	recorder *codeRecorder
}

// sutOption configures the SUT built by [newSUT] and [tryNewSUT].
type sutOption = options.Option[sutConfig]

type sutConfig struct {
	code          codes
	flake         int
	wrapResponder func(codeResponder) codeResponder
}

// withCode seeds the peer's database with code for the syncer to fetch.
func withCode(code codes) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.code = code
	})
}

// withDBFlake fails the syncer's database mutations with
// [saetest.ErrInjected] after flake of them succeed.
func withDBFlake(flake int) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.flake = flake
	})
}

// withWrappedResponder wraps the responder serving the syncer's requests.
func withWrappedResponder(wrap func(codeResponder) codeResponder) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.wrapResponder = wrap
	})
}

func newSUT(t *testing.T, opts ...sutOption) *SUT {
	t.Helper()

	sut, err := tryNewSUT(t, opts...)
	require.NoError(t, err)
	return sut
}

// tryNewSUT returns any [NewSyncer] error rather than failing t. On error the
// SUT has a nil [Syncer], and [SUT.restart] can install a working one.
func tryNewSUT(t *testing.T, opts ...sutOption) (*SUT, error) {
	t.Helper()

	config := options.ApplyTo(&sutConfig{
		flake: math.MaxInt,
	}, opts...)

	clientDB := evmdb.New(memdb.New())
	writeCode(clientDB, config.code)

	log := loggingtest.New(t, logging.Debug)
	responder := newResponder(log, clientDB)
	recorder := synctest.NewRecordingResponder(responder)

	var wrappedResponder codeResponder = recorder
	if config.wrapResponder != nil {
		wrappedResponder = config.wrapResponder(wrappedResponder)
	}
	client := NewClient(synctest.ServeResponder(
		t,
		t.Context(),
		log,
		p2p.EVMCodeRequestHandlerID,
		wrappedResponder,
	))

	avadb := memdb.New()
	flakydb := saetest.NewFlakyDB(avadb, config.flake)
	syncer, err := NewSyncer(log, client, evmdb.New(flakydb))

	return &SUT{
		Syncer:   syncer,
		log:      log,
		client:   client,
		db:       evmdb.New(avadb),
		flakyDB:  flakydb,
		recorder: recorder,
	}, err
}

func (s *SUT) assertHasCode(t *testing.T, c codes) {
	t.Helper()

	for hash, code := range c {
		assert.Equalf(t, code, rawdb.ReadCode(s.db, hash), "expecting code with hash %s", hash)
	}
}

func (s *SUT) assertNoCodeToSync(t *testing.T) {
	t.Helper()

	it := customrawdb.NewCodeToFetchIterator(s.db)
	defer it.Release()

	assert.False(t, it.Next(), "expecting no code to fetch")
}

func (s *SUT) restart(t *testing.T) {
	t.Helper()

	syncer, err := NewSyncer(s.log, s.client, s.db)
	require.NoError(t, err)
	s.Syncer = syncer
}

type codes = map[common.Hash][]byte

func newCodes(t *testing.T, num int) codes {
	t.Helper()

	c := make(codes, num)
	for range num {
		hash, code := randomCode(t)
		c[hash] = code
	}
	return c
}

func randomCode(t *testing.T) (common.Hash, []byte) {
	t.Helper()

	code := make([]byte, 128)
	_, err := rand.Read(code)
	require.NoError(t, err)
	hash := crypto.Keccak256Hash(code)
	return hash, code
}

func writeCode(db ethdb.KeyValueWriter, c codes) {
	for h, code := range c {
		rawdb.WriteCode(db, h, code)
	}
}

func TestSyncer(t *testing.T) {
	tests := []struct {
		name          string
		numFromSource int
		numOnDisk     int
		// copies adds each hash this many times, expecting a single fetch.
		// Zero is remapped to one.
		copies int
	}{
		{name: "empty"},
		{name: "single_blob", numFromSource: 1},
		{name: "batches_across_requests", numFromSource: 3 * maxHashesPerRequest},
		{name: "partial_final_batch", numFromSource: 2*maxHashesPerRequest + 1},
		{name: "skips_code_already_on_disk", numFromSource: 3, numOnDisk: 2},
		{name: "repeats_fetched_once", numFromSource: 1, copies: 200},
		{name: "repeats_at_batch_boundary_fetched_once", numFromSource: maxHashesPerRequest, copies: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			clientCode := newCodes(t, tt.numFromSource)
			// Cancel if the syncer requests more batches than expected rather
			// than re-requesting until the test timeout.
			wantRequests := (tt.numFromSource + maxHashesPerRequest - 1) / maxHashesPerRequest
			sut := newSUT(t,
				withCode(clientCode),
				withWrappedResponder(func(cr codeResponder) codeResponder {
					return synctest.NewCancelAfter(cr, wantRequests+1, cancel)
				}),
			)

			// The peer cannot serve localCode, so the syncer MUST NOT request
			// code that is already on disk.
			localCode := newCodes(t, tt.numOnDisk)
			writeCode(sut.db, localCode)

			allCode := make(codes)
			maps.Copy(allCode, clientCode)
			maps.Copy(allCode, localCode)

			// Add concurrently while the syncer runs, so adds race each other
			// along with the batcher.
			var syncEG errgroup.Group
			syncEG.Go(func() error {
				// Deferred until every add returns, since a later AddCode
				// would be refused.
				defer sut.DoneAdding()

				var addEG errgroup.Group
				for hash := range allCode {
					for range max(tt.copies, 1) {
						addEG.Go(func() error {
							return sut.AddCode([]common.Hash{hash})
						})
					}
				}
				return addEG.Wait()
			})
			syncEG.Go(func() error {
				return sut.Sync(ctx)
			})

			err := syncEG.Wait()
			require.NoError(t, ctx.Err(), "syncer should not be cancelled")
			require.NoError(t, err)

			sut.assertHasCode(t, allCode)
			sut.assertNoCodeToSync(t)

			requests := sut.recorder.Requests()
			assert.Len(t, requests, wantRequests, "syncer sent wrong number of requests")
			requested := 0
			for _, request := range requests {
				size := len(request.GetHashes())
				assert.LessOrEqual(t, size, maxHashesPerRequest, "syncer violated the request size limit")
				requested += size
			}
			assert.Equal(t, tt.numFromSource, requested, "every missing hash should be requested exactly once")
		})
	}
}

func TestSyncer_Retries(t *testing.T) {
	const failures = 2
	tests := []struct {
		name          string
		wrapResponder func(codeResponder) codeResponder
	}{
		{
			name: "tampered_response",
			wrapResponder: func(cr codeResponder) codeResponder {
				return synctest.NewMutatingResponder(cr, failures, func(resp *syncpb.GetCodeResponse) {
					for i := range resp.GetData() {
						resp.Data[i] = []byte("tampered")
					}
				})
			},
		},
		{
			name: "rejected_request",
			wrapResponder: func(cr codeResponder) codeResponder {
				return synctest.NewErroringResponder(cr, failures, errHashNotFound)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code := newCodes(t, 1)
			// The syncer must retry tampered responses until a correct one arrives.
			sut := newSUT(t,
				withCode(code),
				withWrappedResponder(test.wrapResponder),
			)

			for hash := range code {
				require.NoError(t, sut.AddCode([]common.Hash{hash}))
			}
			sut.DoneAdding()

			ctx := t.Context()
			require.NoError(t, sut.Sync(ctx))

			sut.assertHasCode(t, code)
			assert.Lenf(t, sut.recorder.Requests(), failures+1, "%d failed attempts and a correct response", failures)
		})
	}
}

func TestSyncer_SecondSyncRefused(t *testing.T) {
	sut := newSUT(t)
	sut.DoneAdding()

	require.NoError(t, sut.Sync(t.Context()))
	require.ErrorIs(t, sut.Sync(t.Context()), errSyncAlreadyRun)
}

// A cancelled Sync must leave the store recoverable, so a fresh syncer can
// finish the job.
func TestSyncer_ResumesAfterCancel(t *testing.T) {
	// Two batches leave one in flight and one queued when the cancel fires.
	code := newCodes(t, maxHashesPerRequest+1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sut := newSUT(t,
		withCode(code),
		withWrappedResponder(func(cr codeResponder) codeResponder {
			return synctest.NewCancelAfter(cr, 1, cancel)
		}),
	)

	require.NoError(t, sut.AddCode(slices.Collect(maps.Keys(code))))
	// DoneAdding is never called, so only the cancel can end this Sync.
	require.ErrorIs(t, sut.Sync(ctx), context.Canceled)

	sut.restart(t)
	sut.DoneAdding()
	require.NoError(t, sut.Sync(t.Context()))

	sut.assertHasCode(t, code)
	sut.assertNoCodeToSync(t)
}

func TestClaimSet(t *testing.T) {
	t.Parallel()

	var (
		c      claimSet
		hashes = make([]common.Hash, 100)
	)
	for i := range hashes {
		hash := common.Hash{byte(i)}
		hashes[i] = hash

		require.True(t, c.claim(hash))
		require.False(t, c.claim(hash), "a held hash cannot be claimed again")
	}

	c.release(hashes...)
	for _, hash := range hashes {
		require.True(t, c.claim(hash), "a released hash can be claimed again")
	}
}

func TestVerifyCode(t *testing.T) {
	hash, code := randomCode(t)

	tests := []struct {
		name    string
		codes   [][]byte
		wantErr error
	}{
		{
			name:  "valid",
			codes: [][]byte{code},
		},
		{
			name:    "count_mismatch",
			codes:   [][]byte{},
			wantErr: errCodeCountMismatch,
		},
		{
			name:    "hash_mismatch",
			codes:   [][]byte{[]byte("tampered")},
			wantErr: errCodeHashMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, verifyCode([]common.Hash{hash}, tt.codes), tt.wantErr)
		})
	}
}

// AddCode after DoneAdding must be refused and leave no marker. A leftover
// marker is code owed with nothing running to fetch it.
func TestSyncer_AddCodeAfterDoneAdding(t *testing.T) {
	sut := newSUT(t)
	sut.DoneAdding()

	tests := []struct {
		name   string
		hashes []common.Hash
	}{
		{
			name: "no_hashes",
		},
		{
			name:   "single_hash",
			hashes: []common.Hash{{1}},
		},
		{
			name:   "multiple_hashes",
			hashes: []common.Hash{{1}, {2}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorIs(t, sut.AddCode(test.hashes), errDoneAdding)
			sut.assertNoCodeToSync(t)
		})
	}
}

// AddCode racing DoneAdding should maintain AddCode's invariant that all
// successfully provided hashes will eventually be fetched.
func TestSyncer_AddCodeRacesDoneAdding(t *testing.T) {
	code := newCodes(t, 50)
	sut := newSUT(t, withCode(code))

	hashes := slices.Collect(maps.Keys(code))
	errs := make([]error, len(hashes))
	var wg sync.WaitGroup
	for i, hash := range hashes {
		wg.Go(func() {
			errs[i] = sut.AddCode([]common.Hash{hash})
		})
	}
	wg.Go(sut.DoneAdding)
	wg.Wait()

	// Restart recovers every hash that was added successfully. After re-adding
	// the refused ones, a sync must fetch all the code.
	sut.restart(t)
	for i, hash := range hashes {
		if errs[i] != nil {
			require.NoError(t, sut.AddCode([]common.Hash{hash}))
		}
	}
	sut.DoneAdding()

	require.NoError(t, sut.Sync(t.Context()))

	sut.assertHasCode(t, code)
	sut.assertNoCodeToSync(t)
}

// A crash at any single write must leave the store recoverable by a fresh
// syncer plus a re-add of whatever AddCode refused.
func TestSyncer_CrashRecovery(t *testing.T) {
	// Multiple adds and multiple batches put concurrent worker commits in the
	// op stream.
	adds := []codes{
		newCodes(t, maxHashesPerRequest),
		newCodes(t, maxHashesPerRequest+1),
	}
	allCode := make(codes)
	for _, c := range adds {
		maps.Copy(allCode, c)
	}

	// A clean run counts the ops, so the sweep can crash at every one of them.
	ops := func() int {
		sut := newSUT(t, withCode(allCode))
		for _, c := range adds {
			require.NoError(t, sut.AddCode(slices.Collect(maps.Keys(c))))
		}
		sut.DoneAdding()
		require.NoError(t, sut.Sync(t.Context()))
		return sut.flakyDB.Calls()
	}()
	require.Positive(t, ops)

	for failAfter := range ops {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			toAdd := slices.Clone(adds)
			trySync := func(s *SUT) error {
				for len(toAdd) > 0 {
					c := toAdd[0]
					if err := s.AddCode(slices.Collect(maps.Keys(c))); err != nil {
						return err
					}
					// Only a successful AddCode promises a durable marker.
					toAdd = toAdd[1:]
				}
				s.DoneAdding()
				return s.Sync(t.Context())
			}

			sut, err := tryNewSUT(t,
				withCode(allCode),
				withDBFlake(failAfter),
			)
			if err == nil {
				err = trySync(sut)
			}
			if err != nil {
				require.ErrorIs(t, err, saetest.ErrInjected)
			}

			sut.restart(t)
			require.NoError(t, trySync(sut))
			sut.assertHasCode(t, allCode)
			sut.assertNoCodeToSync(t)
		})
	}
}
