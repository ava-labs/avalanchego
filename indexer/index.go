package indexer

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// Maximum number of transaction IDs that can be fetched at a time
	maxFetchedByRange = 1024
)

var (
	// Maps to the byte representation of the next accepted index
	nextAcceptedIndexKey   []byte = []byte("next")
	indexToContainerPrefix []byte = []byte("itc")
	containerToIDPrefix    []byte = []byte("cti")
	errNoneAccepted               = errors.New("no containers have been accepted")

	_ Index = &index{}
)

// Index indexes container (a blob of bytes with an ID) in their order of acceptance
// Index implements triggers.Acceptor
// Index is thread-safe
type Index interface {
	Accept(ctx *snow.Context, containerID ids.ID, container []byte) error
	GetContainerByIndex(index uint64) (Container, error)
	GetContainerRange(startIndex uint64, numToFetch uint64) ([]Container, error)
	GetLastAccepted() (Container, error)
	GetIndex(containerID ids.ID) (uint64, error)
	GetContainerByID(containerID ids.ID) (Container, error)
	Close() error
}

// Returns a new, thread-safe Index
func newIndex(
	db database.Database,
	log logging.Logger,
	codec codec.Manager,
	clock timer.Clock,
) (Index, error) {
	baseDb := versiondb.New(db)
	indexToContainer := prefixdb.New(indexToContainerPrefix, baseDb)
	containerToIndex := prefixdb.New(containerToIDPrefix, baseDb)

	i := &index{
		lock:             &sync.RWMutex{},
		clock:            clock,
		codec:            codec,
		baseDb:           baseDb,
		indexToContainer: indexToContainer,
		containerToIndex: containerToIndex,
		log:              log,
	}

	// Get next accepted index from db
	nextAcceptedIndexBytes, err := i.baseDb.Get(nextAcceptedIndexKey)
	if err == database.ErrNotFound {
		// Couldn't find it in the database. Must not have accepted any containers in previous runs.
		i.log.Info("next accepted index %d", i.nextAcceptedIndex)
		return i, nil
	} else if err != nil {
		return nil, fmt.Errorf("couldn't get next accepted index from database: %w", err)
	}
	p := wrappers.Packer{Bytes: nextAcceptedIndexBytes}
	i.nextAcceptedIndex = p.UnpackLong()
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't parse next accepted index from bytes: %w", err)
	}
	i.log.Info("next accepted index %d", i.nextAcceptedIndex)
	return i, nil
}

// indexer indexes all accepted transactions by the order in which they were accepted
type index struct {
	codec codec.Manager
	clock timer.Clock
	lock  *sync.RWMutex
	// The index of the next accepted transaction
	nextAcceptedIndex uint64
	// When [baseDb] is committed, actual write to disk happens
	baseDb *versiondb.Database
	// Both [indexToContainer] and [containerToIndex] have [baseDb] underneath
	// Index --> Container
	indexToContainer database.Database
	// Container ID --> Index
	containerToIndex database.Database
	log              logging.Logger
}

// Close this index
func (i *index) Close() error {
	errs := wrappers.Errs{}
	if i.indexToContainer != nil {
		errs.Add(i.indexToContainer.Close())
	}
	if i.containerToIndex != nil {
		errs.Add(i.containerToIndex.Close())
	}
	if i.baseDb != nil {
		errs.Add(i.baseDb.Close())
	}
	return errs.Err
}

// Index that the given transaction is accepted
// Returned error should be treated as fatal
func (i *index) Accept(ctx *snow.Context, containerID ids.ID, containerBytes []byte) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	ctx.Log.Error("indexing %d --> %s", i.nextAcceptedIndex, containerID)
	// Persist index --> Container
	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(i.nextAcceptedIndex)
	if p.Err != nil {
		return fmt.Errorf("couldn't convert next accepted index to bytes: %w", p.Err)
	}
	bytes, err := i.codec.Marshal(codecVersion, Container{
		Index:     i.nextAcceptedIndex,
		Bytes:     containerBytes,
		ID:        containerID,
		Timestamp: uint64(i.clock.Time().Unix()),
	})
	if err != nil {
		return fmt.Errorf("couldn't serialize container %s: %w", containerID, err)
	}
	err = i.indexToContainer.Put(p.Bytes, bytes)
	if err != nil {
		return fmt.Errorf("couldn't put accepted container %s into index: %w", containerID, err)
	}

	// Persist container ID --> index
	err = i.containerToIndex.Put(containerID[:], p.Bytes)
	if err != nil {
		return fmt.Errorf("couldn't map container %s to index: %w", containerID, err)
	}

	// Persist next accepted index
	i.nextAcceptedIndex++
	p = wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(i.nextAcceptedIndex)
	if p.Err != nil {
		return fmt.Errorf("couldn't convert next accepted index to bytes: %w", p.Err)
	}
	err = i.baseDb.Put(nextAcceptedIndexKey, p.Bytes)
	if err != nil {
		return fmt.Errorf("couldn't put accepted container %s into index: %w", containerID, err)
	}

	return i.baseDb.Commit()
}

