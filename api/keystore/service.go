// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/encdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/codec"

	jsoncodec "github.com/ava-labs/gecko/utils/json"
	zxcvbn "github.com/nbutton23/zxcvbn-go"
)

const (
	// maxUserPassLen is the maximum length of the username or password allowed
	maxUserPassLen = 1024

	// requiredPassScore defines the score a password must achieve to be accepted
	// as a password with strong characteristics by the zxcvbn package
	//
	// The scoring mechanism defined is as follows;
	//
	// 0 # too guessable: risky password. (guesses < 10^3)
	// 1 # very guessable: protection from throttled online attacks. (guesses < 10^6)
	// 2 # somewhat guessable: protection from unthrottled online attacks. (guesses < 10^8)
	// 3 # safely unguessable: moderate protection from offline slow-hash scenario. (guesses < 10^10)
	// 4 # very unguessable: strong protection from offline slow-hash scenario. (guesses >= 10^10)
	requiredPassScore = 2
)

var (
	errEmptyUsername     = errors.New("username can't be the empty string")
	errUserPassMaxLength = fmt.Errorf("CreateUser call rejected due to username or password exceeding maximum length of %d chars", maxUserPassLen)
	errWeakPassword      = errors.New("Failed to create user as the given password is too weak. A stronger password is one of 8 or more characters containing attributes of upper and lowercase letters, numbers, and/or special characters")
)

// KeyValuePair ...
type KeyValuePair struct {
	Key   []byte `serialize:"true"`
	Value []byte `serialize:"true"`
}

// UserDB describes the full content of a user
type UserDB struct {
	User `serialize:"true"`
	Data []KeyValuePair `serialize:"true"`
}

// Keystore is the RPC interface for keystore management
type Keystore struct {
	lock sync.Mutex
	log  logging.Logger

	codec codec.Codec

	// Key: username
	// Value: The user with that name
	users map[string]*User

	// Used to persist users and their data
	userDB database.Database
	bcDB   database.Database
	//           BaseDB
	//          /      \
	//    UserDB        BlockchainDB
	//                 /      |     \
	//               Usr     Usr    Usr
	//            /   |   \
	//          BID  BID  BID
}

// Initialize the keystore
func (ks *Keystore) Initialize(log logging.Logger, db database.Database) {
	ks.log = log
	ks.codec = codec.NewDefault()
	ks.users = make(map[string]*User)
	ks.userDB = prefixdb.New([]byte("users"), db)
	ks.bcDB = prefixdb.New([]byte("bcs"), db)
}

// CreateHandler returns a new service object that can send requests to thisAPI.
func (ks *Keystore) CreateHandler() *common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(ks, "keystore")
	return &common.HTTPHandler{LockOptions: common.NoLock, Handler: newServer}
}

// Get the user whose name is [username]
func (ks *Keystore) getUser(username string) (*User, error) {
	// If the user is already in memory, return it
	usr, exists := ks.users[username]
	if exists {
		return usr, nil
	}
	// The user is not in memory; try the database
	usrBytes, err := ks.userDB.Get([]byte(username))
	if err != nil { // Most likely bc user doesn't exist in database
		return nil, err
	}

	usr = &User{}
	return usr, ks.codec.Unmarshal(usrBytes, usr)
}

// CreateUserArgs are arguments for passing into CreateUser requests
type CreateUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreateUserReply is the response from calling CreateUser
type CreateUserReply struct {
	Success bool `json:"success"`
}

// CreateUser creates an empty user with the provided username and password
func (ks *Keystore) CreateUser(_ *http.Request, args *CreateUserArgs, reply *CreateUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("CreateUser called with %.*s", maxUserPassLen, args.Username)

	if len(args.Username) > maxUserPassLen || len(args.Password) > maxUserPassLen {
		return errUserPassMaxLength
	}

	if args.Username == "" {
		return errEmptyUsername
	}
	if usr, err := ks.getUser(args.Username); err == nil || usr != nil {
		return fmt.Errorf("user already exists: %s", args.Username)
	}

	if zxcvbn.PasswordStrength(args.Password, nil).Score < requiredPassScore {
		return errWeakPassword
	}

	usr := &User{}
	if err := usr.Initialize(args.Password); err != nil {
		return err
	}

	usrBytes, err := ks.codec.Marshal(usr)
	if err != nil {
		return err
	}

	if err := ks.userDB.Put([]byte(args.Username), usrBytes); err != nil {
		return err
	}
	ks.users[args.Username] = usr
	reply.Success = true
	return nil
}

// ListUsersArgs are the arguments to ListUsers
type ListUsersArgs struct{}

// ListUsersReply is the reply from ListUsers
type ListUsersReply struct {
	Users []string `json:"users"`
}

// ListUsers lists all the registered usernames
func (ks *Keystore) ListUsers(_ *http.Request, args *ListUsersArgs, reply *ListUsersReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("ListUsers called")

	reply.Users = []string{}

	it := ks.userDB.NewIterator()
	defer it.Release()
	for it.Next() {
		reply.Users = append(reply.Users, string(it.Key()))
	}
	return it.Error()
}

