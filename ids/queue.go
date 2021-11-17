// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"container/list"
)

// QueueSet is a set of IDs stored in fifo order
type QueueSet struct {
	idList *list.List
}

func (qs *QueueSet) init() {
	if qs.idList == nil {
		qs.idList = list.New()
	}
}

func (qs *QueueSet) SetHead(id ID) {
	qs.init()

	for qs.idList.Len() > 0 {
		element := qs.idList.Front()
		head := element.Value.(ID)
		if head == id {
			return
		}
		qs.idList.Remove(element)
	}

	qs.idList.PushFront(id)
}

func (qs *QueueSet) Append(id ID) {
	qs.init()

	qs.idList.PushBack(id)
}

func (qs *QueueSet) GetTail() ID {
	qs.init()

	if qs.idList.Len() == 0 {
		return ID{}
	}
	return qs.idList.Back().Value.(ID)
}
