package common

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// Test that blacklist will stop registering queries after a threshold of failures
func TestBlackList(t *testing.T) {
	failureThreshold := 3
	blacklistTime := time.Minute
	blacklist := NewQueryBlacklist(failureThreshold, blacklistTime).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	goodID := ids.NewShortID([20]byte{0})
	failingPeerID := ids.NewShortID([20]byte{1})
	requestID := uint32(0)

	for i := 0; i < failureThreshold; i++ {
		if ok := blacklist.RegisterQuery(failingPeerID, requestID); !ok {
			t.Fatalf("RegisterQuery failed early on iteration: %d", i)
		}
		blacklist.QueryFailed(failingPeerID, requestID)
		requestID++
		if ok := blacklist.RegisterQuery(goodID, requestID); !ok {
			t.Fatalf("RegisterQuery failed for good peer on iteration: %d", i)
		}
		blacklist.RegisterResponse(goodID, requestID)
		requestID++
	}

	if ok := blacklist.RegisterQuery(failingPeerID, requestID); ok {
		t.Fatal("RegisterQuery should have blacklisted query from consistently failing peer")
	}
	requestID++
	if ok := blacklist.RegisterQuery(goodID, requestID); !ok {
		t.Fatal("RegisterQuery should have been successful for responsive peer")
	}

	blacklist.clock.Set(currentTime.Add(blacklistTime))
	if ok := blacklist.RegisterQuery(failingPeerID, requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}
}

func TestBlacklistDoesNotGetStuck(t *testing.T) {
	failureThreshold := 3
	blacklistTime := time.Minute
	blacklist := NewQueryBlacklist(failureThreshold, blacklistTime).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	failingPeerID := ids.NewShortID([20]byte{1})
	requestID := uint32(0)

	for i := 0; i < failureThreshold; i++ {
		if ok := blacklist.RegisterQuery(failingPeerID, requestID); !ok {
			t.Fatalf("RegisterQuery failed early on iteration: %d", i)
		}
		blacklist.QueryFailed(failingPeerID, requestID)
		requestID++
	}

	// Check that calling QueryFailed repeatedly does not change
	// the blacklist end time after it's already been blacklisted
	for i := 0; i < failureThreshold; i++ {
		blacklist.QueryFailed(failingPeerID, requestID)
		requestID++
	}

	if ok := blacklist.RegisterQuery(failingPeerID, requestID); ok {
		t.Fatal("RegisterQuery should have blacklisted query from consistently failing peer")
	}
	requestID++

	blacklist.clock.Set(currentTime.Add(blacklistTime))
	if ok := blacklist.RegisterQuery(failingPeerID, requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}
}
