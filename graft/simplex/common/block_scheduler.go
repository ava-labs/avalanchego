// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

var ErrTooManyPendingVerifications = errors.New("too many blocks being verified to ingest another one")

type Scheduler interface {
	Schedule(task Task) bool
	Size() int
	Close()
}

// BlockDependencyManager manages block verification tasks with dependencies on previous blocks and empty rounds.
// It schedules tasks when their dependencies are resolved.
type BlockDependencyManager struct {
	lock      sync.Mutex
	logger    Logger
	scheduler Scheduler

	dependencies []*TaskWithDependents
	// scheduledSeqs holds every sequence and epoch pair with a task pending, queued or running.
	scheduledSeqs map[seqEpoch]struct{}
	maxDeps       uint64
	closed        atomic.Bool
}

type seqEpoch struct {
	seq   uint64
	epoch uint64
}

type TaskWithDependents struct {
	Task Task

	blockSeq    uint64 // the seq of the block being verified
	epoch       uint64 // the epoch of the block being verified
	prevBlock   *Digest
	emptyRounds map[uint64]struct{}
}

func (t *TaskWithDependents) isReady() bool {
	return t.prevBlock == nil && len(t.emptyRounds) == 0
}

func (t *TaskWithDependents) String() string {
	return fmt.Sprintf("BlockVerificationTask{blockSeq: %d, prevBlock: %v, emptyRounds: %v}", t.blockSeq, t.prevBlock, slices.Collect(maps.Keys(t.emptyRounds)))
}

func NewBlockVerificationScheduler(logger Logger, maxDeps uint64, scheduler Scheduler) *BlockDependencyManager {
	b := &BlockDependencyManager{
		logger:        logger,
		maxDeps:       maxDeps,
		scheduler:     scheduler,
		scheduledSeqs: make(map[seqEpoch]struct{}),
	}

	b.logger.Debug("Created BlockVerificationScheduler", zap.Uint64("maxDeps", maxDeps))
	return b
}

// ExecuteBlockDependents removes the given digest from dependent tasks and schedules any whose dependencies are now resolved.
func (bs *BlockDependencyManager) ExecuteBlockDependents(prev Digest) {
	bs.lock.Lock()
	defer bs.lock.Unlock()

	remainingDeps := make([]*TaskWithDependents, 0, len(bs.dependencies))

	for _, taskWithDeps := range bs.dependencies {
		if taskWithDeps.prevBlock != nil && *taskWithDeps.prevBlock == prev {
			taskWithDeps.prevBlock = nil
		}

		if taskWithDeps.isReady() {
			bs.logger.Debug("Scheduling block verification task as all dependencies are met", zap.Stringer("taskID", prev), zap.Uint64("Seq", taskWithDeps.blockSeq))
			if !bs.scheduler.Schedule(taskWithDeps.Task) {
				delete(bs.scheduledSeqs, seqEpoch{seq: taskWithDeps.blockSeq, epoch: taskWithDeps.epoch})
			}
			continue
		}

		remainingDeps = append(remainingDeps, taskWithDeps)
	}

	if len(remainingDeps) > 0 {
		bs.logger.Debug("Block scheduler has unsatisfied dependencies",
			zap.Stringer("prev", prev),
			zap.Stringers("task", remainingDeps),
		)
	}

	bs.dependencies = remainingDeps
}

// ExecuteEmptyRoundDependents removes the given empty round from dependent tasks and schedules any whose dependencies are now resolved.
func (bs *BlockDependencyManager) ExecuteEmptyRoundDependents(emptyRound uint64) {
	bs.lock.Lock()
	defer bs.lock.Unlock()

	remainingDeps := make([]*TaskWithDependents, 0, len(bs.dependencies))

	for _, taskWithDeps := range bs.dependencies {
		delete(taskWithDeps.emptyRounds, emptyRound)

		if taskWithDeps.isReady() {
			bs.logger.Debug("Scheduling block verification task as all dependencies are met", zap.Stringer("task", taskWithDeps))
			if !bs.scheduler.Schedule(taskWithDeps.Task) {
				delete(bs.scheduledSeqs, seqEpoch{seq: taskWithDeps.blockSeq, epoch: taskWithDeps.epoch})
			}
			continue
		}

		remainingDeps = append(remainingDeps, taskWithDeps)
	}

	if len(remainingDeps) > 0 {
		bs.logger.Debug("Block scheduler has unsatisfied dependencies",
			zap.Uint64("empty round", emptyRound),
			zap.Stringers("task", remainingDeps),
		)
	}

	bs.dependencies = remainingDeps
}

