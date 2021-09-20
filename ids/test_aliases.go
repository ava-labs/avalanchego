// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"reflect"
	"testing"
)

var AliasTests = []func(t *testing.T, r AliaserReader, w AliaserWriter){
	AliaserLookupErrorTest,
	AliaserLookupTest,
	AliaserAliasesEmptyTest,
	AliaserAliasesTest,
	AliaserPrimaryAliasTest,
	AliaserAliasClashTest,
	AliaserRemoveAliasTest,
}

func AliaserLookupErrorTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	_, err := r.Lookup("Batman")
	if err == nil {
		t.Error("Expected an error due to missing alias")
	}
}

func AliaserLookupTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	id := ID{'K', 'a', 't', 'e', ' ', 'K', 'a', 'n', 'e'}
	if err := w.Alias(id, "Batwoman"); err != nil {
		t.Fatal(err)
	}

	res, err := r.Lookup("Batwoman")
	if err != nil {
		t.Fatalf("Unexpected error %q", err)
	}
	if id != res {
		t.Fatalf("Got %v, expected %v", res, id)
	}
}

func AliaserAliasesEmptyTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	id := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}

	aliases, err := r.Aliases(id)
	if err != nil {
		t.Fatalf("Unexpected error %q", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("Unexpected aliases %#v", aliases)
	}
}

func AliaserAliasesTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	id := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	if err := w.Alias(id, "Batman"); err != nil {
		t.Fatal(err)
	}
	if err := w.Alias(id, "Dark Knight"); err != nil {
		t.Fatal(err)
	}

	aliases, err := r.Aliases(id)
	if err != nil {
		t.Fatalf("Unexpected error %q", err)
	}

	expected := []string{"Batman", "Dark Knight"}
	if !reflect.DeepEqual(aliases, expected) {
		t.Fatalf("Got %v, expected %v", aliases, expected)
	}
}

func AliaserPrimaryAliasTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	if err := w.Alias(id2, "Batman"); err != nil {
		t.Fatal(err)
	}
	if err := w.Alias(id2, "Dark Knight"); err != nil {
		t.Fatal(err)
	}

	res, err := r.PrimaryAlias(id1)
	if res != "" {
		t.Fatalf("Unexpected alias for %v", id1)
	}
	if err == nil {
		t.Fatal("Expected an error given an id with no aliases")
	}

	res, err = r.PrimaryAlias(id2)
	expected := "Batman"
	if res != expected {
		t.Fatalf("Got %v, expected %v", res, expected)
	}
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
}

func AliaserAliasClashTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'D', 'i', 'c', 'k', ' ', 'G', 'r', 'a', 'y', 's', 'o', 'n'}
	if err := w.Alias(id1, "Batman"); err != nil {
		t.Fatal(err)
	}

	err := w.Alias(id2, "Batman")
	if err == nil {
		t.Fatalf("Expected an error, due to an existing alias")
	}
}

func AliaserRemoveAliasTest(t *testing.T, r AliaserReader, w AliaserWriter) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	if err := w.Alias(id1, "Batman"); err != nil {
		t.Fatal(err)
	}
	if err := w.Alias(id1, "Dark Knight"); err != nil {
		t.Fatal(err)
	}

	w.RemoveAliases(id1)

	_, err := r.PrimaryAlias(id1)
	if err == nil {
		t.Fatalf("PrimaryAlias should have errored while getting primary alias for removed ID")
	}

	err = w.Alias(id2, "Batman")
	if err != nil {
		t.Fatalf("Unexpected error: %s when re-assigning removed alias", err)
	}

	err = w.Alias(id2, "Dark Knight")
	if err != nil {
		t.Fatalf("Unexpected error: %s when re-assigning removed alias", err)
	}

	err = w.Alias(id1, "Dark Night Rises")
	if err != nil {
		t.Fatalf("Unexpected error: %s when re-assigning removed ID in aliaser", err)
	}
}
