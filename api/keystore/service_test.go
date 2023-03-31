// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// strongPassword defines a password used for the following tests that
// scores high enough to pass the password strength scoring system
var strongPassword = "N_+=_jJ;^(<;{4,:*m6CET}'&N;83FYK.wtNpwp-Jt" // #nosec G101

func TestServiceListNoUsers(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	reply := ListUsersReply{}
	if err := s.ListUsers(nil, nil, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Users) != 0 {
		t.Fatalf("No users should have been created yet")
	}
}

func TestServiceCreateUser(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	{
		err := s.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &api.EmptyReply{})
		if err != nil {
			t.Fatal(err)
		}
	}

	{
		reply := ListUsersReply{}
		if err := s.ListUsers(nil, nil, &reply); err != nil {
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

// genStr returns a string of given length
func genStr(n int) string {
	b := make([]byte, n)
	rand.Read(b) // #nosec G404
	return fmt.Sprintf("%x", b)[:n]
}

// TestServiceCreateUserArgsCheck generates excessively long usernames or
// passwords to assure the sanity checks on string length are not exceeded
func TestServiceCreateUserArgsCheck(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	{
		reply := api.EmptyReply{}
		err := s.CreateUser(nil, &api.UserPass{
			Username: genStr(maxUserLen + 1),
			Password: strongPassword,
		}, &reply)

		if err != errUserMaxLength {
			t.Fatal("User was created when it should have been rejected due to too long a Username, err =", err)
		}
	}

	{
		reply := api.EmptyReply{}
		err := s.CreateUser(nil, &api.UserPass{
			Username: "shortuser",
			Password: genStr(maxUserLen + 1),
		}, &reply)

		if err == nil {
			t.Fatal("User was created when it should have been rejected due to too long a Password, err =", err)
		}
	}

	{
		reply := ListUsersReply{}
		if err := s.ListUsers(nil, nil, &reply); err != nil {
			t.Fatal(err)
		}

		if len(reply.Users) > 0 {
			t.Fatalf("A user exists when there should be none")
		}
	}
}

// TestServiceCreateUserWeakPassword tests creating a new user with a weak
// password to ensure the password strength check is working
func TestServiceCreateUserWeakPassword(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	{
		reply := api.EmptyReply{}
		err := s.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: "weak",
		}, &reply)

		if err == nil {
			t.Error("Expected error when testing weak password")
		}
	}
}

func TestServiceCreateDuplicate(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	{
		err := s.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &api.EmptyReply{})
		if err != nil {
			t.Fatal(err)
		}
	}

	{
		err := s.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &api.EmptyReply{})
		if err == nil {
			t.Fatalf("Should have errored due to the username already existing")
		}
	}
}

func TestServiceCreateUserNoName(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	reply := api.EmptyReply{}
	if err := s.CreateUser(nil, &api.UserPass{
		Password: strongPassword,
	}, &reply); err == nil {
		t.Fatalf("Shouldn't have allowed empty username")
	}
}

