// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/sync/errgroup"
)

const defaultNumThreads int = 4

type TrieProgress struct {
	trie      *trie.StackTrie
	batch     ethdb.Batch
	batchSize int
	startFrom []byte

	// used for ETA calculations
	startTime time.Time
	eta       SyncETA
}

func NewTrieProgress(db ethdb.Batcher, batchSize int, eta SyncETA) *TrieProgress {
	batch := db.NewBatch()
	return &TrieProgress{
		batch:     batch,
		batchSize: batchSize,
		trie:      trie.NewStackTrie(batch),
		eta:       eta,
	}
}

type StorageTrieProgress struct {
	*TrieProgress
	Account            common.Hash
	AdditionalAccounts []common.Hash
	Skipped            bool
}

// StateSyncProgress tracks the progress of syncing the main trie and the
// sub-tasks for syncing storage tries.
type StateSyncProgress struct {
	MainTrie     *TrieProgress
	MainTrieDone bool
	Root         common.Hash
	StorageTries map[common.Hash]*StorageTrieProgress
}

// stateSyncer manages syncing the main trie and storage tries concurrently from peers.
// Invariant that allows resumability: Each account with a snapshot entry and a non-empty
// storage trie MUST either:
// (a) have its storage trie fully on disk and its snapshot populated with the same data as the trie, or
// (b) have an entry in the progress marker persisted to disk.
// In case there is an entry for a storage trie in the progress marker, the in progress
// sync for that storage trie will be resumed prior to resuming the main trie sync.
// This ensures the number of tries in progress remains less than or equal to [numThreads].
// Once fewer than [numThreads] storage tries are in progress, the main trie sync will
// continue concurrently.
//
// Note: stateSyncer assumes that the snapshot will be wiped completely prior to starting
// a new sync task (or if the target sync root changes or the snapshot is modified by normal operation).
type stateSyncer struct {
	lock           sync.Mutex
	progressMarker *StateSyncProgress
	numThreads     int

	syncer     *syncclient.CallbackLeafSyncer
	codeSyncer *codeSyncer
	trieDB     *trie.Database
	db         ethdb.Database
	batchSize  int
	client     syncclient.Client

	done chan error
	eta  SyncETA
}

type EVMStateSyncerConfig struct {
	Root                     common.Hash
	Client                   syncclient.Client
	DB                       ethdb.Database
	BatchSize                int
	MaxOutstandingCodeHashes int // Maximum number of code hashes in the code syncer queue
	NumCodeFetchingWorkers   int // Number of code syncing threads
}

func NewEVMStateSyncer(config *EVMStateSyncerConfig) (*stateSyncer, error) {
	eta := NewSyncEta(config.Root)
	progressMarker, err := loadProgress(config.DB, config.Root)
	if err != nil {
		return nil, err
	}

	// initialise tries in the progress marker
	progressMarker.MainTrie = NewTrieProgress(config.DB, config.BatchSize, eta)
	if err := restoreMainTrieProgressFromSnapshot(config.DB, progressMarker.MainTrie); err != nil {
		return nil, err
	}

	for _, storageProgress := range progressMarker.StorageTries {
		storageProgress.TrieProgress = NewTrieProgress(config.DB, config.BatchSize, eta)
		// the first account's storage snapshot contains the key/value pairs we need to restore
		// the stack trie. if other in-progress accounts happen to share the same storage root,
		// their storage snapshot remains empty until the storage trie is fully synced, then it
		// will be copied from the first account's storage snapshot.
		if err := restoreStorageTrieProgressFromSnapshot(config.DB, storageProgress.TrieProgress, storageProgress.Account); err != nil {
			return nil, err
		}
	}

	return &stateSyncer{
		progressMarker: progressMarker,
		batchSize:      config.BatchSize,
		client:         config.Client,
		trieDB:         trie.NewDatabase(config.DB),
		db:             config.DB,
		numThreads:     defaultNumThreads,
		syncer:         syncclient.NewCallbackLeafSyncer(config.Client),
		codeSyncer: newCodeSyncer(CodeSyncerConfig{
			DB:                       config.DB,
			Client:                   config.Client,
			MaxOutstandingCodeHashes: config.MaxOutstandingCodeHashes,
			NumCodeFetchingWorkers:   config.NumCodeFetchingWorkers,
		}),
		eta:  eta,
		done: make(chan error, 1),
	}, nil
}

