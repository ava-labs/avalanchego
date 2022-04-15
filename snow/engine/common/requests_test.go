// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/ids"
)

func TestRequests(t *testing.T) {
	req := Requests{}

	length := req.Len()
	assert.Equal(t, 0, length, "should have had no outstanding requests")

	_, removed := req.Remove(ids.ShortEmpty, 0)
	assert.False(t, removed, "shouldn't have removed the request")

	removed = req.RemoveAny(ids.Empty)
	assert.False(t, removed, "shouldn't have removed the request")

	constains := req.Contains(ids.Empty)
	assert.False(t, constains, "shouldn't contain this request")

	req.Add(ids.ShortEmpty, 0, ids.Empty)

	length = req.Len()
	assert.Equal(t, 1, length, "should have had one outstanding request")

	_, removed = req.Remove(ids.ShortEmpty, 1)
	assert.False(t, removed, "shouldn't have removed the request")

	_, removed = req.Remove(ids.ShortID{1}, 0)
	assert.False(t, removed, "shouldn't have removed the request")

	constains = req.Contains(ids.Empty)
	assert.True(t, constains, "should contain this request")

	length = req.Len()
	assert.Equal(t, 1, length, "should have had one outstanding request")

	req.Add(ids.ShortEmpty, 10, ids.Empty.Prefix(0))

	length = req.Len()
	assert.Equal(t, 2, length, "should have had two outstanding requests")

	_, removed = req.Remove(ids.ShortEmpty, 1)
	assert.False(t, removed, "shouldn't have removed the request")

	_, removed = req.Remove(ids.ShortID{1}, 0)
	assert.False(t, removed, "shouldn't have removed the request")

	constains = req.Contains(ids.Empty)
	assert.True(t, constains, "should contain this request")

	length = req.Len()
	assert.Equal(t, 2, length, "should have had two outstanding requests")

	removedID, removed := req.Remove(ids.ShortEmpty, 0)
	assert.Equal(t, ids.Empty, removedID, "should have removed the requested ID")
	assert.True(t, removed, "should have removed the request")

	removedID, removed = req.Remove(ids.ShortEmpty, 10)
	assert.Equal(t, ids.Empty.Prefix(0), removedID, "should have removed the requested ID")
	assert.True(t, removed, "should have removed the request")

	length = req.Len()
	assert.Equal(t, 0, length, "should have had no outstanding requests")

	req.Add(ids.ShortEmpty, 0, ids.Empty)

	length = req.Len()
	assert.Equal(t, 1, length, "should have had one outstanding request")

	removed = req.RemoveAny(ids.Empty)
	assert.True(t, removed, "should have removed the request")

	length = req.Len()
	assert.Equal(t, 0, length, "should have had no outstanding requests")

	removed = req.RemoveAny(ids.Empty)
	assert.False(t, removed, "shouldn't have removed the request")

	length = req.Len()
	assert.Equal(t, 0, length, "should have had no outstanding requests")
}