func TestServiceUseBlockchainDB(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	s := service{ks: ks.(*keystore)}

	{
		err := s.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &api.EmptyReply{})
		if err != nil {
			t.Fatal(err)
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Put([]byte("hello"), []byte("world")); err != nil {
			t.Fatal(err)
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
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
	encodings := []formatting.Encoding{formatting.Hex}
	for _, encoding := range encodings {
		ks, err := CreateTestKeystore()
		if err != nil {
			t.Fatal(err)
		}
		s := service{ks: ks.(*keystore)}

		{
			err := s.CreateUser(nil, &api.UserPass{
				Username: "bob",
				Password: strongPassword,
			}, &api.EmptyReply{})
			if err != nil {
				t.Fatal(err)
			}
		}

		{
			db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Put([]byte("hello"), []byte("world")); err != nil {
				t.Fatal(err)
			}
		}

		exportArgs := ExportUserArgs{
			UserPass: api.UserPass{
				Username: "bob",
				Password: strongPassword,
			},
			Encoding: encoding,
		}
		exportReply := ExportUserReply{}
		if err := s.ExportUser(nil, &exportArgs, &exportReply); err != nil {
			t.Fatal(err)
		}

		newKS, err := CreateTestKeystore()
		if err != nil {
			t.Fatal(err)
		}
		newS := service{ks: newKS.(*keystore)}

		{
			err := newS.ImportUser(nil, &ImportUserArgs{
				UserPass: api.UserPass{
					Username: "bob",
					Password: "",
				},
				User: exportReply.User,
			}, &api.EmptyReply{})
			if err == nil {
				t.Fatal("Should have errored due to incorrect password")
			}
		}

		{
			err := newS.ImportUser(nil, &ImportUserArgs{
				UserPass: api.UserPass{
					Username: "",
					Password: "strongPassword",
				},
				User: exportReply.User,
			}, &api.EmptyReply{})
			if err == nil {
				t.Fatal("Should have errored due to empty username")
			}
		}

		{
			err := newS.ImportUser(nil, &ImportUserArgs{
				UserPass: api.UserPass{
					Username: "bob",
					Password: strongPassword,
				},
				User:     exportReply.User,
				Encoding: encoding,
			}, &api.EmptyReply{})
			if err != nil {
				t.Fatal(err)
			}
		}

		{
			db, err := newKS.GetDatabase(ids.Empty, "bob", strongPassword)
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
}

func TestServiceDeleteUser(t *testing.T) {
	testUser := "testUser"
	password := "passwTest@fake01ord"
	tests := []struct {
		desc      string
		setup     func(ks *keystore) error
		request   *api.UserPass
		want      *api.EmptyReply
		wantError bool
	}{{
		desc:      "empty user name case",
		request:   &api.UserPass{},
		wantError: true,
	}, {
		desc:      "user not exists case",
		request:   &api.UserPass{Username: "dummy"},
		wantError: true,
	}, {
		desc:      "user exists and invalid password case",
		request:   &api.UserPass{Username: testUser, Password: "password"},
		wantError: true,
	}, {
		desc: "user exists and valid password case",
		setup: func(ks *keystore) error {
			s := service{ks: ks}
			return s.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &api.EmptyReply{})
		},
		request: &api.UserPass{Username: testUser, Password: password},
		want:    &api.EmptyReply{},
	}, {
		desc: "delete a user, imported from import api case",
		setup: func(ks *keystore) error {
			s := service{ks: ks}

			reply := api.EmptyReply{}
			if err := s.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &reply); err != nil {
				return err
			}

			// created data in bob db
			db, err := ks.GetDatabase(ids.Empty, testUser, password)
			if err != nil {
				return err
			}

			return db.Put([]byte("hello"), []byte("world"))
		},
		request: &api.UserPass{Username: testUser, Password: password},
		want:    &api.EmptyReply{},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ksIntf, err := CreateTestKeystore()
			if err != nil {
				t.Fatal(err)
			}
			ks := ksIntf.(*keystore)
			s := service{ks: ks}

			if tt.setup != nil {
				if err := tt.setup(ks); err != nil {
					t.Fatalf("failed to create user setup in keystore: %v", err)
				}
			}
			got := &api.EmptyReply{}
			err = s.DeleteUser(nil, tt.request, got)
			if (err != nil) != tt.wantError {
				t.Fatalf("DeleteUser() failed: error %v, wantError %v", err, tt.wantError)
			}

			if !tt.wantError && !reflect.DeepEqual(tt.want, got) {
				t.Fatalf("DeleteUser() failed: got %v, want %v", got, tt.want)
			}

			if err == nil { // delete is successful
				if _, ok := ks.usernameToPassword[testUser]; ok {
					t.Fatalf("DeleteUser() failed: expected the user %s should be delete from users map", testUser)
				}

				// deleted user details should be available to create user again.
				err := s.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &api.EmptyReply{})
				if err != nil {
					t.Fatalf("failed to create user: %v", err)
				}
			}
		})
	}
}
