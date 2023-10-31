// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reflectcodec

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSizeWithNil(t *testing.T) {
	require := require.New(t)
	var x *int32
	y := int32(1)
	c := genericCodec{}
	_, _, err := c.size(reflect.ValueOf(x), false /*=nullable*/, nil /*=typeStack*/)
	require.ErrorIs(err, errMarshalNil)
	len, _, err := c.size(reflect.ValueOf(x), true /*=nullable*/, nil /*=typeStack*/)
	require.Empty(err)
	require.Equal(1, len)
	x = &y
	len, _, err = c.size(reflect.ValueOf(y), true /*=nullable*/, nil /*=typeStack*/)
	require.Empty(err)
	require.Equal(4, len)
	len, _, err = c.size(reflect.ValueOf(x), true /*=nullable*/, nil /*=typeStack*/)
	require.Empty(err)
	require.Equal(5, len)
}
