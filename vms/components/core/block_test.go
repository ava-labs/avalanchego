package core

import (
	"testing"

	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"

	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/versiondb"
)

func TestBlock(t *testing.T) {
	parentID := ids.NewID([32]byte{1, 2, 3, 4, 5})
	db := versiondb.New(memdb.New())
	state, err := NewSnowmanState(func([]byte) (snowman.Block, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	b := NewBlock(parentID)

	b.Initialize([]byte{1, 2, 3}, &SnowmanVM{
		DB:    db,
		State: state,
	})

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
