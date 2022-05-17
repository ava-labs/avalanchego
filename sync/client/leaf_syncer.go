// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/sync/errgroup"
)

var (
	errFailedToFetchLeafs = errors.New("failed to fetch leafs")
)

const defaultLeafRequestLimit = 1024

// OnStart is the callback used by LeafSyncTask to determine if work can be skipped.
// Returns true if work should be skipped (eg, because data was available on disk)
type OnStart func(root common.Hash) (bool, error)

// OnLeafs is the callback used by LeafSyncTask when there are new leaves received from the network.
// Returns a slice of LeafSyncTasks that will be added to the tasks of the LeafSyncer.
type OnLeafs func(root common.Hash, keys [][]byte, values [][]byte) ([]*LeafSyncTask, error)

// OnFinish is called the LeafSyncTask has completed syncing all of the leaves for the trie given by [root].
// OnFinish will be called after there are no more leaves in the trie to be synced.
type OnFinish func(root common.Hash) error

// OnSyncFailure is notified with the error that caused the sync to halt
// will never be called with nil, the returned error is simply logged
type OnSyncFailure func(error) error

// LeafSyncTask represents a complete task to be completed by the leaf syncer.
type LeafSyncTask struct {
	Root          common.Hash      // Root of the trie to sync
	Start         []byte           // Starting key to request new leaves
	NodeType      message.NodeType // Specifies the message type (atomic/state trie) for the leaf syncer to send
	OnStart       OnStart          // Callback when tasks begins, returns true if work can be skipped
	OnLeafs       OnLeafs          // Callback when new leaves are received from the network
	OnFinish      OnFinish         // Callback when there are no more leaves in the trie to sync
	OnSyncFailure OnSyncFailure    // Callback when the leaf syncer fails while performing the task.
}

type CallbackLeafSyncer struct {
	client LeafClient
	tasks  chan *LeafSyncTask
	done   chan error

	wg sync.WaitGroup
}

type LeafClient interface {
	GetLeafs(message.LeafsRequest) (message.LeafsResponse, error)
}

// NewCallbackLeafSyncer creates a new syncer object to perform leaf sync of tries.
func NewCallbackLeafSyncer(client LeafClient) *CallbackLeafSyncer {
	return &CallbackLeafSyncer{
		client: client,
		tasks:  make(chan *LeafSyncTask),
		done:   make(chan error),
	}
}

// workerLoop reads from [c.tasks] and calls [c.syncTask] until [ctx] is finished
// or [c.tasks] is closed.
func (c *CallbackLeafSyncer) workerLoop(ctx context.Context) error {
	for {
		select {
		case task, open := <-c.tasks:
			if !open {
				return nil
			}
			if err := c.syncTask(ctx, task); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// syncTask performs [task], requesting the leaves of the trie corresponding to [task.Root]
// starting at [task.Start] and invoking the callbacks as necessary.
func (c *CallbackLeafSyncer) syncTask(ctx context.Context, task *LeafSyncTask) error {
	defer c.wg.Done()

	var (
		root  = task.Root
		start = task.Start
	)

	if task.OnStart != nil {
		if skip, err := task.OnStart(root); err != nil {
			return err
		} else if skip {
			return nil
		}
	}

	for {
		// If [ctx] has finished, return early.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		leafsResponse, err := c.client.GetLeafs(message.LeafsRequest{
			Root:     root,
			Start:    start,
			End:      nil, // will request until the end of the trie
			Limit:    defaultLeafRequestLimit,
			NodeType: task.NodeType,
		})

		if err != nil {
			return fmt.Errorf("%s: %w", errFailedToFetchLeafs, err)
		}

		if tasks, err := task.OnLeafs(task.Root, leafsResponse.Keys, leafsResponse.Vals); err != nil {
			return err
		} else if len(tasks) > 0 {
			// add [tasks] to [c.tasks] and propagate the error
			// from [ctx] if it finishes
			if err := c.addTasks(ctx, tasks); err != nil {
				return err
			}
		}

		// If we have completed syncing this trie, invoke [onFinish] and mark the task
		// as complete.
		if !leafsResponse.More {
			return task.OnFinish(root)
		}

		if len(leafsResponse.Keys) == 0 {
			return fmt.Errorf("found no keys in a response with more set to true")
		}
		// Update start to be one bit past the last returned key for the next request.
		// Note: since more was true, this cannot cause an overflow.
		start = leafsResponse.Keys[len(leafsResponse.Keys)-1]
		IncrOne(start)
	}
}

// Start begins syncing the leaves of the tries corresponding to [task] and [subtasks].
//
// Note: a maximum of [numThreads] tasks can be executed at once, such that
// if all of the actively syncing tasks call addTask while the task buffer is full,
// there may be a deadlock. For the atomic trie, this should never happen since there is
// only a single root task and no subtasks.
// For the EVM state trie, subtasks are started before the main task. This ensures the number
// of in progress tasks stays less than or equal to [numThreads]. These subtasks will not
// add subtasks, since they have no sub-tries. Therefore, an actively syncing storage
// trie task never blocks.
// Once the number of tasks in progress is below [numThreads], the main task begins. This task
// adds more subtasks as storage roots are encountered during the sync. This will block when
// there are [numThreads-1] storage tries being synced by worker threads.
func (c *CallbackLeafSyncer) Start(ctx context.Context, numThreads int, task *LeafSyncTask, subtasks ...*LeafSyncTask) {
	// Start the worker threads with the desired context.
	eg, egCtx := errgroup.WithContext(ctx)
	for i := 0; i < numThreads; i++ {
		eg.Go(func() error {
			return c.workerLoop(egCtx)
		})
	}

	tasks := make([]*LeafSyncTask, 0, len(subtasks)+1)
	tasks = append(tasks, subtasks...) // add [subtasks] first, they are not allowed to add additional tasks
	tasks = append(tasks, task)        // [task] can add tasks. Start it last to keep number of in-progress tries <= [numThreads]

	// Start a goroutine to pass the given tasks in. This ensures we do not
	// block here if there are more tasks than worker threads.
	eg.Go(func() error {
		err := c.addTasks(egCtx, tasks)
		c.wg.Wait() // wait for all tasks added to [c.tasks] to finish
		close(c.tasks)
		return err
	})

	go func() {
		err := eg.Wait()
		if err != nil {
			if err := task.OnSyncFailure(err); err != nil {
				log.Error("error handling sync failure callback", "err", err)
			}
		}
		c.done <- err
		close(c.done)
	}()
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (c *CallbackLeafSyncer) Done() <-chan error { return c.done }

// addTasks adds [tasks] to [c.tasks] and returns nil if all tasks were added.
// if [ctx] finishes before all tasks are added, the error from [ctx] is returned.
func (c *CallbackLeafSyncer) addTasks(ctx context.Context, tasks []*LeafSyncTask) error {
	// Invariant: we add the number of tasks to [c.wg] before adding
	// the task [c.tasks].
	// We mark tasks as completed when syncTask returns, or if [ctx]
	// finishes before the task was added to [c.tasks]
	c.wg.Add(len(tasks))
	for i, task := range tasks {
		select {
		case c.tasks <- task:
		case <-ctx.Done():
			// clear tasks that were not scheduled from [c.wg]
			for j := i; j < len(tasks); j++ {
				c.wg.Done()
			}
			return ctx.Err()
		}
	}
	return nil
}

// IncrOne increments bytes value by one
func IncrOne(bytes []byte) {
	index := len(bytes) - 1
	for index >= 0 {
		if bytes[index] < 255 {
			bytes[index]++
			break
		} else {
			bytes[index] = 0
			index--
		}
	}
}
