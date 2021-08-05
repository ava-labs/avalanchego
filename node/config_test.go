package node

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestVMAliasesMarshalJSON(t *testing.T) {
	assert := assert.New(t)
	{
		var v VMAliases
		bytes, err := v.MarshalJSON()
		assert.NoError(err)
		assert.EqualValues("{}", string(bytes))
	}
	{
		v := make(VMAliases)
		id1 := ids.GenerateTestID()
		v[id1] = []string{"foo"}
		bytes, err := v.MarshalJSON()
		assert.NoError(err)
		expected := fmt.Sprintf("{\"%s\": [\"foo\"]}", id1)
		assert.EqualValues(expected, string(bytes))
	}
	{
		v := make(VMAliases)
		id1 := ids.GenerateTestID()
		v[id1] = []string{"foo", "bar"}
		bytes, err := v.MarshalJSON()
		assert.NoError(err)
		expected := fmt.Sprintf("{\"%s\": [\"foo\",\"bar\"]}", id1)
		assert.EqualValues(expected, string(bytes))
	}
	{
		v := make(VMAliases)
		id1 := ids.GenerateTestID()
		id2 := ids.GenerateTestID()
		v[id1] = []string{"foo", "bar"}
		v[id2] = []string{"foo2", "bar2"}
		bytes, err := v.MarshalJSON()
		assert.NoError(err)
		expected := fmt.Sprintf("{\"%s\": [\"foo\",\"bar\"],\"%s\": [\"foo2\",\"bar2\"]}", id1, id2)
		assert.EqualValues(expected, string(bytes))
	}
}
