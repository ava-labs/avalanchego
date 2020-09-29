package blacklist

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
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

	config := &Config{
		Threshold:  3,
		Duration:   time.Minute,
		MaxPortion: 0.5,
		Validators: vdrs,
	}
	blacklist := NewQueryBlacklist(config).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < config.Threshold; i++ {
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

	blacklist.clock.Set(currentTime.Add(config.Duration))
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

	config := &Config{
		Threshold:  3,
		Duration:   time.Minute,
		MaxPortion: 0.5,
		Validators: vdrs,
	}
	blacklist := NewQueryBlacklist(config).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < config.Threshold; i++ {
		if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
			t.Fatalf("RegisterQuery failed early on iteration: %d", i)
		}
		blacklist.QueryFailed(vdr0.ID(), requestID)
		requestID++
	}

	// Check that calling QueryFailed repeatedly does not change
	// the blacklist end time after it's already been blacklisted
	for i := 0; i < config.Threshold; i++ {
		blacklist.QueryFailed(vdr0.ID(), requestID)
		requestID++
	}

	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); ok {
		t.Fatal("RegisterQuery should have blacklisted query from consistently failing peer")
	}
	requestID++

	blacklist.clock.Set(currentTime.Add(config.Duration))
	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
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

	config := &Config{
		Threshold:  3,
		Duration:   time.Minute,
		MaxPortion: 0.5,
		Validators: vdrs,
	}
	blacklist := NewQueryBlacklist(config).(*queryBlacklist)

	currentTime := time.Now()
	blacklist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < config.Threshold; i++ {
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

	blacklist.clock.Set(currentTime.Add(config.Duration))
	if ok := blacklist.RegisterQuery(vdr0.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}
	if ok := blacklist.RegisterQuery(vdr1.ID(), requestID); !ok {
		t.Fatal("RegisterQuery should have succeeded after blacklisting time elapsed")
	}
}

type noBlacklist struct{}

// NewNoBlacklist returns an empty blacklist that will never stop any queries
func NewNoBlacklist() Manager { return &noBlacklist{} }

// RegisterQuery ...
func (b *noBlacklist) RegisterQuery(chainID ids.ID, validatorID ids.ShortID, requestID uint32) bool {
	return true
}

// RegisterResponse ...
func (b *noBlacklist) RegisterResponse(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {}

// QueryFailed ...
func (b *noBlacklist) QueryFailed(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {}
