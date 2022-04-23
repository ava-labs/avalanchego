package rpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPasswordStripped(t *testing.T) {
	url := "https://user:password@my.avalanche.api.com/ext/P"
	stipped := stripPassword(url)
	assert.Equal(t, "https://user:***@my.avalanche.api.com/ext/P", stipped)
}