// Start starts the leaf syncer on the root task as well as any in-progress storage tasks.
func (s *stateSyncer) Start(ctx context.Context) {
	rootTask := &syncclient.LeafSyncTask{
		Root:          s.progressMarker.Root,
		Start:         s.progressMarker.MainTrie.startFrom,
		NodeType:      message.StateTrieNode,
		OnLeafs:       s.handleLeafs,
		OnFinish:      s.onFinish,
		OnSyncFailure: s.onSyncFailure,
	}

	storageTasks := make([]*syncclient.LeafSyncTask, 0, len(s.progressMarker.StorageTries))
	for storageRoot, storageTrieProgress := range s.progressMarker.StorageTries {
		storageTasks = append(storageTasks, &syncclient.LeafSyncTask{
			Root:          storageRoot,
			Account:       storageTrieProgress.Account,
			Start:         storageTrieProgress.startFrom,
			NodeType:      message.StateTrieNode,
			OnLeafs:       storageTrieProgress.handleLeafs,
			OnFinish:      s.onFinish,
			OnSyncFailure: s.onSyncFailure,
		})
	}

	// Start the syncer and code syncer.
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		s.syncer.Start(ctx, s.numThreads, rootTask, storageTasks...)
		err := <-s.syncer.Done()
		return err
	})
	eg.Go(func() error {
		s.codeSyncer.start(ctx)
		err := <-s.codeSyncer.Done()
		return err
	})

	// The errgroup wait will take care of returning the first error that occurs, or returning
	// nil if both finish without an error.
	go func() {
		s.done <- eg.Wait()
	}()
}

func (s *stateSyncer) handleLeafs(root common.Hash, keys [][]byte, values [][]byte) ([]*syncclient.LeafSyncTask, error) {
	var (
		tasks    []*syncclient.LeafSyncTask
		mainTrie = s.progressMarker.MainTrie
	)
	if mainTrie.startTime.IsZero() {
		mainTrie.startTime = time.Now()
	}

	codeHashes := make([]common.Hash, 0)
	for i, key := range keys {
		value := values[i]
		accountHash := common.BytesToHash(key)
		if err := mainTrie.trie.TryUpdate(key, value); err != nil {
			return nil, err
		}

		// decode value into types.StateAccount
		var acc types.StateAccount
		if err := rlp.DecodeBytes(value, &acc); err != nil {
			return nil, fmt.Errorf("could not decode main trie as account, key=%s, valueLen=%d, err=%w", common.Bytes2Hex(key), len(value), err)
		}

		// check if this account has storage root that we need to fetch
		if acc.Root != (common.Hash{}) && acc.Root != types.EmptyRootHash {
			if storageTask, err := s.createStorageTrieTask(accountHash, acc.Root); err != nil {
				return nil, err
			} else if storageTask != nil {
				tasks = append(tasks, storageTask)
			}
		}

		// check if this account has code and fetch it
		codeHash := common.BytesToHash(acc.CodeHash)
		if codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash {
			codeHashes = append(codeHashes, codeHash)
		}

		// write account snapshot
		writeAccountSnapshot(mainTrie.batch, accountHash, acc)
	}

	// Add collected code hashes to the code syncer before writing the main trie progress to disk.
	// This ensures that the main trie will not have progress on disk, unless there is a marker for
	// the required code bytes.
	if err := s.codeSyncer.addCode(codeHashes); err != nil {
		return nil, err
	}
	// Only write the main trie batch to disk after we have added any code tasks necessary to disk.
	// Note: we do not need to check the size of the batch within the loop, since there is a limit of 1024 leaf
	// keys that will be handled each time.
	if mainTrie.batch.ValueSize() > mainTrie.batchSize {
		if err := mainTrie.batch.Write(); err != nil {
			return nil, err
		}
		mainTrie.batch.Reset()
	}

	if len(keys) > 0 {
		// notify progress for eta calculations on the last key
		mainTrie.eta.NotifyProgress(root, mainTrie.startTime, mainTrie.startFrom, keys[len(keys)-1])
	}
	return tasks, nil
}

