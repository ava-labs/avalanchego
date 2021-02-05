package benchlist

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	minimumFailingDuration = 5 * time.Minute
)

// Test that benchlist will stop registering queries after a threshold of failures
func TestBenchlist(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(50)
	vdr1 := validators.GenerateRandomValidator(50)
	vdr2 := validators.GenerateRandomValidator(50)
	vdr3 := validators.GenerateRandomValidator(50)
	vdr4 := validators.GenerateRandomValidator(50)

	errs := wrappers.Errs{}
	errs.Add(
		vdrs.AddWeight(vdr0.ID(), vdr0.Weight()),
		vdrs.AddWeight(vdr1.ID(), vdr1.Weight()),
		vdrs.AddWeight(vdr2.ID(), vdr2.Weight()),
		vdrs.AddWeight(vdr3.ID(), vdr3.Weight()),
		vdrs.AddWeight(vdr4.ID(), vdr4.Weight()),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	benchIntf, err := NewQueryBenchlist(
		vdrs,
		snow.DefaultContextTest(),
		threshold,
		minimumFailingDuration,
		duration,
		maxPortion,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	b := benchIntf.(*queryBenchlist)

	currentTime := time.Now()
	b.clock.Set(currentTime)

	requestID := uint32(0)

	if ok := b.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery failed early for vdr1")
	}
	b.RegisterResponse(vdr1.ID(), requestID)

	if ok := bench(b, []ids.ShortID{vdr0.ID(), vdr2.ID()}); !ok {
		t.Fatal("RegisterQuery failed early")
	}

	if ok := b.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("RegisterQuery should have benchlisted query from unresponsive peer: vdr0")
	}
	if ok := b.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("RegisterQuery should have benchlisted query from unresponsive peer: vdr2")
	}
	requestID++
	if ok := b.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have been successful for responsive peer: vdr1")
	}

	b.clock.Set(b.clock.Time().Add(duration))
	if ok := b.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed for vdr0")
	}
	if ok := b.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed for vdr2")
	}
}

func TestBenchlistDoesNotGetStuck(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(50)
	vdr1 := validators.GenerateRandomValidator(50)
	vdr2 := validators.GenerateRandomValidator(50)

	errs := wrappers.Errs{}
	errs.Add(
		vdrs.AddWeight(vdr0.ID(), vdr0.Weight()),
		vdrs.AddWeight(vdr1.ID(), vdr1.Weight()),
		vdrs.AddWeight(vdr2.ID(), vdr2.Weight()),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	benchIntf, err := NewQueryBenchlist(
		vdrs,
		snow.DefaultContextTest(),
		threshold,
		minimumFailingDuration,
		duration,
		maxPortion,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	b := benchIntf.(*queryBenchlist)

	currentTime := time.Now()
	b.clock.Set(currentTime)

	requestID := uint32(0)
	currentTime = currentTime.Add(minimumFailingDuration + time.Second)

	if ok := bench(b, []ids.ShortID{vdr0.ID()}); !ok {
		t.Fatal("RegisterQuery failed early for vdr0")
	}

	// Check that calling QueryFailed repeatedly does not change
	// the benchlist end time after it's already been benchlisted
	for ; requestID < uint32(threshold); requestID++ {
		b.QueryFailed(vdr0.ID(), requestID)
	}

	if ok := b.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); ok {
		t.Fatal("RegisterQuery should have benchlisted query from consistently failing peer")
	}
	requestID++

	b.clock.Set(b.clock.Time().Add(duration))

	if ok := b.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after benchlisting time elapsed")
	}

	b.clock.Set(b.clock.Time().Add(minimumFailingDuration + time.Second))
	b.QueryFailed(vdr0.ID(), requestID)

	// Test that consecutive failures is reset after benchlisting
	if ok := b.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		t.Fatal("RegisterQuery should have succeeded after vdr0 was removed from benchlist and only failed once")
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

	errs := wrappers.Errs{}
	errs.Add(
		vdrs.AddWeight(vdr0.ID(), vdr0.Weight()),
		vdrs.AddWeight(vdr1.ID(), vdr1.Weight()),
		vdrs.AddWeight(vdr2.ID(), vdr2.Weight()),
		vdrs.AddWeight(vdr3.ID(), vdr3.Weight()),
		vdrs.AddWeight(vdr4.ID(), vdr4.Weight()),
		vdrs.AddWeight(vdr5.ID(), vdr5.Weight()),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	benchIntf, err := NewQueryBenchlist(
		vdrs,
		snow.DefaultContextTest(),
		threshold,
		minimumFailingDuration,
		duration,
		maxPortion,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	b := benchIntf.(*queryBenchlist)

	currentTime := time.Now()
	b.clock.Set(currentTime)

	requestID := uint32(0)
	failingValidators := []ids.ShortID{vdr0.ID(), vdr1.ID(), vdr2.ID(), vdr3.ID()}

	bench(b, failingValidators)

	benchedWeight := uint64(0)
	if ok := b.RegisterQuery(vdr0.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr0.Weight()
	}
	if ok := b.RegisterQuery(vdr1.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr1.Weight()
	}
	if ok := b.RegisterQuery(vdr2.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr2.Weight()
	}
	if ok := b.RegisterQuery(vdr3.ID(), requestID, constants.PullQueryMsg); !ok {
		benchedWeight += vdr3.Weight()
	}

	if benchedWeight > 125 {
		t.Fatalf("Expected benched weight to be less than half ie. 125, but found benched weight of %d", benchedWeight)
	} else if benchedWeight < 100 {
		t.Fatalf("Unbenched too much weight during cleanup. Expected bench weight to be: %d. Found: %d", 100, benchedWeight)
	}

	b.clock.Set(b.clock.Time().Add(duration))
	for i, validatorID := range failingValidators {
		if ok := b.RegisterQuery(validatorID, 83, constants.PullQueryMsg); !ok {
			t.Fatalf("RegisterQuery should have succeeded for vdr%d after benchlisting time elapsed", i)
		}
	}
}

// failMessage registers a query and failure for [validatorID]
// returns false if the message cannot be regisered in the first place
func failMessage(benchlist *queryBenchlist, validatorID ids.ShortID) bool {
	if ok := benchlist.RegisterQuery(validatorID, 5, constants.PullQueryMsg); !ok {
		return false
	}
	benchlist.QueryFailed(validatorID, 5)

	return true
}

// bench adjusts the time and fails sufficient messages to bench
// [validatorIDs]
func bench(b *queryBenchlist, validatorIDs []ids.ShortID) bool {
	currentTime := b.clock.Time()
	for _, validatorID := range validatorIDs {
		if ok := failMessage(b, validatorID); !ok {
			return false
		}
	}
	b.clock.Set(currentTime.Add(b.minimumFailingDuration + time.Second))

	for requestID := 0; requestID < b.threshold-1; requestID++ {
		for _, validatorID := range validatorIDs {
			if ok := failMessage(b, validatorID); !ok {
				return false
			}
		}
	}

	return true
}
