// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"reflect"
	"testing"
)

func TestAliaserLookupError(t *testing.T) {
	emptyAliaser := &Aliaser{}
	emptyAliaser.Initialize()
	tests := []struct {
		label   string
		aliaser *Aliaser
		alias   string
		res     ID
	}{
		{"Uninitialized", &Aliaser{}, "Batwoman", ID{}},
		{"Empty", emptyAliaser, "Batman", ID{}},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			res, err := tt.aliaser.Lookup(tt.alias)
			if tt.res != res {
				t.Errorf("Got %v, expected %v", res, tt.res)
			}
			if err == nil {
				t.Error("Expected an error due to missing alias")
			}
		})
	}
}

func TestAliaserLookup(t *testing.T) {
	id := ID{'K', 'a', 't', 'e', ' ', 'K', 'a', 'n', 'e'}
	aliaser := Aliaser{}
	aliaser.Initialize()
	if err := aliaser.Alias(id, "Batwoman"); err != nil {
		t.Fatal(err)
	}

	res, err := aliaser.Lookup("Batwoman")
	if err != nil {
		t.Fatalf("Unexpected error %q", err)
	}
	if id != res {
		t.Fatalf("Got %v, expected %v", res, id)
	}
}

func TestAliaserAliasesEmpty(t *testing.T) {
	id := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	aliaser := Aliaser{}
	aliaser.Initialize()

	aliases := aliaser.Aliases(id)
	if len(aliases) != 0 {
		t.Fatalf("Unexpected aliases %#v", aliases)
	}
}

func TestAliaserAliases(t *testing.T) {
	id := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	aliaser := Aliaser{}
	aliaser.Initialize()
	if err := aliaser.Alias(id, "Batman"); err != nil {
		t.Fatal(err)
	}
	if err := aliaser.Alias(id, "Dark Knight"); err != nil {
		t.Fatal(err)
	}

	aliases := aliaser.Aliases(id)
	expected := []string{"Batman", "Dark Knight"}
	if !reflect.DeepEqual(aliases, expected) {
		t.Fatalf("Got %v, expected %v", aliases, expected)
	}
}

func TestAliaserPrimaryAlias(t *testing.T) {
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	aliaser := Aliaser{}
	aliaser.Initialize()
	if err := aliaser.Alias(id2, "Batman"); err != nil {
		t.Fatal(err)
	}
	if err := aliaser.Alias(id2, "Dark Knight"); err != nil {
		t.Fatal(err)
	}

	res, err := aliaser.PrimaryAlias(id1)
	if res != "" {
		t.Fatalf("Unexpected alias for %v", id1)
	}
	if err == nil {
		t.Fatal("Expected an error given an id with no aliases")
	}

	res, err = aliaser.PrimaryAlias(id2)
	expected := "Batman"
	if res != expected {
		t.Fatalf("Got %v, expected %v", res, expected)
	}
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
}

func TestAliaserAliasClash(t *testing.T) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'D', 'i', 'c', 'k', ' ', 'G', 'r', 'a', 'y', 's', 'o', 'n'}
	aliaser := Aliaser{}
	aliaser.Initialize()
	if err := aliaser.Alias(id1, "Batman"); err != nil {
		t.Fatal(err)
	}

	err := aliaser.Alias(id2, "Batman")
	if err == nil {
		t.Fatalf("Expected an error, due to an existing alias")
	}
}

func TestAliaserRemoveAlias(t *testing.T) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	aliaser := Aliaser{}
	aliaser.Initialize()
	if err := aliaser.Alias(id1, "Batman"); err != nil {
		t.Fatal(err)
	}
	if err := aliaser.Alias(id1, "Dark Knight"); err != nil {
		t.Fatal(err)
	}

	aliaser.RemoveAliases(id1)

	_, err := aliaser.PrimaryAlias(id1)
	if err == nil {
		t.Fatalf("PrimaryAlias should have errored while getting primary alias for removed ID")
	}

	err = aliaser.Alias(id2, "Batman")
	if err != nil {
		t.Fatalf("Unexpected error: %s when re-assigning removed alias", err)
	}

	err = aliaser.Alias(id2, "Dark Knight")
	if err != nil {
		t.Fatalf("Unexpected error: %s when re-assigning removed alias", err)
	}

	err = aliaser.Alias(id1, "Dark Night Rises")
	if err != nil {
		t.Fatalf("Unexpected error: %s when re-assigning removed ID in aliaser", err)
	}
}