func (tp *StorageTrieProgress) handleLeafs(root common.Hash, keys [][]byte, values [][]byte) ([]*syncclient.LeafSyncTask, error) {
	// Note this method does not need to hold a lock:
	// - handleLeafs is called synchronously by CallbackLeafSyncer
	// - if an additional account is encountered with the same storage trie,
	//   it will be appended to [tp.AdditionalAccounts] (not accessed here)
	if tp.startTime.IsZero() {
		tp.startTime = time.Now()
	}
	for i, key := range keys {
		if err := tp.trie.TryUpdate(key, values[i]); err != nil {
			return nil, err
		}
		keyHash := common.BytesToHash(key)
		// write to [tp.Account] here, the snapshot for [tp.AdditionalAccounts] will be populated
		// after the trie is finished syncing by copying entries from [tp.Account]'s storage snapshot.
		rawdb.WriteStorageSnapshot(tp.batch, tp.Account, keyHash, values[i])
		if tp.batch.ValueSize() > tp.batchSize {
			if err := tp.batch.Write(); err != nil {
				return nil, err
			}
			tp.batch.Reset()
		}
	}
	if len(keys) > 0 {
		// notify progress for eta calculations on the last key
		tp.eta.NotifyProgress(root, tp.startTime, tp.startFrom, keys[len(keys)-1])
	}
	return nil, nil // storage tries never add new tasks to the leaf syncer
}

// createStorageTrieTask creates a LeafSyncTask to be returned from the callback,
// and records the storage trie as in progress to maintain the resumability invariant.
func (s *stateSyncer) createStorageTrieTask(accountHash common.Hash, storageRoot common.Hash) (*syncclient.LeafSyncTask, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// check if we're already syncing this storage trie.
	// if we are: add this account hash to the progress marker so
	// when the trie is downloaded, the snapshot will be copied
	// to this account as well
	if storageProgress, exists := s.progressMarker.StorageTries[storageRoot]; exists {
		storageProgress.AdditionalAccounts = append(storageProgress.AdditionalAccounts, accountHash)
		return nil, addInProgressTrie(s.db, storageRoot, accountHash)
	}

	progress := &StorageTrieProgress{
		TrieProgress: NewTrieProgress(s.db, s.batchSize, s.eta),
		Account:      accountHash,
	}
	s.progressMarker.StorageTries[storageRoot] = progress
	return &syncclient.LeafSyncTask{
		Root:          storageRoot,
		Account:       accountHash,
		NodeType:      message.StateTrieNode,
		OnLeafs:       progress.handleLeafs,
		OnFinish:      s.onFinish,
		OnSyncFailure: s.onSyncFailure,
		OnStart: func(common.Hash) (bool, error) {
			// check if this storage root is on disk
			storageTrie, err := trie.New(storageRoot, s.trieDB)
			if err != nil {
				return false, nil
			}

			// If the storage trie is already on disk, we only need to populate the storage snapshot for [accountHash]
			// with the trie contents. There is no need to re-sync the trie, since it is already present.
			if err := writeAccountStorageSnapshotFromTrie(s.db.NewBatch(), s.batchSize, accountHash, storageTrie); err != nil {
				// If the storage trie cannot be iterated (due to an incomplete trie from pruning this storage trie in the past)
				// then we re-sync it here. Therefore, this error is not fatal and we can safely continue here.
				log.Info("could not populate storage snapshot from trie with existing root, syncing from peers instead", "account", accountHash, "root", storageRoot, "err", err)
				return false, nil
			}

			// If populating the snapshot from the existing storage trie was successful,
			// return true to skip this task
			progress.Skipped = true              // set skipped to true to avoid committing the stack trie in onFinish
			return true, s.onFinish(storageRoot) // call onFinish to delete this task from the map. onFinish will take [s.lock]
		},
	}, addInProgressTrie(s.db, storageRoot, accountHash)
}

