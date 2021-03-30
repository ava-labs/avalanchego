// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/encdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"

	jsoncodec "github.com/ava-labs/avalanchego/utils/json"
)

const (
	// maxUserLen is the maximum allowed length of a username
	maxUserLen = 1024
)

var (
	errEmptyUsername = errors.New("empty username")
	errUserMaxLength = fmt.Errorf("username exceeds maximum length of %d chars", maxUserLen)
)

type Keystore interface {
	// Create the API endpoints for this keystore.
	CreateHandler() (*common.HTTPHandler, error)

	// NewBlockchainKeyStore returns this keystore limiting the functionality to
	// a single blockchain database.
	NewBlockchainKeyStore(blockchainID ids.ID) BlockchainKeystore

	// Get a database that is able to read and write unencrypted values from the
	// underlying database.
	GetDatabase(bID ids.ID, username, password string) (*encdb.Database, error)

	// Get the underlying database that is able to read and write encrypted
	// values. This Database will not perform any encrypting or decrypting of
	// values and is not recommended to be used when implementing a VM.
	GetRawDatabase(bID ids.ID, username, password string) (database.Database, error)

	// AddUser attempts to register this username and password as a new user of
	// the keystore.
	AddUser(username, pw string) error

	// DeleteUser attempts to remove the provided username and all of its data
	// from the keystore.
	DeleteUser(username, pw string) error

	getPassword(username string) (*password.Hash, error)
}

type kvPair struct {
	Key   []byte `serialize:"true"`
	Value []byte `serialize:"true"`
}

// user describes the full content of a user
type user struct {
	password.Hash `serialize:"true"`
	Data          []kvPair `serialize:"true"`
}

// keystore implements keystore management logic
type keystore struct {
	lock sync.Mutex
	log  logging.Logger

	// Key: username
	// Value: The hash of that user's password
	usernameToPassword map[string]*password.Hash

	// Used to persist users and their data
	userDB database.Database
	bcDB   database.Database
	//           BaseDB
	//          /      \
	//    UserDB        BlockchainDB
	//                 /      |     \
	//               Usr     Usr    Usr
	//             /  |  \
	//          BID  BID  BID
}

func New(log logging.Logger, db database.Database) Keystore {
	return &keystore{
		log:                log,
		usernameToPassword: make(map[string]*password.Hash),
		userDB:             prefixdb.New([]byte("users"), db),
		bcDB:               prefixdb.New([]byte("bcs"), db),
	}
}

// CreateHandler returns a new service object that can send requests to thisAPI.
func (ks *keystore) CreateHandler() (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(ks, "keystore"); err != nil {
		return nil, err
	}
	return &common.HTTPHandler{LockOptions: common.NoLock, Handler: newServer}, nil
}

// NewBlockchainKeyStore ...
func (ks *keystore) NewBlockchainKeyStore(blockchainID ids.ID) BlockchainKeystore {
	return &blockchainKeystore{
		blockchainID: blockchainID,
		ks:           ks,
	}
}

// GetDatabase ...
func (ks *keystore) GetDatabase(bID ids.ID, username, password string) (*encdb.Database, error) {
	bcDB, err := ks.GetRawDatabase(bID, username, password)
	if err != nil {
		return nil, err
	}
	return encdb.New([]byte(password), bcDB)
}

// GetRawDatabase ...
func (ks *keystore) GetRawDatabase(bID ids.ID, username, password string) (database.Database, error) {
	if username == "" {
		return nil, errEmptyUsername
	}

	ks.lock.Lock()
	defer ks.lock.Unlock()

	passwordHash, err := ks.getPassword(username)
	if err != nil {
		return nil, err
	}
	if !passwordHash.Check(password) {
		return nil, fmt.Errorf("incorrect password for user %q", username)
	}

	userDB := prefixdb.New([]byte(username), ks.bcDB)
	bcDB := prefixdb.NewNested(bID[:], userDB)
	return bcDB, nil
}

func (ks *keystore) AddUser(username, pw string) error {
	if username == "" {
		return errEmptyUsername
	}
	if len(username) > maxUserLen {
		return errUserMaxLength
	}

	ks.lock.Lock()
	defer ks.lock.Unlock()

	if passwordHash, err := ks.getPassword(username); err == nil || passwordHash != nil {
		return fmt.Errorf("user already exists: %s", username)
	}

	if err := password.IsValid(pw, password.OK); err != nil {
		return err
	}

	passwordHash := &password.Hash{}
	if err := passwordHash.Set(pw); err != nil {
		return err
	}

	passwordBytes, err := c.Marshal(codecVersion, passwordHash)
	if err != nil {
		return err
	}

	if err := ks.userDB.Put([]byte(username), passwordBytes); err != nil {
		return err
	}
	ks.usernameToPassword[username] = passwordHash

	return nil
}

func (ks *keystore) DeleteUser(username, pw string) error {
	if username == "" {
		return errEmptyUsername
	}

	ks.lock.Lock()
	defer ks.lock.Unlock()

	// check if user exists and valid user.
	passwordHash, err := ks.getPassword(username)
	switch {
	case err != nil || passwordHash == nil:
		return fmt.Errorf("user doesn't exist: %s", username)
	case !passwordHash.Check(pw):
		return fmt.Errorf("incorrect password for user %q", username)
	}

	userNameBytes := []byte(username)
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
	delete(ks.usernameToPassword, username)
	return nil
}

// Get the password that is used by [username]
func (ks *keystore) getPassword(username string) (*password.Hash, error) {
	// If the user is already in memory, return it
	passwordHash, exists := ks.usernameToPassword[username]
	if exists {
		return passwordHash, nil
	}
	// The user is not in memory; try the database
	userBytes, err := ks.userDB.Get([]byte(username))
	if err != nil { // Most likely bc user doesn't exist in database
		return nil, err
	}

	passwordHash = &password.Hash{}
	_, err = c.Unmarshal(userBytes, passwordHash)
	return passwordHash, err
}
