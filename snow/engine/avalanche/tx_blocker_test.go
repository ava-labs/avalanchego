package avalanche

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/stretchr/testify/assert"
)

var (
	dep0 = ids.GenerateTestID()
	dep1 = ids.GenerateTestID()
	dep2 = ids.GenerateTestID()
)

func generatedBlocked() (*events.TestBlockable, *events.TestBlockable, *events.TestBlockable) {
	blocked0 := &events.TestBlockable{}
	blocked0.Default()
	blocked0.DependenciesF = func() ids.Set {
		blocked0.DependenciesCalled = true
		s := ids.Set{}
		s.Add(dep0, dep1)
		return s
	}

	blocked1 := &events.TestBlockable{}
	blocked1.Default()
	blocked1.DependenciesF = func() ids.Set {
		blocked1.DependenciesCalled = true
		s := ids.Set{}
		s.Add(dep0, dep1)
		return s
	}

	blocked2 := &events.TestBlockable{}
	blocked2.Default()
	blocked2.DependenciesF = func() ids.Set {
		blocked2.DependenciesCalled = true
		s := ids.Set{}
		s.Add(dep1, dep2)
		return s
	}
	return blocked0, blocked1, blocked2
}

// Test that txBlocker's register method works
func TestTxBlockerRegister(t *testing.T) {
	tb := txBlocker(nil)

	blocked0, blocked1, blocked2 := generatedBlocked()
	tb.register(blocked0, 0)
	assert.True(t, blocked0.DependenciesCalled)
	assert.NotNil(t, tb)
	assert.NotNil(t, tb[dep0])
	assert.Len(t, tb[dep0], 1)
	assert.NotNil(t, tb[dep0][0])
	assert.Len(t, tb[dep0][0], 1)
	assert.NotNil(t, tb[dep1])
	assert.Len(t, tb[dep1], 1)
	assert.Len(t, tb[dep1][0], 1)

	tb.register(blocked1, 0)
	assert.True(t, blocked1.DependenciesCalled)
	assert.NotNil(t, tb)
	assert.NotNil(t, tb[dep0])
	assert.Len(t, tb[dep0], 1)
	assert.Len(t, tb[dep0][0], 2)
	assert.NotNil(t, tb[dep1])
	assert.Len(t, tb[dep1], 1)
	assert.Len(t, tb[dep1][0], 2)

	tb.register(blocked2, 1)
	assert.True(t, blocked2.DependenciesCalled)
	assert.NotNil(t, tb)
	assert.NotNil(t, tb[dep0])
	assert.Len(t, tb[dep0], 1)
	assert.Len(t, tb[dep0][0], 2)
	assert.NotNil(t, tb[dep1])
	assert.Len(t, tb[dep1], 2)
	assert.Len(t, tb[dep1][0], 2)
	assert.Len(t, tb[dep1][1], 1)
	assert.NotNil(t, tb[dep2])
	assert.Len(t, tb[dep2][1], 1)
}

// Test that txBlocker's markIssued works in a simple case where
// there is one vertex that depends on 2 transactions
func TestTxBlockerMarkIssued(t *testing.T) {
	blocked0, blocked1, blocked2 := generatedBlocked()
	tb := txBlocker(nil)
	blocked01Epoch := uint32(10)
	blocked2Epoch := blocked01Epoch + 1

	tb.register(blocked0, blocked01Epoch)
	tb.register(blocked1, blocked01Epoch)
	tb.register(blocked2, blocked2Epoch)
	blocked0.DependenciesCalled = false
	blocked1.DependenciesCalled = false
	blocked2.DependenciesCalled = false

	// Mark that this transition was issued in an epoch later than any of the blockers
	// are blocked on
	tb.markIssued(dep0, blocked2Epoch+1)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.FulfillCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	}

	// Mark that dep0 was issued in an epoch earlier than any of the blockers
	// are blocked on
	tb.markIssued(dep0, blocked01Epoch-1)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.FulfillCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	}

	// Mark an unrelated transition as having been issued in the blocked01Epoch
	trID := ids.GenerateTestID()
	tb.markIssued(trID, blocked01Epoch)
	tb.markIssued(trID, blocked01Epoch)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.FulfillCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	}

	// Mark dep1 as having been issued in blocked01Epoch
	tb.markIssued(dep1, blocked01Epoch)
	switch {
	case !blocked0.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case !blocked1.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.UpdateCalled, blocked2.FulfillCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	}
	assert.NotNil(t, tb[dep1]) // blocked2 still depends on dep1
	assert.Len(t, tb[dep1][blocked2Epoch], 1)
	assert.Nil(t, tb[dep1][blocked01Epoch]) // Nobody depends on dep1 in blocked01Epoch anymore
	assert.Len(t, tb[dep0], 1)              // blocked0 and blocked1 still depend on dep0
	assert.Len(t, tb[dep0][blocked01Epoch], 2)
	assert.NotNil(t, tb[dep2])                // blocked2's deps are unaffected
	assert.Len(t, tb[dep2][blocked2Epoch], 1) // blocked2's deps are unaffected
	blocked0.FulfillCalled = false            // reset
	blocked1.FulfillCalled = false            // reset

	// Mark that dep0 was issued in blocked01Epoch
	tb.markIssued(dep0, blocked01Epoch)
	switch {
	case !blocked0.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case !blocked1.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.UpdateCalled, blocked2.FulfillCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	}
	assert.Nil(t, tb[dep0])    // Nobody depends on dep0 anymore
	assert.NotNil(t, tb[dep1]) // blocked2 still depends on dep1
	assert.Len(t, tb[dep1][blocked2Epoch], 1)
	assert.NotNil(t, tb[dep2])                // blocked2's deps are unaffected
	assert.Len(t, tb[dep2][blocked2Epoch], 1) // blocked2's deps are unaffected
	blocked0.FulfillCalled = false            // reset
	blocked1.FulfillCalled = false            // reset

	// Mark that dep0 was issued in blocked01Epoch again
	tb.markIssued(dep0, blocked01Epoch)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.FulfillCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	}

	// Now blocked0 and blocked1 are not blocked on anything
	// blocked2 is blocked on dep1 and dep2 in epoch blocked2Epoch

	// Mark that dep1 was issued in epoch blocked2Epoch
	tb.markIssued(dep1, blocked2Epoch)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case !blocked2.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	}
	assert.Len(t, tb, 1)                      // just dep2
	assert.Nil(t, tb[dep1])                   // nobody depends on dep1 anymore
	assert.NotNil(t, tb[dep2])                // other dependency is unaffected
	assert.NotNil(t, tb[dep2][blocked2Epoch]) // other dependency is unaffected
	blocked2.FulfillCalled = false            // reset

	// Mark that dep2 was issued in epoch blocked2Epoch
	tb.markIssued(dep2, blocked2Epoch)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case !blocked2.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	}
	assert.Len(t, tb, 0) // No more dependencies
}