// onFinish marks the task corresponding to [root] as finished.
// If [root] is a storage root, then we remove it from the progress marker.
// when the progress marker contains no more storage root and the
// main trie is marked as complete, the main trie's root is committed (see checkAllDone).
func (s *stateSyncer) onFinish(root common.Hash) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// handle the case where root is the main trie's root
	if root == s.progressMarker.Root {
		// mark main trie as done.
		s.progressMarker.MainTrieDone = true
		// Notify the code syncer that there will be no more code hashes to request.
		s.codeSyncer.notifyAccountTrieCompleted()
		return s.checkAllDone()
	}

	// since root was not the main trie, it must belong to a storage trie.
	storageTrieProgress, exists := s.progressMarker.StorageTries[root]
	if !exists {
		return fmt.Errorf("unknown root [%s] finished syncing", root)
	}

	if !storageTrieProgress.Skipped {
		storageRoot, err := storageTrieProgress.trie.Commit()
		if err != nil {
			return err
		}
		if storageRoot != root {
			return fmt.Errorf("unexpected storage root, expected=%s, actual=%s account=%s", root, storageRoot, storageTrieProgress.Account)
		}
	}
	// Note: we hold the lock when copying storage snapshots and adding new accounts.
	// This prevents race conditions between these two operations.
	if len(storageTrieProgress.AdditionalAccounts) > 0 {
		// It is necessary to flush the batch here to write
		// any pending items to the storage snapshot before
		// we use that as a source to copy to other accounts.
		if err := storageTrieProgress.batch.Write(); err != nil {
			return err
		}
		storageTrieProgress.batch.Reset()
		if err := copyStorageSnapshot(
			s.db,
			storageTrieProgress.Account,
			storageTrieProgress.batch,
			storageTrieProgress.batchSize,
			storageTrieProgress.AdditionalAccounts,
		); err != nil {
			return err
		}
	}
	delete(s.progressMarker.StorageTries, root)
	// clear the progress marker on completion of the trie
	if err := storageTrieProgress.batch.Write(); err != nil {
		return err
	}
	if err := removeInProgressStorageTrie(s.db, root, storageTrieProgress); err != nil {
		return err
	}
	s.eta.RemoveSyncedTrie(root, storageTrieProgress.Skipped)
	return s.checkAllDone()
}

// checkAllDone checks if there are no more tries in progress and the main trie is complete
// this will write the main trie's root to disk, and is the last step of stateSyncer's process.
// assumes lock is held
func (s *stateSyncer) checkAllDone() error {
	// Note: this check ensures we do not commit the main trie until all of the storage tries
	// have been committed.
	if !s.progressMarker.MainTrieDone || len(s.progressMarker.StorageTries) > 0 {
		return nil
	}

	mainTrie := s.progressMarker.MainTrie
	mainTrieRoot, err := mainTrie.trie.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit main trie: %w", err)
	}
	if mainTrieRoot != s.progressMarker.Root {
		return fmt.Errorf("expected main trie root [%s] not same as actual [%s]", s.progressMarker.Root, mainTrieRoot)
	}
	if err := mainTrie.batch.Write(); err != nil {
		return err
	}
	// remove the main trie storage marker, after which there should be none in the db.
	return removeInProgressTrie(s.db, mainTrieRoot, common.Hash{})
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (s *stateSyncer) Done() <-chan error { return s.done }

// onSyncFailure writes all in-progress batches to disk to preserve maximum progress
func (s *stateSyncer) onSyncFailure(error) error {
	for _, storageTrieProgress := range s.progressMarker.StorageTries {
		if err := storageTrieProgress.batch.Write(); err != nil {
			return err
		}
	}
	return s.progressMarker.MainTrie.batch.Write()
}
