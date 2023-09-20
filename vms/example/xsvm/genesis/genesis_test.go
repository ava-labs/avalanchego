package genesis

import (
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestGenesis(t *testing.T) {
	id, err := ids.ShortFromString("6Y3kysjF9jnHnYkdS9yGAuoHyae2eNmeV")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := ids.ShortFromString("LeKrndtsMxcLMzHz3w4uo1XtLDpfi66c")
	if err != nil {
		t.Fatal(err)
	}

	genesis := Genesis{
		Timestamp: 123,
		Allocations: []Allocation{
			{Address: id, Balance: 1000000000},
			{Address: id2, Balance: 3000000000},
		},
	}
	bytes, err := Codec.Marshal(Version, genesis)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := Parse(bytes)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(genesis, *parsed) {
		t.Fatalf("expected %v, got %v", genesis, parsed)
	}
}
