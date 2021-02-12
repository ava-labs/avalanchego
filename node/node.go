// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ava-labs/coreth/accounts"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/prometheus/tsdb/fileutil"
)

// Node is a container on which services can be registered.
type Node struct {
	eventmux      *event.TypeMux
	config        *Config
	accman        *accounts.Manager
	log           log.Logger
	ephemKeystore string            // if non-empty, the key directory that will be removed by Stop
	dirLock       fileutil.Releaser // prevents concurrent use of instance directory
	stop          chan struct{}     // Channel to wait for termination notifications
	server        *p2p.Server       // Currently running P2P networking layer
	startStopLock sync.Mutex        // Start/Stop are protected by an additional lock
	state         int               // Tracks state of node lifecycle

	lock          sync.Mutex
	rpcAPIs       []rpc.API   // List of APIs currently provided by the node
	inprocHandler *rpc.Server // In-process RPC request handler to process the API requests

	databases map[*closeTrackingDB]struct{} // All open databases
}

const (
	initializingState = iota
	closedState
)

func (n *Node) openDataDir() error {
	if n.config.DataDir == "" {
		return nil // ephemeral
	}

	instdir := filepath.Join(n.config.DataDir, n.config.name())
	if err := os.MkdirAll(instdir, 0700); err != nil {
		return err
	}
	// Lock the instance directory to prevent concurrent use by another instance as well as
	// accidental use of the instance directory as a database.
	release, _, err := fileutil.Flock(filepath.Join(instdir, "LOCK"))
	if err != nil {
		return convertFileLockError(err)
	}
	n.dirLock = release
	return nil
}

// New creates a new P2P node, ready for protocol registration.
func New(conf *Config) (*Node, error) {
	// Copy config and resolve the datadir so future changes to the current
	// working directory don't affect the node.
	confCopy := *conf
	conf = &confCopy
	if conf.DataDir != "" {
		absdatadir, err := filepath.Abs(conf.DataDir)
		if err != nil {
			return nil, err
		}
		conf.DataDir = absdatadir
	}
	if conf.Logger == nil {
		conf.Logger = log.New()
	}

	// Ensure that the instance name doesn't cause weird conflicts with
	// other files in the data directory.
	if strings.ContainsAny(conf.Name, `/\`) {
		return nil, errors.New(`Config.Name must not contain '/' or '\'`)
	}
	if conf.Name == datadirDefaultKeyStore {
		return nil, errors.New(`Config.Name cannot be "` + datadirDefaultKeyStore + `"`)
	}
	if strings.HasSuffix(conf.Name, ".ipc") {
		return nil, errors.New(`Config.Name cannot end in ".ipc"`)
	}

	node := &Node{
		config:        conf,
		inprocHandler: rpc.NewServer(0),
		eventmux:      new(event.TypeMux),
		log:           conf.Logger,
		stop:          make(chan struct{}),
		// server:        &p2p.Server{Config: conf.P2P},
		databases: make(map[*closeTrackingDB]struct{}),
	}

	// Register built-in APIs.
	node.rpcAPIs = append(node.rpcAPIs, node.apis()...)

	// Acquire the instance directory lock.
	if err := node.openDataDir(); err != nil {
		return nil, err
	}
	// Ensure that the AccountManager method works before the node has started. We rely on
	// this in cmd/geth.
	am, ephemeralKeystore, err := makeAccountManager(conf)
	if err != nil {
		return nil, err
	}
	node.accman = am
	node.ephemKeystore = ephemeralKeystore

	// // Initialize the p2p server. This creates the node key and discovery databases.
	// node.server.Config.PrivateKey = node.config.NodeKey()
	// node.server.Config.Name = node.config.NodeName()
	// node.server.Config.Logger = node.log
	// if node.server.Config.StaticNodes == nil {
	// 	node.server.Config.StaticNodes = node.config.StaticNodes()
	// }
	// if node.server.Config.TrustedNodes == nil {
	// 	node.server.Config.TrustedNodes = node.config.TrustedNodes()
	// }
	// if node.server.Config.NodeDatabase == "" {
	// 	node.server.Config.NodeDatabase = node.config.NodeDB()
	// }

	// Configure RPC servers.

	return node, nil
}