// IsSequenceScheduled reports whether a task for seq is pending on dependencies, queued or running.
func (bs *BlockDependencyManager) IsSequenceScheduled(seq, epoch uint64) bool {
	bs.lock.Lock()
	defer bs.lock.Unlock()

	_, ok := bs.scheduledSeqs[seqEpoch{seq: seq, epoch: epoch}]
	return ok
}

func (bs *BlockDependencyManager) ScheduleTaskWithDependencies(task Task, blockSeq uint64, epoch uint64, prev *Digest, emptyRounds []uint64) error {
	bs.lock.Lock()
	defer bs.lock.Unlock()

	if bs.closed.Load() {
		return nil
	}

	key := seqEpoch{seq: blockSeq, epoch: epoch}

	wrappedTask := func() Digest {
		id := task()
		bs.lock.Lock()
		delete(bs.scheduledSeqs, key)
		bs.lock.Unlock()
		bs.ExecuteBlockDependents(id)
		return id
	}

	totalSize := uint64(len(bs.dependencies) + bs.scheduler.Size())
	if totalSize >= bs.maxDeps {
		bs.logger.Warn("Too many blocks being verified to ingest another one", zap.Uint64("pendingBlocks", totalSize))
		return fmt.Errorf("%w: %d pending verifications (max %d)", ErrTooManyPendingVerifications, totalSize, bs.maxDeps)
	}

	bs.scheduledSeqs[key] = struct{}{}

	if prev == nil && len(emptyRounds) == 0 {
		bs.logger.Debug("Scheduling block verification task with no dependencies", zap.Uint64("blockSeq", blockSeq))
		if !bs.scheduler.Schedule(wrappedTask) {
			delete(bs.scheduledSeqs, key)
		}
		return nil
	}

	bs.logger.Debug("Adding block verification task with dependencies", zap.Any("prevBlock", prev), zap.Uint64s("emptyRounds", emptyRounds), zap.Uint64("blockSeq", blockSeq))
	emptyRoundsSet := make(map[uint64]struct{})
	for _, round := range emptyRounds {
		emptyRoundsSet[round] = struct{}{}
	}

	bs.dependencies = append(bs.dependencies, &TaskWithDependents{
		Task:        wrappedTask,
		prevBlock:   prev,
		emptyRounds: emptyRoundsSet,
		blockSeq:    blockSeq,
		epoch:       epoch,
	})

	return nil
}

// We can remove all tasks that have an empty notarization dependency for a round that has been finalized.
func (bs *BlockDependencyManager) RemoveOldTasks(seq uint64) {
	bs.lock.Lock()
	defer bs.lock.Unlock()

	remainingDeps := make([]*TaskWithDependents, 0, len(bs.dependencies))
	for _, taskWithDeps := range bs.dependencies {
		if taskWithDeps.blockSeq <= seq {
			bs.logger.Debug("Removing block verification task as its block seq is less than or equal to finalized seq", zap.Uint64("blockSeq", taskWithDeps.blockSeq), zap.Uint64("finalizedSeq", seq))
			key := seqEpoch{seq: taskWithDeps.blockSeq, epoch: taskWithDeps.epoch}
			delete(bs.scheduledSeqs, key)
			continue
		}
		remainingDeps = append(remainingDeps, taskWithDeps)
	}

	bs.dependencies = remainingDeps
}

func (bs *BlockDependencyManager) Close() {
	bs.closed.Store(true)
	bs.scheduler.Close()
}
