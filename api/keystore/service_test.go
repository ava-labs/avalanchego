// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"bytes"
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/logging"
)

func TestServiceListNoUsers(t *testing.T) {
	ks := Keystore{}
	ks.Initialize(logging.NoLog{}, memdb.New())

	reply := ListUsersReply{}
	if err := ks.ListUsers(nil, &ListUsersArgs{}, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Users) != 0 {
		t.Fatalf("No users should have been created yet")
	}
}

func TestServiceCreateUser(t *testing.T) {
	ks := Keystore{}
	ks.Initialize(logging.NoLog{}, memdb.New())

	{
		reply := CreateUserReply{}
		if err := ks.CreateUser(nil, &CreateUserArgs{
			Username: "bob",
			Password: "launch",
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		reply := ListUsersReply{}
		if err := ks.ListUsers(nil, &ListUsersArgs{}, &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Users) != 1 {
			t.Fatalf("One user should have been created")
		}
		if user := reply.Users[0]; user != "bob" {
			t.Fatalf("'bob' should have been a user that was created")
		}
	}
}

func TestServiceCreateDuplicate(t *testing.T) {
	ks := Keystore{}
	ks.Initialize(logging.NoLog{}, memdb.New())

	{
		reply := CreateUserReply{}
		if err := ks.CreateUser(nil, &CreateUserArgs{
			Username: "bob",
			Password: "launch",
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		reply := CreateUserReply{}
		if err := ks.CreateUser(nil, &CreateUserArgs{
			Username: "bob",
			Password: "launch!",
		}, &reply); err == nil {
			t.Fatalf("Should have errored due to the username already existing")
		}
	}
}

func TestServiceCreateUserNoName(t *testing.T) {
	ks := Keystore{}
	ks.Initialize(logging.NoLog{}, memdb.New())

	reply := CreateUserReply{}
	if err := ks.CreateUser(nil, &CreateUserArgs{
		Password: "launch",
	}, &reply); err == nil {
		t.Fatalf("Shouldn't have allowed empty username")
	}
}

func TestServiceUseBlockchainDB(t *testing.T) {
	ks := Keystore{}
	ks.Initialize(logging.NoLog{}, memdb.New())

	{
		reply := CreateUserReply{}
		if err := ks.CreateUser(nil, &CreateUserArgs{
			Username: "bob",
			Password: "launch",
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", "launch")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Put([]byte("hello"), []byte("world")); err != nil {
			t.Fatal(err)
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", "launch")
		if err != nil {
			t.Fatal(err)
		}
		if val, err := db.Get([]byte("hello")); err != nil {
			t.Fatal(err)
		} else if !bytes.Equal(val, []byte("world")) {
			t.Fatalf("Should have read '%s' from the db", "world")
		}
	}
}

func TestServiceExportImport(t *testing.T) {
	ks := Keystore{}
	ks.Initialize(logging.NoLog{}, memdb.New())

	{
		reply := CreateUserReply{}
		if err := ks.CreateUser(nil, &CreateUserArgs{
			Username: "bob",
			Password: "launch",
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", "launch")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Put([]byte("hello"), []byte("world")); err != nil {
			t.Fatal(err)
		}
	}

	exportReply := ExportUserReply{}
	if err := ks.ExportUser(nil, &ExportUserArgs{
		Username: "bob",
		Password: "launch",
	}, &exportReply); err != nil {
		t.Fatal(err)
	}

	newKS := Keystore{}
	newKS.Initialize(logging.NoLog{}, memdb.New())

	{
		reply := ImportUserReply{}
		if err := newKS.ImportUser(nil, &ImportUserArgs{
			Username: "bob",
			Password: "launch",
			User:     exportReply.User,
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been imported successfully")
		}
	}

	{
		db, err := newKS.GetDatabase(ids.Empty, "bob", "launch")
		if err != nil {
			t.Fatal(err)
		}
		if val, err := db.Get([]byte("hello")); err != nil {
			t.Fatal(err)
		} else if !bytes.Equal(val, []byte("world")) {
			t.Fatalf("Should have read '%s' from the db", "world")
		}
	}
}
