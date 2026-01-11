// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/migrate"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// Known chain IDs for mainnet
	pChainIDStr = "11111111111111111111111111111111LpoYY"
	cChainIDStr = "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"
	xChainIDStr = "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"
	dfkChainIDStr = "q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi"
)

func main() {
	dbPath := flag.String("db-path", "", "Path to the monolithic database (e.g., /root/.avalanchego/db/mainnet/v1.4.5)")
	dryRun := flag.Bool("dry-run", false, "Don't actually perform migration, just report what would be done")
	verifyOnly := flag.Bool("verify", false, "Only run verification on existing migration")
	flag.Parse()

	if *dbPath == "" {
		fmt.Println("Error: --db-path is required")
		flag.Usage()
		os.Exit(1)
	}

	// Create logger
	logFactory := logging.NewFactory(logging.Config{
		DisplayLevel: logging.Info,
		LogLevel:     logging.Info,
	})
	log, err := logFactory.Make("migrate")
	if err != nil {
		fmt.Printf("Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	log.Info("AvalancheGo Per-Chain Database Migration Tool")
	log.Info("database path", zap.String("path", *dbPath))

	// Parse chain IDs
	pChainID, err := ids.FromString(pChainIDStr)
	if err != nil {
		fmt.Printf("Failed to parse P-chain ID '%s': %v\n", pChainIDStr, err)
		os.Exit(1)
	}
	log.Info("parsed P-chain ID", zap.Stringer("id", pChainID), zap.Bool("isEmpty", pChainID == ids.Empty))

	cChainID, err := ids.FromString(cChainIDStr)
	if err != nil {
		fmt.Printf("Failed to parse C-chain ID '%s': %v\n", cChainIDStr, err)
		os.Exit(1)
	}
	log.Info("parsed C-chain ID", zap.Stringer("id", cChainID))

	xChainID, err := ids.FromString(xChainIDStr)
	if err != nil {
		fmt.Printf("Failed to parse X-chain ID '%s': %v\n", xChainIDStr, err)
		os.Exit(1)
	}
	log.Info("parsed X-chain ID", zap.Stringer("id", xChainID))

	dfkChainID, err := ids.FromString(dfkChainIDStr)
	if err != nil {
		fmt.Printf("Failed to parse DFK chain ID '%s': %v\n", dfkChainIDStr, err)
		os.Exit(1)
	}
	log.Info("parsed DFK chain ID", zap.Stringer("id", dfkChainID))

	chains := []struct {
		id   ids.ID
		name string
	}{
		{pChainID, "P-Chain"},
		{cChainID, "C-Chain"},
		{xChainID, "X-Chain"},
		{dfkChainID, "DFK-Chain"},
	}

	if *verifyOnly {
		log.Info("verification-only mode - opening databases")
		if err := verifyMigration(*dbPath, chains, log); err != nil {
			log.Error("verification failed", zap.Error(err))
			os.Exit(1)
		}
		log.Info("verification completed successfully")
		return
	}

	if *dryRun {
		log.Info("dry-run mode - no changes will be made")
		if err := analyzeMigration(*dbPath, chains, log); err != nil {
			log.Error("analysis failed", zap.Error(err))
			os.Exit(1)
		}
		return
	}

	// Perform actual migration
	log.Info("starting migration - this may take several minutes")
	if err := performMigration(*dbPath, chains, log); err != nil {
		log.Error("migration failed", zap.Error(err))
		os.Exit(1)
	}

	log.Info("migration completed successfully")
	log.Info("verifying migration")

	if err := verifyMigration(*dbPath, chains, log); err != nil {
		log.Error("verification failed", zap.Error(err))
		log.Error("IMPORTANT: Migration completed but verification failed - check logs")
		os.Exit(1)
	}

	log.Info("verification passed - migration successful")
	log.Info("you can now start avalanchego with db-use-per-chain-databases: true")
}

func analyzeMigration(dbPath string, chains []struct{ id ids.ID; name string }, log logging.Logger) error {
	log.Info("opening source database")
	sourceDB, err := leveldb.New(dbPath, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer sourceDB.Close()

	log.Info("analyzing database contents")

	// Count keys per chain
	chainKeyCounts := make(map[ids.ID]int)
	totalKeys := 0
	unknownKeys := 0

	iterator := sourceDB.NewIterator()
	defer iterator.Release()

	for iterator.Next() {
		key := iterator.Key()
		totalKeys++

		if len(key) < 32 {
			unknownKeys++
			continue
		}

		chainIDBytes := key[:32]
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			unknownKeys++
			continue
		}

		chainKeyCounts[chainID]++
	}

	if err := iterator.Error(); err != nil {
		return fmt.Errorf("iterator error: %w", err)
	}

	// Report findings
	log.Info("analysis complete",
		zap.Int("totalKeys", totalKeys),
		zap.Int("unknownKeys", unknownKeys),
	)

	for _, chain := range chains {
		count := chainKeyCounts[chain.id]
		if count > 0 {
			log.Info("chain found",
				zap.String("name", chain.name),
				zap.Stringer("chainID", chain.id),
				zap.Int("keys", count),
			)
		}
	}

	return nil
}

func performMigration(dbPath string, chains []struct{ id ids.ID; name string }, log logging.Logger) error {
	log.Info("opening source database")
	sourceDB, err := leveldb.New(dbPath, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer sourceDB.Close()

	// Create migrator
	migrator := migrate.NewPerChainMigrator(sourceDB, log)

	// Create target databases for each chain
	targetDBs := make(map[ids.ID]database.Database)
	defer func() {
		for _, db := range targetDBs {
			db.Close()
		}
	}()

	for _, chain := range chains {
		targetPath := filepath.Join(dbPath, chain.id.String())
		log.Info("creating target database",
			zap.String("name", chain.name),
			zap.String("path", targetPath),
		)

		targetDB, err := leveldb.New(targetPath, nil, logging.NoLog{}, prometheus.NewRegistry())
		if err != nil {
			return fmt.Errorf("failed to create target database for %s: %w", chain.name, err)
		}

		targetDBs[chain.id] = targetDB

		if err := migrator.RegisterChainDB(chain.id, targetDB); err != nil {
			return fmt.Errorf("failed to register chain %s: %w", chain.name, err)
		}
	}

	// Perform migration
	log.Info("starting migration - this will take several minutes")
	if err := migrator.Migrate(); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Get statistics
	processed, copied, skipped := migrator.GetStatistics()
	log.Info("migration statistics",
		zap.Int("keysProcessed", processed),
		zap.Int("keysCopied", copied),
		zap.Int("keysSkipped", skipped),
	)

	return nil
}

func verifyMigration(dbPath string, chains []struct{ id ids.ID; name string }, log logging.Logger) error {
	log.Info("opening source database for verification")
	sourceDB, err := leveldb.New(dbPath, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer sourceDB.Close()

	// Create migrator
	migrator := migrate.NewPerChainMigrator(sourceDB, log)

	// Open target databases
	targetDBs := make(map[ids.ID]database.Database)
	defer func() {
		for _, db := range targetDBs {
			db.Close()
		}
	}()

	for _, chain := range chains {
		targetPath := filepath.Join(dbPath, chain.id.String())

		// Check if target exists
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			log.Info("target database does not exist (chain may not have data)",
				zap.String("name", chain.name),
			)
			continue
		}

		targetDB, err := leveldb.New(targetPath, nil, logging.NoLog{}, prometheus.NewRegistry())
		if err != nil {
			return fmt.Errorf("failed to open target database for %s: %w", chain.name, err)
		}

		targetDBs[chain.id] = targetDB

		if err := migrator.RegisterChainDB(chain.id, targetDB); err != nil {
			return fmt.Errorf("failed to register chain %s: %w", chain.name, err)
		}
	}

	// Verify
	return migrator.VerifyMigration()
}
