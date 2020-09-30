package blacklist

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow/validators"
)

// Test that blacklist will stop registering queries after a threshold of failures
func TestBlackList(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(50)
	vdr1 := validators.GenerateRandomValidator(50)
	vdr2 := validators.GenerateRandomValidator(50)
	vdr3 := validators.GenerateRandomValidator(50)
	vdr4 := validators.GenerateRandomValidator(50)
	vdrs.AddWeight(vdr0.ID(), vdr0.Weight())
	vdrs.AddWeight(vdr1.ID(), vdr1.Weight())
	vdrs.AddWeight(vdr2.ID(), vdr2.Weight())
	vdrs.AddWeight(vdr3.ID(), vdr3.Weight())
	vdrs.AddWeight(vdr4.ID(), vdr4.Weight())

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	blacklist := NewQueryBlacklist(vdrs, threshold, duration, maxPortion).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < threshold; i++ {
		if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed early for vdr0 on iteration: %d", i)
		}
		blacklist.QueryFailed(vdr0.ID(), requestID)
		requestID++
		if ok := blacklist.RegisterQuery(vdr1.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed for vdr1 on iteration: %d", i)
		}
		blacklist.RegisterResponse(vdr1.ID(), requestID)

		requestID++
		if ok := blacklist.RegisterQuery(vdr2.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed early for vdr2 on iteration: %d", i)
		}
		blacklist.QueryFailed(vdr2.ID(), requestID)
		requestID++
	}

	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); ok {
		t.Fatal("RegisterQuery should have blacklisted query from unresponsive peer: vdr0")
	}
	if ok := blacklist.RegisterQuery(vdr2.ID(), requestID); ok {
		t.Fatal("RegisterQuery should have blacklisted query from unresponsive peer: vdr2")
	}
	requestID++
	if ok := blacklist.RegisterQuery(vdr1.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have been successful for responsive peer: vdr1")
	}

	blacklist.clock.Set(currentTime.Add(duration))
	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed for vdr0")
	}
	if ok := blacklist.RegisterQuery(vdr2.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed for vdr2")
	}
}

func TestBlacklistDoesNotGetStuck(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(50)
	vdr1 := validators.GenerateRandomValidator(50)
	vdr2 := validators.GenerateRandomValidator(50)
	vdrs.AddWeight(vdr0.ID(), vdr0.Weight())
	vdrs.AddWeight(vdr1.ID(), vdr1.Weight())
	vdrs.AddWeight(vdr2.ID(), vdr2.Weight())

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	blacklist := NewQueryBlacklist(vdrs, threshold, duration, maxPortion).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < threshold; i++ {
		if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed early on iteration: %d", i)
		}
		blacklist.QueryFailed(vdr0.ID(), requestID)
		requestID++
	}

	// Check that calling QueryFailed repeatedly does not change
	// the blacklist end time after it's already been blacklisted
	for i := 0; i < threshold; i++ {
		blacklist.QueryFailed(vdr0.ID(), requestID)
		requestID++
	}

	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); ok {
		t.Fatal("RegisterQuery should have blacklisted query from consistently failing peer")
	}
	requestID++

	blacklist.clock.Set(currentTime.Add(duration))

	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}

	blacklist.QueryFailed(vdr0.ID(), requestID)

	// Test that consecutive failures is reset after blacklisting
	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after vdr0 was removed from blacklist")
	}
}

func TestBlacklistDoesNotExceedThreshold(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(50)
	vdr1 := validators.GenerateRandomValidator(50)
	vdr2 := validators.GenerateRandomValidator(50)
	vdrs.AddWeight(vdr0.ID(), vdr0.Weight())
	vdrs.AddWeight(vdr1.ID(), vdr1.Weight())
	vdrs.AddWeight(vdr2.ID(), vdr2.Weight())

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	blacklist := NewQueryBlacklist(vdrs, threshold, duration, maxPortion).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < threshold; i++ {
		if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed early for vdr0 on iteration: %d", i)
		}
		if ok := blacklist.RegisterQuery(vdr1.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed early for vdr1 on iteration: %d", i)
		}
		blacklist.QueryFailed(vdr0.ID(), requestID)
		blacklist.QueryFailed(vdr1.ID(), requestID)
		requestID++
	}

	ok0 := blacklist.RegisterQuery(vdr0.ID(), requestID)
	ok1 := blacklist.RegisterQuery(vdr1.ID(), requestID)
	if !ok0 && !ok1 {
		t.Fatal("Blacklisted staking weight past the allowed threshold")
	}

	blacklist.clock.Set(currentTime.Add(duration))
	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}
	if ok := blacklist.RegisterQuery(vdr1.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}
}
