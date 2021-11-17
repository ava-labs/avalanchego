// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"reflect"
	"testing"
)

func TestQueueSetinit(t *testing.T) {
	qs := QueueSet{}
	qs.init()
	if qs.idList == nil {
		t.Fatal("Failed to initialize")
	}
	list := qs.idList
	qs.init()
	if list != qs.idList {
		t.Fatal("Mutated an already intialized queue")
	}
}

func TestQueueSetSetHead(t *testing.T) {
	qs := QueueSet{}
	id := ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	qs.SetHead(id)
	if qs.idList == nil || id != qs.idList.Front().Value.(ID) {
		t.Fatal("Failed to set head of unintilised queue")
	}

	qs.SetHead(id)
	if qs.idList.Len() != 1 || id != qs.idList.Front().Value.(ID) {
		t.Fatal("Mutated a queue which already had the desired head")
	}

	id2 := ID{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	qs.SetHead(id2)
	if qs.idList.Len() != 1 || id2 != qs.idList.Front().Value.(ID) {
		t.Fatal("Didn't replace the existing head")
	}
}

func TestQueueSetAppend(t *testing.T) {
	qs := QueueSet{}
	id := ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	qs.Append(id)
	if qs.idList == nil || id != qs.idList.Front().Value.(ID) {
		t.Fatal("Failed to append to an uninitialised queue")
	}

	id2 := ID{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	qs.Append(id2)
	if qs.idList.Len() != 2 || id2 != qs.idList.Back().Value.(ID) {
		t.Fatal("Failed to append to the back of the queue")
	}
}

func TestQueueGetTail(t *testing.T) {
	qs := QueueSet{}
	tail := qs.GetTail()
	if !reflect.DeepEqual(tail, ID{}) {
		t.Fatalf("Empty queue returned %v, expected empty ID %v", tail, Empty)
	}

	qs.Append(ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'})
	id2 := ID{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	qs.Append(id2)
	tail = qs.GetTail()
	if tail != id2 {
		t.Fatalf("Populated queue returned %v, expected %v", tail, id2)
	}
}