// Returns the ID of the [index]th accepted container and the container itself.
// For example, if [index] == 0, returns the first accepted container.
// If [index] == 1, returns the second accepted container, etc.
func (i *index) GetContainerByIndex(index uint64) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	lastAcceptedIndex, ok := i.lastAcceptedIndex()
	if !ok || index > lastAcceptedIndex {
		return Container{}, fmt.Errorf("no container at index %d", index)
	}

	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(index)
	if p.Err != nil {
		return Container{}, fmt.Errorf("couldn't convert index to bytes: %w", p.Err)
	}

	containerBytes, err := i.indexToContainer.Get(p.Bytes)
	switch {
	case err == database.ErrNotFound:
		return Container{}, fmt.Errorf("no container at index %d", index)
	case err != nil:
		i.log.Error("couldn't read container from database: %w", err)
		return Container{}, fmt.Errorf("couldn't read from database: %w", err)
	}

	var container Container
	_, err = i.codec.Unmarshal(containerBytes, &container)
	if err != nil {
		return Container{}, fmt.Errorf("couldn't unmarshal container: %w", err)
	}
	return container, nil
}

/// GetContainerRange returns the IDs of containers at index
// [startIndex], [startIndex+1], ..., [startIndex+numToFetch-1]
func (i *index) GetContainerRange(startIndex, numToFetch uint64) ([]Container, error) {
	i.log.Error("%d %d", startIndex, numToFetch) // TODO remove
	// Check arguments for validity
	if numToFetch == 0 {
		return nil, nil
	} else if numToFetch > maxFetchedByRange {
		return nil, fmt.Errorf("requested %d but maximum page size is %d", numToFetch, maxFetchedByRange)
	}
	i.lock.RLock()
	defer i.lock.RUnlock()

	lastAcceptedIndex, ok := i.lastAcceptedIndex()
	if !ok {
		return nil, fmt.Errorf("start index (%d) > last accepted index (-1)", startIndex)
	} else if startIndex > lastAcceptedIndex {
		return nil, fmt.Errorf("start index (%d) > last accepted index (%d)", startIndex, lastAcceptedIndex)
	}

	// Convert start index to bytes
	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(startIndex)
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't convert start index to bytes: %w", p.Err)
	}

	// Calculate the size of the needed slice
	lastIndex := math.Min64(startIndex+numToFetch, lastAcceptedIndex)
	// [lastIndex] is always >= [startIndex] so this is safe.
	// [n] is limited to [maxFetchedByRange] so [containerIDs] can't be crazy big.
	containers := make([]Container, 0, int(lastIndex)-int(startIndex)+1)

	iter := i.indexToContainer.NewIteratorWithStart(p.Bytes)
	defer iter.Release()

	for len(containers) < int(numToFetch) && iter.Next() {
		containerBytes := iter.Value()
		var container Container
		_, err := i.codec.Unmarshal(containerBytes, &container)
		if err != nil {
			return nil, fmt.Errorf("couldn't unmarshal container: %w", err)
		}
		containers = append(containers, container)
	}
	return containers, nil
}

func (i *index) GetIndex(containerID ids.ID) (uint64, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	indexBytes, err := i.containerToIndex.Get(containerID[:])
	if err != nil {
		return 0, err
	}

	p := wrappers.Packer{Bytes: indexBytes}
	index := p.UnpackLong()
	if p.Err != nil {
		// Should never happen
		i.log.Error("couldn't unpack index: %w", err)
		return 0, p.Err
	}
	return index, nil
}

func (i *index) GetContainerByID(containerID ids.ID) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	// Read index from database
	indexBytes, err := i.containerToIndex.Get(containerID[:])
	if err != nil {
		return Container{}, err
	}

	// Read container from database
	containerBytes, err := i.indexToContainer.Get(indexBytes)
	if err != nil {
		err = fmt.Errorf("couldn't read container from database: %w", err)
		i.log.Error("%s", err)
		return Container{}, err
	}

	// Parse container
	var container Container
	_, err = i.codec.Unmarshal(containerBytes, &container)
	if err != nil {
		return Container{}, fmt.Errorf("couldn't unmarshal container: %w", err)
	}
	return container, nil
}

// GetLastAccepted returns the last accepted container
// Returns an error if no containers have been accepted
func (i *index) GetLastAccepted() (Container, error) {
	lastAcceptedIndex, exists := i.lastAcceptedIndex()
	if !exists {
		return Container{}, errNoneAccepted
	}

	return i.GetContainerByIndex(lastAcceptedIndex)
}

// Returns:
// 1) The index of the most recently accepted transaction,
//    or 0 if no transactions have been accepted
// 2) Whether at least 1 transaction has been accepted
func (i *index) lastAcceptedIndex() (uint64, bool) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	if i.nextAcceptedIndex == 0 {
		return 0, false
	}
	return i.nextAcceptedIndex - 1, true
}
