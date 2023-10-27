package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
)

const (
	chainIDKey = "chainID"
	deleteKey  = "delete"
)

func main() {
	fs := config.BuildFlagSet()
	fs.String(chainIDKey, "", "chain ID of the chain to remove from the database")
	fs.Bool(deleteKey, false, "delete the database")
	v, err := config.BuildViper(fs, os.Args[1:])

	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}

	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	chainIDStr := v.GetString("chainID")

	if chainIDStr == "" {
		fmt.Println("chainID is required")
		os.Exit(1)
	}

	// TODO: Options:
	// 1- either use subnetID restrict deleting primary network subnetID
	// and then determine chainIDs from subnet
	//
	// 2- or use chainID and determine subnetID from chainID
	// and then restrict deleting primary network subnetID
	chainID, err := ids.FromString(chainIDStr)
	if err != nil {
		fmt.Printf("couldn't parse chainID: %s\n", err)
		os.Exit(1)
	}

	nodeConfig, err := config.GetNodeConfig(v)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	// Get database
	dbManager, err := node.NewDatabaseManager(nodeConfig.DatabaseConfig, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		fmt.Printf("couldn't create database: %s\n", err)
		os.Exit(1)
	}

	databaseConfigJSON, err := json.Marshal(nodeConfig.DatabaseConfig)
	if err != nil {
		fmt.Printf("couldn't marshal database config: %s", err)
		os.Exit(1)
	}

	fmt.Printf("Database Config: %s \n", string(databaseConfigJSON))

	prefixDBManager := dbManager.NewPrefixDBManager(chainID[:])

	prefixDB := prefixDBManager.Current().Database

	// iterate over the bootstrapping database
	bootstrappingDB := prefixdb.New([]byte("bs"), prefixDB)
	iterator := bootstrappingDB.NewIterator()
	bootstrapItems := uint64(0)
	bootstrapSize := float64(0)
	for iterator.Next() {
		bootstrapItems++
		bootstrapSize += float64(len(iterator.Key()) + len(iterator.Value()))
		if v.GetBool(deleteKey) {
			if err := bootstrappingDB.Delete(iterator.Key()); err != nil {
				fmt.Printf("couldn't delete key from bootstrappingDB. key: %s: err: %s", iterator.Key(), err)
				iterator.Release()
				os.Exit(1)
			}
		}
	}
	iterator.Release()

	vmDBManager := prefixDBManager.NewPrefixDBManager([]byte("vm"))
	vmDB := vmDBManager.Current().Database
	iterator = vmDB.NewIterator()
	vmItems := uint64(0)
	vmSize := float64(0)
	for iterator.Next() {
		vmItems++
		vmSize += float64(len(iterator.Key()) + len(iterator.Value()))
		if v.GetBool(deleteKey) {
			if err := vmDB.Delete(iterator.Key()); err != nil {
				fmt.Printf("couldn't delete key from vmDB. key: %s: err: %s", iterator.Key(), err)
				iterator.Release()
				os.Exit(1)
			}
		}
	}
	iterator.Release()

	// report results
	fmt.Printf("Bootstrap DB: %d items, %f MB\n", bootstrapItems, bootstrapSize/units.MiB)
	fmt.Printf("VM DB: %d items, %f MB\n", vmItems, vmSize/units.MiB)
	fmt.Printf("Total: %d items, %f MB\n", bootstrapItems+vmItems, (bootstrapSize+vmSize)/units.MiB)
	if v.GetBool(deleteKey) {
		fmt.Println("Deleted chain database")
	}
}
