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
	blocked01Epoch := uint32(10)
	blocked2Epoch := blocked01Epoch + 1

	tb.register(blocked0, blocked01Epoch)
	assert.True(t, blocked0.DependenciesCalled)
	assert.NotNil(t, tb)
	assert.NotNil(t, tb[blocked01Epoch])
	assert.Len(t, tb[blocked01Epoch], 2) // dep0, dep1
	assert.NotNil(t, tb[blocked01Epoch][dep0])
	assert.Len(t, tb[blocked01Epoch][dep0], 1)
	assert.NotNil(t, tb[blocked01Epoch][dep1])
	assert.Len(t, tb[blocked01Epoch][dep1], 1)

	tb.register(blocked1, blocked01Epoch)
	assert.True(t, blocked1.DependenciesCalled)
	assert.NotNil(t, tb)
	assert.Len(t, tb, 1)
	assert.NotNil(t, tb[blocked01Epoch])
	assert.Len(t, tb[blocked01Epoch], 2) // dep0, dep1
	assert.NotNil(t, tb[blocked01Epoch][dep0])
	assert.Len(t, tb[blocked01Epoch][dep0], 2) // blocked0, blocked1
	assert.NotNil(t, tb[blocked01Epoch][dep1])
	assert.Len(t, tb[blocked01Epoch][dep1], 2) // blocked0, blocked1

	tb.register(blocked2, blocked2Epoch)
	assert.True(t, blocked2.DependenciesCalled)
	assert.NotNil(t, tb)
	assert.Len(t, tb, 2) // blocked01Epoch, blocked2Epoch
	assert.NotNil(t, tb[blocked01Epoch])
	assert.Len(t, tb[blocked01Epoch], 2) // dep0, dep1
	assert.NotNil(t, tb[blocked01Epoch][dep0])
	assert.Len(t, tb[blocked01Epoch][dep0], 2) // blocked0, blocked1
	assert.NotNil(t, tb[blocked01Epoch][dep1])
	assert.Len(t, tb[blocked01Epoch][dep1], 2) // blocked0, blocked1
	assert.Len(t, tb[blocked2Epoch], 2)        // dep1, dep2
	assert.Len(t, tb[blocked2Epoch][dep1], 1)
	assert.Len(t, tb[blocked2Epoch][dep2], 1)
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

	// Mark that dep0 was issued in an epoch later than any of the blockers
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

	// Mark an unrelated transition as having been issued in blocked01Epoch
	trID := ids.GenerateTestID()
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
	assert.NotNil(t, tb[blocked01Epoch])       // blocked0 and blocked1 still depend on dep0 in this epoch
	assert.Len(t, tb[blocked01Epoch], 1)       // dep0
	assert.Len(t, tb[blocked01Epoch][dep0], 2) // blocked0 and blocked1
	assert.NotNil(t, tb[blocked2Epoch])        // blocked2's deps are unaffected
	assert.Len(t, tb[blocked2Epoch][dep1], 1)  // blocked2's deps are unaffected
	assert.Len(t, tb[blocked2Epoch][dep2], 1)  // blocked2's deps are unaffected
	blocked0.FulfillCalled = false             // reset
	blocked1.FulfillCalled = false             // reset

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
	assert.Nil(t, tb[blocked01Epoch]) // Nobody depends on anything in this epoch
	assert.NotNil(t, tb[blocked2Epoch])
	assert.Len(t, tb[blocked2Epoch][dep1], 1) // blocked2's deps are unaffected
	assert.Len(t, tb[blocked2Epoch][dep2], 1) // blocked2's deps are unaffected
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
	assert.Nil(t, tb[blocked2Epoch][dep1])    // nobody depends on dep1 anymore
	assert.NotNil(t, tb[blocked2Epoch])       // other dependency is unaffected
	assert.NotNil(t, tb[blocked2Epoch][dep2]) // other dependency is unaffected
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
	assert.NotNil(t, tb[blocked01Epoch])       // Still have dependency on dep1 in this epoch
	assert.Len(t, tb[blocked01Epoch], 1)       // dep1
	assert.Len(t, tb[blocked01Epoch][dep1], 2) // blocked0, blocked1
	assert.Len(t, tb[blocked2Epoch], 2)        // dep1, dep2
	blocked0.FulfillCalled = false             // reset
	blocked1.FulfillCalled = false             // reset

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
	assert.Len(t, tb[blocked01Epoch], 1)       // dep1
	assert.Len(t, tb[blocked01Epoch][dep1], 2) // blocked0, blocked1
	assert.Len(t, tb[blocked2Epoch], 1)        // dep1
	blocked2.FulfillCalled = false             // reset

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

	tb.abandon(dep2, blocked2Epoch)
	assert.Len(t, tb[blocked2Epoch][dep1], 1)  // blocked2
	assert.Len(t, tb[blocked01Epoch], 2)       // dep0, dep1
	assert.Len(t, tb[blocked01Epoch][dep0], 2) // blocked0, blocked1
	assert.Len(t, tb[blocked01Epoch][dep1], 2) // blocked0, blocked1

	tb.abandon(dep0, blocked01Epoch)
	assert.Len(t, tb, 2)                 // blocked0Epoch, blocked1Epoch
	assert.Len(t, tb[blocked01Epoch], 1) // dep1

	tb.abandon(dep0, blocked01Epoch) // abandoning again shouldn't do anything
	assert.Len(t, tb, 2)
	assert.Len(t, tb[blocked01Epoch], 1)

	tb.abandon(dep1, blocked2Epoch)
	assert.Len(t, tb, 1)
	assert.Len(t, tb[blocked01Epoch], 1)       // dep1
	assert.Len(t, tb[blocked01Epoch][dep1], 2) // blocked0, blocked1

	tb.abandon(dep1, blocked01Epoch)
	assert.Len(t, tb, 0)
}
