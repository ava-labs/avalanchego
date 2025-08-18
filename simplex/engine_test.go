package simplex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSimplexEngineStart(t *testing.T) {
	// start an engine from genesis
	config := newEngineConfig(t, 1)
	engine, err := NewEngine(context.TODO(), config)
	require.NoError(t, err)

	require.NoError(t, engine.Start(context.Background(), 1))
}
func TestSimplexEngineStartFromBlock(t *testing.T) {
	// start an engine from a block
}
