package benchlist

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// Test that benchlist will stop registering queries after a threshold of failures
func TestBenchlist(t *testing.T) {
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
	benchlist := NewQueryBenchlist(vdrs, snow.DefaultContextTest(), threshold, duration, maxPortion, false, "").(*queryBenchlist)

	currentTime := time.Now()
	benchlist.clock.Set(currentTime)

	requestID := uint32(0)

	for i := 0; i < threshold; i++ {
		if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr0 on iteration: %d", i)
		}
		benchlist.QueryFailed(vdr0.ID(), requestID)
		requestID++
		if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed for vdr1 on iteration: %d", i)
		}
		benchlist.RegisterResponse(vdr1.ID(), requestID)

		requestID++
		if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr2 on iteration: %d", i)
		}
		benchlist.QueryFailed(vdr2.ID(), requestID)
		requestID++
	}

	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("RegisterQuery should have benchlisted query from unresponsive peer: vdr0")
	}
	if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("RegisterQuery should have benchlisted query from unresponsive peer: vdr2")
	}
	requestID++
	if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have been successful for responsive peer: vdr1")
	}

	benchlist.clock.Set(currentTime.Add(duration))
	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed for vdr0")
	}
	if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed for vdr2")
	}
}

func TestBenchlistDoesNotGetStuck(t *testing.T) {
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
	benchlist := NewQueryBenchlist(vdrs, snow.DefaultContextTest(), threshold, duration, maxPortion, false, "").(*queryBenchlist)

	currentTime := time.Now()
	benchlist.clock.Set(currentTime)

	requestID := uint32(0)

	for ; requestID < uint32(threshold); requestID++ {
		if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early on requestID: %d", requestID)
		}
		benchlist.QueryFailed(vdr0.ID(), requestID)
	}

	// Check that calling QueryFailed repeatedly does not change
	// the benchlist end time after it's already been benchlisted
	for ; requestID < uint32(threshold); requestID++ {
		benchlist.QueryFailed(vdr0.ID(), requestID)
	}

	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("RegisterQuery should have benchlisted query from consistently failing peer")
	}
	requestID++

	benchlist.clock.Set(currentTime.Add(duration))

	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}

	benchlist.QueryFailed(vdr0.ID(), requestID)

	// Test that consecutive failures is reset after benchlisting
	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after vdr0 was removed from benchlist")
	}
}

func TestBenchlistDoesNotExceedThreshold(t *testing.T) {
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
	benchlist := NewQueryBenchlist(vdrs, snow.DefaultContextTest(), threshold, duration, maxPortion, false, "").(*queryBenchlist)

	currentTime := time.Now()
	benchlist.clock.Set(currentTime)

	requestID := uint32(0)

	for ; requestID < uint32(threshold); requestID++ {
		if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr0 on requestID: %d", requestID)
		}
		if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr1 on requestID: %d", requestID)
		}
		benchlist.QueryFailed(vdr0.ID(), requestID)
		benchlist.QueryFailed(vdr1.ID(), requestID)
	}

	ok0 := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg)
	ok1 := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg)
	if !ok0 && !ok1 {
		t.Fatal("Benchlisted staking weight past the allowed threshold")
	}

	benchlist.clock.Set(currentTime.Add(duration))
	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}
	if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}
}

func TestBenchlistOverUnbench(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(1)
	vdr1 := validators.GenerateRandomValidator(49)
	vdr2 := validators.GenerateRandomValidator(50)
	vdr3 := validators.GenerateRandomValidator(50)
	vdr4 := validators.GenerateRandomValidator(50)
	vdr5 := validators.GenerateRandomValidator(50)
	vdrs.AddWeight(vdr0.ID(), vdr0.Weight())
	vdrs.AddWeight(vdr1.ID(), vdr1.Weight())
	vdrs.AddWeight(vdr2.ID(), vdr2.Weight())
	vdrs.AddWeight(vdr3.ID(), vdr3.Weight())
	vdrs.AddWeight(vdr4.ID(), vdr4.Weight())
	vdrs.AddWeight(vdr5.ID(), vdr5.Weight())

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	benchlist := NewQueryBenchlist(vdrs, snow.DefaultContextTest(), threshold, duration, maxPortion, false, "").(*queryBenchlist)

	currentTime := time.Now()
	benchlist.clock.Set(currentTime)

	requestID := uint32(0)

	// Fail enough queries to bench validators 0, 1, and 2 ie. 100/250 total weight
	for ; requestID < uint32(threshold); requestID++ {
		if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr0 on requestID: %d", requestID)
		}
		if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr1 on requestID: %d", requestID)
		}
		if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vdr2 on requestID: %d", requestID)
		}
		benchlist.QueryFailed(vdr0.ID(), requestID)
		benchlist.QueryFailed(vdr1.ID(), requestID)
		benchlist.QueryFailed(vdr2.ID(), requestID)
	}

	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("vdr0 should have been benched")
	}
	if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("vdr1 should have been benched")
	}
	if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("vdr2 should have been benched")
	}

	// Benching 1 more validator should surpass the benchlist weight limit of half the portion
	// by benching 150/250 total weight
	// this should result in enough being removed so that it falls under the 0.5 max portion
	for requestID := uint32(0); requestID < uint32(threshold); requestID++ {
		if ok := benchlist.RegisterQuery(vdr3.ID(), requestID, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery failed early for vsr2 on requestID: %d", requestID)
		}
		benchlist.QueryFailed(vdr3.ID(), requestID)
	}

	benchedWeight := uint64(0)
	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr0.Weight()
	}
	if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr1.Weight()
	}
	if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr2.Weight()
	}
	if ok := benchlist.RegisterQuery(vdr3.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr3.Weight()
	}

	if benchedWeight > 125 {
		t.Fatalf("Expected benched weight to be less than half ie. 125, but found benched weight of %d", benchedWeight)
	}

	benchlist.clock.Set(currentTime.Add(duration))
	if ok := benchlist.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}
	if ok := benchlist.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}
	if ok := benchlist.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}
	if ok := benchlist.RegisterQuery(vdr3.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}
}