// Config returns the configuration of node.
func (n *Node) Config() *Config {
	return n.config
}

// Server retrieves the currently running P2P network layer. This method is meant
// only to inspect fields of the currently running server. Callers should not
// start or stop the returned server.
func (n *Node) Server() *p2p.Server {
	// n.lock.Lock()
	// defer n.lock.Unlock()

	// return n.server
	return nil
}

// DataDir retrieves the current datadir used by the protocol stack.
// Deprecated: No files should be stored in this directory, use InstanceDir instead.
func (n *Node) DataDir() string {
	return n.config.DataDir
}

// InstanceDir retrieves the instance directory used by the protocol stack.
func (n *Node) InstanceDir() string {
	return n.config.instanceDir()
}

// AccountManager retrieves the account manager used by the protocol stack.
func (n *Node) AccountManager() *accounts.Manager {
	return n.accman
}

// EventMux retrieves the event multiplexer used by all the network services in
// the current protocol stack.
func (n *Node) EventMux() *event.TypeMux {
	return n.eventmux
}

// OpenDatabase opens an existing database with the given name (or creates one if no
// previous can be found) from within the node's instance directory. If the node is
// ephemeral, a memory database is returned.
func (n *Node) OpenDatabase(name string, cache, handles int, namespace string) (ethdb.Database, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.state == closedState {
		return nil, ErrNodeStopped
	}

	var db ethdb.Database
	var err error
	if n.config.DataDir == "" {
		db = rawdb.NewMemoryDatabase()
	} else {
		db, err = rawdb.NewLevelDBDatabase(n.ResolvePath(name), cache, handles, namespace)
	}

	if err == nil {
		db = n.wrapDatabase(db)
	}
	return db, err
}

// OpenDatabaseWithFreezer opens an existing database with the given name (or
// creates one if no previous can be found) from within the node's data directory,
// also attaching a chain freezer to it that moves ancient chain data from the
// database to immutable append-only files. If the node is an ephemeral one, a
// memory database is returned.
func (n *Node) OpenDatabaseWithFreezer(name string, cache, handles int, freezer, namespace string) (ethdb.Database, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.state == closedState {
		return nil, ErrNodeStopped
	}

	var db ethdb.Database
	var err error
	if n.config.DataDir == "" {
		db = rawdb.NewMemoryDatabase()
	} else {
		root := n.ResolvePath(name)
		switch {
		case freezer == "":
			freezer = filepath.Join(root, "ancient")
		case !filepath.IsAbs(freezer):
			freezer = n.ResolvePath(freezer)
		}
		db, err = rawdb.NewLevelDBDatabaseWithFreezer(root, cache, handles, freezer, namespace)
	}

	if err == nil {
		db = n.wrapDatabase(db)
	}
	return db, err
}

// ResolvePath returns the absolute path of a resource in the instance directory.
func (n *Node) ResolvePath(x string) string {
	return n.config.ResolvePath(x)
}

// closeTrackingDB wraps the Close method of a database. When the database is closed by the
// service, the wrapper removes it from the node's database map. This ensures that Node
// won't auto-close the database if it is closed by the service that opened it.
type closeTrackingDB struct {
	ethdb.Database
	n *Node
}

func (db *closeTrackingDB) Close() error {
	db.n.lock.Lock()
	delete(db.n.databases, db)
	db.n.lock.Unlock()
	return db.Database.Close()
}

// wrapDatabase ensures the database will be auto-closed when Node is closed.
func (n *Node) wrapDatabase(db ethdb.Database) ethdb.Database {
	wrapper := &closeTrackingDB{db, n}
	n.databases[wrapper] = struct{}{}
	return wrapper
}

// closeDatabases closes all open databases.
func (n *Node) closeDatabases() (errors []error) {
	for db := range n.databases {
		delete(n.databases, db)
		if err := db.Database.Close(); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

// RegisterAPIs registers the APIs a service provides on the node.
func (n *Node) RegisterAPIs(apis []rpc.API) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.state != initializingState {
		panic("can't register APIs on running/stopped node")
	}
	n.rpcAPIs = append(n.rpcAPIs, apis...)
}
