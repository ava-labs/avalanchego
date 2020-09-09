package core

import (
	"testing"

	"github.com/ava-labs/avalanche-go/database/memdb"
	"github.com/ava-labs/avalanche-go/database/versiondb"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/choices"
	"github.com/ava-labs/avalanche-go/snow/consensus/snowman"
)

func TestBlock(t *testing.T) {
	parentID := ids.NewID([32]byte{1, 2, 3, 4, 5})
	db := versiondb.New(memdb.New())
	state, err := NewSnowmanState(func([]byte) (snowman.Block, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	b := NewBlock(parentID, 1)

	b.Initialize([]byte{1, 2, 3}, &SnowmanVM{
		DB:    db,
		State: state,
	})

	if b.Hght != 1 {
		t.Fatal("height should be 1")
	}

	// should be unknown until someone queries for it
	if status := b.Metadata.status; status != choices.Unknown {
		t.Fatalf("status should be unknown but is %s", status)
	}

	// querying should change status to processing
	if status := b.Status(); status != choices.Processing {
		t.Fatalf("status should be processing but is %s", status)
	}

	b.Accept()
	if status := b.Status(); status != choices.Accepted {
		t.Fatalf("status should be accepted but is %s", status)
	}

	b.Reject()
	if status := b.Status(); status != choices.Rejected {
		t.Fatalf("status should be rejected but is %s", status)
	}
}
