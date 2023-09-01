// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type changes struct {
	key      []byte
	value    []byte
	isDelete bool
}

func entry(key string, value string) changes {
	return changes{[]byte(key), []byte(value), false}
}

func delete(key string) changes {
	return changes{[]byte(key), []byte{}, true}
}

func store(t *testing.T, db *archiveDB, changes []changes) uint64 {
	writer, err := db.NewBatch()
	height := writer.Height()
	require.NoError(t, err)
	for _, change := range changes {
		if change.isDelete {
			require.NoError(t, writer.Delete(change.key))
		} else {
			require.NoError(t, writer.Put(change.key, change.value))
		}
	}
	require.NoError(t, writer.Write())
	return height
}

type keyResultSet struct {
	key        []byte
	lastSeenAt uint64
}

func consumeAllKeys(t *testing.T, i keysIterator) []keyResultSet {
	defer i.Release()
	results := []keyResultSet{}
	for i.Next() {
		results = append(results, keyResultSet{i.Key(), i.Value()})
	}
	require.NoError(t, i.Error())

	return results
}

func getDBWithState(t *testing.T, state [][]changes) *archiveDB {
	db, err := getBasicDB()
	require.NoError(t, err)
	height := uint64(1)

	for _, changes := range state {
		require.Equal(t, height, store(t, db, changes))
		height += 1
	}

	return db
}

func isPrime(num int) bool {
	if num < 2 {
		return false
	}
	sqroot := int(math.Sqrt(float64(num)))
	for i := 2; i <= sqroot; i++ {
		if num%i == 0 {
			return false
		}
	}
	return true
}

func getDBWithDefaultState(t *testing.T) *archiveDB {
	var allChanges [][]changes
	for i := 1; i <= 10000; i++ {
		var block []changes
		value := fmt.Sprintf("value of %d", i)
		if isPrime(i) {
			block = append(block, entry(fmt.Sprintf("prime:%d", i), value))
			block = append(block, entry("last:prime", value))
		}
		if i%2 == 0 {
			block = append(block, entry(fmt.Sprintf("even:%d", i), value))
			block = append(block, entry("last:even", value))
		} else {
			block = append(block, entry(fmt.Sprintf("odd:%d", i), value))
			block = append(block, entry("last:odd", value))
		}
		allChanges = append(allChanges, block)
	}
	return getDBWithState(t, allChanges)
}

func TestGetAllKeysByPrefix(t *testing.T) {
	db := getDBWithState(t, [][]changes{
		{},
		{
			entry("prefix:key1", "value1@1"),
			entry("prefix:key2", "value2@1"),
		},
		{
			entry("prefix:key1", "value1@10"),
			entry("prefix:key2", "value2@10"),
		},
		{},
		{
			entry("prefix:key1", "value1@100"),
		},
		{},
		{
			entry("prefix:key1", "value1@1000"),
			entry("prefix:key2", "value2@1000"),
		},
		{
			entry("prefix:ac", "value1@1000"),
			entry("suffix:ac", "value2@1000"),
		},
	})
	require.Equal(t, []keyResultSet{
		{[]byte("prefix:ac"), 8},
		{[]byte("prefix:key1"), 7},
		{[]byte("prefix:key2"), 7},
	}, consumeAllKeys(t, db.GetKeysByPrefix([]byte("prefix:"))))

	require.Equal(t, []keyResultSet{
		{[]byte("suffix:ac"), 8},
	}, consumeAllKeys(t, db.GetKeysByPrefix([]byte("s"))))
}

func TestGetAllKeysByPrefixBenchmark(t *testing.T) {
	db := getDBWithDefaultState(t)
	allPrimes := consumeAllKeys(t, db.GetKeysByPrefix([]byte("prime:")))
	require.Len(t, allPrimes, 1229)
	require.Equal(t, []byte("prime:1009"), allPrimes[0].key)
	lastVars := consumeAllKeys(t, db.GetKeysByPrefix([]byte("last:")))
	require.Len(t, lastVars, 3)
	require.Equal(t, []byte("last:even"), lastVars[0].key)
	require.Equal(t, []byte("last:odd"), lastVars[1].key)
	require.Equal(t, []byte("last:prime"), lastVars[2].key)
}
