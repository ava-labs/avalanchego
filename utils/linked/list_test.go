// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linked

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func flattenForwards[T any](l *List[T]) []T {
	var s []T
	for e := l.Front(); e != nil; e = e.Next() {
		s = append(s, e.Value)
	}
	return s
}

func flattenBackwards[T any](l *List[T]) []T {
	var s []T
	for e := l.Back(); e != nil; e = e.Prev() {
		s = append(s, e.Value)
	}
	return s
}

func TestList_Empty(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	require.Empty(flattenForwards(l))
	require.Empty(flattenBackwards(l))
	require.Zero(l.Len())
}

func TestList_PushBack(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	for i := 0; i < 5; i++ {
		l.PushBack(&ListElement[int]{
			Value: i,
		})
	}

	require.Equal([]int{0, 1, 2, 3, 4}, flattenForwards(l))
	require.Equal([]int{4, 3, 2, 1, 0}, flattenBackwards(l))
	require.Equal(5, l.Len())
}

func TestList_PushBack_Duplicate(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	e := &ListElement[int]{
		Value: 0,
	}
	l.PushBack(e)
	l.PushBack(e)

	require.Equal([]int{0}, flattenForwards(l))
	require.Equal([]int{0}, flattenBackwards(l))
	require.Equal(1, l.Len())
}

func TestList_PushFront(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	for i := 0; i < 5; i++ {
		l.PushFront(&ListElement[int]{
			Value: i,
		})
	}

	require.Equal([]int{4, 3, 2, 1, 0}, flattenForwards(l))
	require.Equal([]int{0, 1, 2, 3, 4}, flattenBackwards(l))
	require.Equal(5, l.Len())
}

func TestList_PushFront_Duplicate(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	e := &ListElement[int]{
		Value: 0,
	}
	l.PushFront(e)
	l.PushFront(e)

	require.Equal([]int{0}, flattenForwards(l))
	require.Equal([]int{0}, flattenBackwards(l))
	require.Equal(1, l.Len())
}

func TestList_Remove(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	e0 := &ListElement[int]{
		Value: 0,
	}
	e1 := &ListElement[int]{
		Value: 1,
	}
	e2 := &ListElement[int]{
		Value: 2,
	}
	l.PushBack(e0)
	l.PushBack(e1)
	l.PushBack(e2)

	l.Remove(e1)

	require.Equal([]int{0, 2}, flattenForwards(l))
	require.Equal([]int{2, 0}, flattenBackwards(l))
	require.Equal(2, l.Len())
	require.Nil(e1.next)
	require.Nil(e1.prev)
	require.Nil(e1.list)
}

func TestList_MoveToFront(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	e0 := &ListElement[int]{
		Value: 0,
	}
	e1 := &ListElement[int]{
		Value: 1,
	}
	l.PushFront(e0)
	l.PushFront(e1)
	l.MoveToFront(e0)

	require.Equal([]int{0, 1}, flattenForwards(l))
	require.Equal([]int{1, 0}, flattenBackwards(l))
	require.Equal(2, l.Len())
}

func TestList_MoveToBack(t *testing.T) {
	require := require.New(t)

	l := NewList[int]()

	e0 := &ListElement[int]{
		Value: 0,
	}
	e1 := &ListElement[int]{
		Value: 1,
	}
	l.PushFront(e0)
	l.PushFront(e1)
	l.MoveToBack(e1)

	require.Equal([]int{0, 1}, flattenForwards(l))
	require.Equal([]int{1, 0}, flattenBackwards(l))
	require.Equal(2, l.Len())
}