// ExportUserArgs are the arguments to ExportUser
type ExportUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ExportUserReply is the reply from ExportUser
type ExportUserReply struct {
	User formatting.CB58 `json:"user"`
}

// ExportUser exports a serialized encoding of a user's information complete with encrypted database values
func (ks *Keystore) ExportUser(_ *http.Request, args *ExportUserArgs, reply *ExportUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("ExportUser called for %s", args.Username)

	usr, err := ks.getUser(args.Username)
	if err != nil {
		return err
	}
	if !usr.CheckPassword(args.Password) {
		return fmt.Errorf("incorrect password for user %q", args.Username)
	}

	userDB := prefixdb.New([]byte(args.Username), ks.bcDB)

	userData := UserDB{
		User: *usr,
	}

	it := userDB.NewIterator()
	defer it.Release()
	for it.Next() {
		userData.Data = append(userData.Data, KeyValuePair{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	if err := it.Error(); err != nil {
		return err
	}

	b, err := ks.codec.Marshal(&userData)
	if err != nil {
		return err
	}
	reply.User.Bytes = b
	return nil
}

// ImportUserArgs are arguments for ImportUser
type ImportUserArgs struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	User     formatting.CB58 `json:"user"`
}

// ImportUserReply is the response for ImportUser
type ImportUserReply struct {
	Success bool `json:"success"`
}

// ImportUser imports a serialized encoding of a user's information complete with encrypted database values, integrity checks the password, and adds it to the database
func (ks *Keystore) ImportUser(r *http.Request, args *ImportUserArgs, reply *ImportUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("ImportUser called for %s", args.Username)

	if usr, err := ks.getUser(args.Username); err == nil || usr != nil {
		return fmt.Errorf("user already exists: %s", args.Username)
	}

	userData := UserDB{}
	if err := ks.codec.Unmarshal(args.User.Bytes, &userData); err != nil {
		return err
	}
	if !userData.User.CheckPassword(args.Password) {
		return fmt.Errorf("incorrect password for user %q", args.Username)
	}

	usrBytes, err := ks.codec.Marshal(&userData.User)
	if err != nil {
		return err
	}

	userBatch := ks.userDB.NewBatch()
	if err := userBatch.Put([]byte(args.Username), usrBytes); err != nil {
		return err
	}

	userDataDB := prefixdb.New([]byte(args.Username), ks.bcDB)
	dataBatch := userDataDB.NewBatch()
	for _, kvp := range userData.Data {
		dataBatch.Put(kvp.Key, kvp.Value)
	}

	if err := atomic.WriteAll(dataBatch, userBatch); err != nil {
		return err
	}

	ks.users[args.Username] = &userData.User

	reply.Success = true
	return nil
}

// DeleteUserArgs are arguments for passing into DeleteUser requests
type DeleteUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// DeleteUserReply is the response from calling DeleteUser
type DeleteUserReply struct {
	Success bool `json:"success"`
}

// DeleteUser deletes user with the provided username and password.
func (ks *Keystore) DeleteUser(_ *http.Request, args *DeleteUserArgs, reply *DeleteUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("DeleteUser called with %s", args.Username)

	if args.Username == "" {
		return errEmptyUsername
	}

	// check if user exists and valid user.
	usr, err := ks.getUser(args.Username)
	switch {
	case err != nil || usr == nil:
		return fmt.Errorf("user doesn't exist: %s", args.Username)
	case !usr.CheckPassword(args.Password):
		return fmt.Errorf("incorrect password for user %q", args.Username)
	}

	userNameBytes := []byte(args.Username)
	userBatch := ks.userDB.NewBatch()
	if err := userBatch.Delete(userNameBytes); err != nil {
		return err
	}

	userDataDB := prefixdb.New(userNameBytes, ks.bcDB)
	dataBatch := userDataDB.NewBatch()

	it := userDataDB.NewIterator()
	defer it.Release()

	for it.Next() {
		if err = dataBatch.Delete(it.Key()); err != nil {
			return err
		}
	}

	if err = it.Error(); err != nil {
		return err
	}

	if err := atomic.WriteAll(dataBatch, userBatch); err != nil {
		return err
	}

	// delete from users map.
	delete(ks.users, args.Username)

	reply.Success = true
	return nil
}

// NewBlockchainKeyStore ...
func (ks *Keystore) NewBlockchainKeyStore(blockchainID ids.ID) *BlockchainKeystore {
	return &BlockchainKeystore{
		blockchainID: blockchainID,
		ks:           ks,
	}
}

// GetDatabase ...
func (ks *Keystore) GetDatabase(bID ids.ID, username, password string) (database.Database, error) {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	usr, err := ks.getUser(username)
	if err != nil {
		return nil, err
	}
	if !usr.CheckPassword(password) {
		return nil, fmt.Errorf("incorrect password for user %q", username)
	}

	userDB := prefixdb.New([]byte(username), ks.bcDB)
	bcDB := prefixdb.NewNested(bID.Bytes(), userDB)
	encDB, err := encdb.New([]byte(password), bcDB)

	if err != nil {
		return nil, err
	}

	return encDB, nil
}