// Test that txBlocker's markAccepted works
func TestTxBlockerMarkAccepted(t *testing.T) {
	blocked0, blocked1, blocked2 := generatedBlocked()
	tb := txBlocker(nil)
	blocked01Epoch := uint32(10)
	blocked2Epoch := blocked01Epoch + 1

	tb.register(blocked0, blocked01Epoch)
	tb.register(blocked1, blocked01Epoch)
	tb.register(blocked2, blocked2Epoch)
	blocked0.DependenciesCalled = false // reset
	blocked1.DependenciesCalled = false
	blocked2.DependenciesCalled = false

	// Mark an unrelated transition as accepted in blocked01Epoch
	tb.markAccepted(ids.GenerateTestID(), blocked01Epoch)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.FulfillCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.FulfillCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.FulfillCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	}

	// Mark dep0 as having been accepted in blocked01Epoch
	tb.markAccepted(dep0, blocked01Epoch)
	switch {
	case !blocked0.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case !blocked1.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.FulfillCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	}
	assert.Nil(t, tb[dep0]) // Nobody depends on dep0 anymore
	assert.NotNil(t, tb[dep1])
	assert.Len(t, tb[dep1], 2)     // blocked01Epoch and blocked2Epoch
	blocked0.FulfillCalled = false // reset
	blocked1.FulfillCalled = false // reset

	// Mark dep2 as having been accepted in blocked01Epoch
	tb.markAccepted(dep2, blocked01Epoch)
	switch {
	case blocked0.AbandonCalled, blocked0.DependenciesCalled, blocked0.UpdateCalled, blocked0.FulfillCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case blocked1.AbandonCalled, blocked1.DependenciesCalled, blocked1.UpdateCalled, blocked1.FulfillCalled:
		assert.FailNow(t, "shouldn't have called any methods")
	case !blocked2.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	}
	assert.Nil(t, tb[dep2]) // nobody depends on dep2 anymore
	assert.NotNil(t, tb[dep1])
	assert.Len(t, tb[dep1], 2)     // blocked01Epoch and blocked2Epoch
	blocked2.FulfillCalled = false // reset

	// Mark dep1 as having been accepted in blocked2Epoch
	tb.markAccepted(dep1, blocked2Epoch)
	switch {
	case !blocked0.AbandonCalled:
		assert.FailNow(t, "should have called abandon")
	case blocked0.DependenciesCalled, blocked0.UpdateCalled, blocked0.FulfillCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case !blocked1.AbandonCalled:
		assert.FailNow(t, "should have called abandon")
	case blocked1.DependenciesCalled, blocked1.UpdateCalled, blocked1.FulfillCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	case !blocked2.FulfillCalled:
		assert.FailNow(t, "should have called fulfill")
	case blocked2.AbandonCalled, blocked2.DependenciesCalled, blocked2.UpdateCalled:
		assert.FailNow(t, "shouldn't have called these methods")
	}
	assert.Len(t, tb, 0) // No outstanding dependencies
}

func TestTxBlockerAbandon(t *testing.T) {
	blocked0, blocked1, blocked2 := generatedBlocked()
	tb := txBlocker(nil)
	blocked01Epoch := uint32(10)
	blocked2Epoch := blocked01Epoch + 1

	tb.register(blocked0, blocked01Epoch)
	tb.register(blocked1, blocked01Epoch)
	tb.register(blocked2, blocked2Epoch)
	blocked0.DependenciesCalled = false // reset
	blocked1.DependenciesCalled = false
	blocked2.DependenciesCalled = false

	tb.abandon(dep2)
	assert.Nil(t, tb[dep2])
	assert.NotNil(t, tb[dep0])
	assert.NotNil(t, tb[dep1])

	tb.abandon(dep0)
	assert.Len(t, tb, 0)

	tb.abandon(dep0) // abandoning again shouldn't do anything
	assert.Len(t, tb, 0)

	tb.abandon(dep1)
	assert.Len(t, tb, 0)
}
