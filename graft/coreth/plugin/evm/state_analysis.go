// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
)

// StateAnalysisConfig holds configuration for state analysis
type StateAnalysisConfig struct {
	OutputDir        string           // Directory for JSON output
	ProgressInterval time.Duration    // Interval for progress updates (default 30s)
	Workers          int              // Number of parallel workers (default: 8)
	Addresses        []common.Address // Specific addresses to analyze (empty = all)
}

// RawStateData holds the raw counting results - minimal processing for speed
type RawStateData struct {
	// Metadata
	Timestamp   string `json:"timestamp"`
	StateRoot   string `json:"state_root"`
	BlockNumber uint64 `json:"block_number"`

	// Counts
	TotalAccounts       uint64 `json:"total_accounts"`
	AccountsWithStorage uint64 `json:"accounts_with_storage"`
	AccountsWithCode    uint64 `json:"accounts_with_code"`
	TotalStorageSlots   uint64 `json:"total_storage_slots"`
	TotalStorageBytes   uint64 `json:"total_storage_bytes"` // Total bytes of storage values

	// Raw per-contract data - all contracts with storage
	// This is the key data for post-processing
	Contracts []RawContractData `json:"contracts"`

	// Timing
	AnalysisDurationMs int64 `json:"analysis_duration_ms"`

	// Resume support
	LastProcessedHash string `json:"last_processed_hash"` // Last account hash processed
	Complete          bool   `json:"complete"`            // Whether analysis finished
}

// RawContractData holds minimal per-contract data for speed
type RawContractData struct {
	AddressHash  string `json:"h"`     // Short key name for smaller JSON
	StorageSlots uint64 `json:"slots"` // Short key name for smaller JSON
	StorageBytes uint64 `json:"bytes"` // Total bytes of storage values
	HasCode      bool   `json:"code"`  // Whether contract has code
}

// binaryContractData is the binary representation for fast I/O
// Layout: [32]byte hash + uint64 slots + uint64 bytes + uint8 hasCode = 49 bytes per contract
const binaryRecordSize = 32 + 8 + 8 + 1

// storageResult holds the result from parallel storage counting
type storageResult struct {
	accountHash common.Hash
	slots       uint64
	bytes       uint64
	hasCode     bool
}

// getOutputPath returns the path for the state analysis file
func getOutputPath(outputDir string, stateRoot common.Hash) string {
	if outputDir == "" {
		outputDir = "."
	}
	filename := fmt.Sprintf("state_analysis_%s.json", stateRoot.Hex()[:16])
	return filepath.Join(outputDir, filename)
}

// getBinaryPath returns the path for the binary intermediate file
func getBinaryPath(outputDir string, stateRoot common.Hash) string {
	if outputDir == "" {
		outputDir = "."
	}
	filename := fmt.Sprintf("state_analysis_%s.bin", stateRoot.Hex()[:16])
	return filepath.Join(outputDir, filename)
}

// loadExistingData loads existing state analysis data if available and matching
func loadExistingData(outputPath string, stateRoot common.Hash) (*RawStateData, error) {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var existing RawStateData
	if err := json.Unmarshal(data, &existing); err != nil {
		return nil, fmt.Errorf("failed to parse existing data: %w", err)
	}

	if existing.StateRoot != stateRoot.Hex() {
		log.Info("Existing analysis has different state root, starting fresh",
			"existing", existing.StateRoot,
			"current", stateRoot.Hex(),
		)
		return nil, nil
	}

	if existing.Complete {
		log.Info("Found complete analysis, returning cached result",
			"accounts", existing.TotalAccounts,
			"slots", existing.TotalStorageSlots,
		)
		return &existing, nil
	}

	return &existing, nil
}

// AnalyzeState performs fast state counting using snapshot iterators and parallel workers.
// Falls back to trie iteration if snapshots are unavailable.
func AnalyzeState(bc *core.BlockChain, cfg StateAnalysisConfig) (*RawStateData, error) {
	startTime := time.Now()

	// Set defaults
	if cfg.ProgressInterval <= 0 {
		cfg.ProgressInterval = 30 * time.Second
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "."
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.NumCPU()
		if cfg.Workers > 16 {
			cfg.Workers = 16
		}
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	currentBlock := bc.CurrentBlock()
	stateRoot := currentBlock.Root
	blockNumber := currentBlock.Number.Uint64()
	outputPath := getOutputPath(cfg.OutputDir, stateRoot)
	binaryPath := getBinaryPath(cfg.OutputDir, stateRoot)

	// Try to load existing data
	existing, err := loadExistingData(outputPath, stateRoot)
	if err != nil {
		log.Warn("Failed to load existing data, starting fresh", "err", err)
	}

	// If complete, return cached result (caller will log it)
	if existing != nil && existing.Complete {
		log.Info("Found complete analysis, returning cached result",
			"accounts", existing.TotalAccounts,
			"slots", existing.TotalStorageSlots,
		)
		return existing, nil
	}

	// If we have partial data, analyze and log it before continuing
	if existing != nil && len(existing.Contracts) > 0 {
		log.Info("Found partial state data, analyzing before continuing",
			"accounts", existing.TotalAccounts,
			"slots", existing.TotalStorageSlots,
		)
		// Pass nil for db - address resolution will be done in final analysis
		analyzed := AnalyzeRawData(existing, 100, nil)
		LogAnalyzedResult(analyzed)
	}

	// If specific addresses are provided, analyze only those
	if len(cfg.Addresses) > 0 {
		log.Info("Analyzing specific addresses",
			"count", len(cfg.Addresses),
			"stateRoot", stateRoot.Hex(),
			"blockNumber", blockNumber,
		)
		return analyzeSpecificAddresses(bc, cfg, stateRoot, blockNumber, outputPath, startTime)
	}

	// Try to use snapshot iterators (faster than trie)
	snap := bc.Snapshots()
	if snap == nil {
		log.Warn("Snapshots not available, this will be slower")
		return analyzeStateWithTrie(bc, cfg, stateRoot, blockNumber, outputPath, binaryPath, startTime)
	}

	return analyzeStateWithSnapshot(snap, cfg, stateRoot, blockNumber, outputPath, binaryPath, startTime)
}

// analyzeStateWithSnapshot uses fast snapshot iterators with parallel storage counting
func analyzeStateWithSnapshot(snap *snapshot.Tree, cfg StateAnalysisConfig, stateRoot common.Hash, blockNumber uint64, outputPath, binaryPath string, startTime time.Time) (*RawStateData, error) {
	log.Info("Starting fast state analysis with snapshots",
		"stateRoot", stateRoot.Hex(),
		"blockNumber", blockNumber,
		"workers", cfg.Workers,
	)

	result := &RawStateData{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		StateRoot:   stateRoot.Hex(),
		BlockNumber: blockNumber,
		Contracts:   make([]RawContractData, 0, 1000000),
	}

	// Open binary file for append-only writes
	binFile, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open binary file: %w", err)
	}
	defer binFile.Close()

	// Track existing data from binary file
	existingHashes := make(map[common.Hash]struct{})
	if binData, err := os.ReadFile(binaryPath); err == nil && len(binData) > 0 {
		for i := 0; i+binaryRecordSize <= len(binData); i += binaryRecordSize {
			hash := common.BytesToHash(binData[i : i+32])
			existingHashes[hash] = struct{}{}
			slots := binary.LittleEndian.Uint64(binData[i+32 : i+40])
			bytes := binary.LittleEndian.Uint64(binData[i+40 : i+48])
			hasCode := binData[i+48] == 1

			result.TotalAccounts++
			result.AccountsWithStorage++
			result.TotalStorageSlots += slots
			result.TotalStorageBytes += bytes
			if hasCode {
				result.AccountsWithCode++
			}
			result.Contracts = append(result.Contracts, RawContractData{
				AddressHash:  hash.Hex(),
				StorageSlots: slots,
				StorageBytes: bytes,
				HasCode:      hasCode,
			})
		}
		if len(existingHashes) > 0 {
			log.Info("Resuming from binary checkpoint",
				"accountsLoaded", len(existingHashes),
				"slotsLoaded", result.TotalStorageSlots,
			)
		}
	}

	// Create parallel workers for storage counting
	workers := utils.NewBoundedWorkers(cfg.Workers)
	resultsChan := make(chan storageResult, 10000)

	// Atomic counters for progress
	var accountsProcessed atomic.Uint64
	var pendingWork atomic.Int64

	// Result collector goroutine
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	var binMu sync.Mutex
	binBuf := make([]byte, binaryRecordSize)

	go func() {
		defer collectorWg.Done()
		for res := range resultsChan {
			result.TotalStorageSlots += res.slots
			result.TotalStorageBytes += res.bytes
			result.AccountsWithStorage++
			if res.hasCode {
				result.AccountsWithCode++
			}
			result.Contracts = append(result.Contracts, RawContractData{
				AddressHash:  res.accountHash.Hex(),
				StorageSlots: res.slots,
				StorageBytes: res.bytes,
				HasCode:      res.hasCode,
			})

			// Write to binary file
			copy(binBuf[:32], res.accountHash[:])
			binary.LittleEndian.PutUint64(binBuf[32:40], res.slots)
			binary.LittleEndian.PutUint64(binBuf[40:48], res.bytes)
			if res.hasCode {
				binBuf[48] = 1
			} else {
				binBuf[48] = 0
			}
			binMu.Lock()
			binFile.Write(binBuf)
			binMu.Unlock()

			pendingWork.Add(-1)
		}
	}()

	// Iterate accounts using snapshot
	accIt := snap.DiskAccountIterator(common.Hash{})
	lastLogTime := time.Now()
	var lastAccountHash common.Hash

	for accIt.Next() {
		accountHash := accIt.Hash()
		lastAccountHash = accountHash
		accountsProcessed.Add(1)
		result.TotalAccounts++

		// Skip already processed
		if _, exists := existingHashes[accountHash]; exists {
			continue
		}

		// Check if account has storage by trying to iterate
		// Dispatch to worker pool
		pendingWork.Add(1)
		workers.Execute(func() {
			stats := countStorageWithSnapshot(snap, accountHash)
			if stats.slots > 0 {
				resultsChan <- storageResult{
					accountHash: accountHash,
					slots:       stats.slots,
					bytes:       stats.bytes,
					hasCode:     false, // Can't determine from snapshot alone
				}
			} else {
				pendingWork.Add(-1)
			}
		})

		// Progress logging
		if time.Since(lastLogTime) > cfg.ProgressInterval {
			elapsed := time.Since(startTime)
			processed := accountsProcessed.Load()
			rate := float64(processed) / elapsed.Seconds()

			log.Info("State analysis progress",
				"accounts", processed,
				"withStorage", result.AccountsWithStorage,
				"totalSlots", result.TotalStorageSlots,
				"totalSize", formatBytes(result.TotalStorageBytes),
				"pending", pendingWork.Load(),
				"rate", fmt.Sprintf("%.0f/s", rate),
				"elapsed", elapsed.Round(time.Second),
			)
			lastLogTime = time.Now()
		}
	}
	accIt.Release()

	if err := accIt.Error(); err != nil {
		return nil, fmt.Errorf("account iterator error: %w", err)
	}

	// Wait for all workers to finish
	workers.Wait()

	// Wait for pending results to be processed
	for pendingWork.Load() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	close(resultsChan)
	collectorWg.Wait()

	// Mark complete
	result.Complete = true
	result.LastProcessedHash = lastAccountHash.Hex()
	result.AnalysisDurationMs = time.Since(startTime).Milliseconds()

	// Write final JSON
	if err := writeJSON(result, outputPath); err != nil {
		log.Warn("Failed to write final JSON", "err", err)
	}

	// Clean up binary file
	os.Remove(binaryPath)

	log.Info("State analysis complete",
		"accounts", result.TotalAccounts,
		"withStorage", result.AccountsWithStorage,
		"withCode", result.AccountsWithCode,
		"totalSlots", result.TotalStorageSlots,
		"totalSize", formatBytes(result.TotalStorageBytes),
		"duration", time.Since(startTime).Round(time.Second),
	)

	return result, nil
}

// storageStats holds slot count and byte size
type storageStats struct {
	slots uint64
	bytes uint64
}

// countStorageWithSnapshot counts storage using snapshot iterator (fastest)
func countStorageWithSnapshot(snap *snapshot.Tree, accountHash common.Hash) storageStats {
	storageIt := snap.DiskStorageIterator(accountHash, common.Hash{})
	defer storageIt.Release()

	var stats storageStats
	for storageIt.Next() {
		stats.slots++
		stats.bytes += uint64(len(storageIt.Slot()))
	}
	return stats
}

// analyzeSpecificAddresses analyzes only the specified addresses
func analyzeSpecificAddresses(bc *core.BlockChain, cfg StateAnalysisConfig, stateRoot common.Hash, blockNumber uint64, outputPath string, startTime time.Time) (*RawStateData, error) {
	result := &RawStateData{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		StateRoot:   stateRoot.Hex(),
		BlockNumber: blockNumber,
		Contracts:   make([]RawContractData, 0, len(cfg.Addresses)),
	}

	snap := bc.Snapshots()
	trieDB := bc.TrieDB()

	for i, addr := range cfg.Addresses {
		// Compute account hash (keccak256 of address)
		accountHash := crypto.Keccak256Hash(addr.Bytes())
		result.TotalAccounts++

		var stats storageStats

		// Try snapshot first, fall back to trie
		if false {
			log.Info("Using snapshot for storage counting", "address", addr.Hex())
			stats = countStorageWithSnapshot(snap, accountHash)
		} else {
			log.Info("Using trie for storage counting", "address", addr.Hex())
			// Need to get storage root from account - use state
			stateDB, stateErr := bc.StateAt(stateRoot)
			if stateErr != nil {
				log.Warn("Failed to get state", "err", stateErr, "address", addr.Hex())
				continue
			}
			storageRoot := stateDB.GetStorageRoot(addr)
			if storageRoot != (common.Hash{}) && storageRoot != types.EmptyRootHash {
				var countErr error
				stats, countErr = countStorageWithTrie(trieDB, stateRoot, accountHash, storageRoot)
				if countErr != nil {
					log.Warn("Failed to count storage", "err", countErr, "address", addr.Hex())
					continue
				}
			}
		}

		if stats.slots > 0 {
			result.AccountsWithStorage++
			result.TotalStorageSlots += stats.slots
			result.TotalStorageBytes += stats.bytes

			result.Contracts = append(result.Contracts, RawContractData{
				AddressHash:  accountHash.Hex(),
				StorageSlots: stats.slots,
				StorageBytes: stats.bytes,
				HasCode:      true, // Assume has code since user specified it
			})

			log.Info("Analyzed address",
				"index", i+1,
				"total", len(cfg.Addresses),
				"address", addr.Hex(),
				"slots", stats.slots,
				"size", formatBytes(stats.bytes),
			)
		} else {
			log.Info("Address has no storage",
				"index", i+1,
				"total", len(cfg.Addresses),
				"address", addr.Hex(),
			)
		}
	}

	result.Complete = true
	result.AnalysisDurationMs = time.Since(startTime).Milliseconds()

	// Write JSON
	if err := writeJSON(result, outputPath); err != nil {
		log.Warn("Failed to write JSON", "err", err)
	}

	log.Info("Specific address analysis complete",
		"addressesAnalyzed", len(cfg.Addresses),
		"withStorage", result.AccountsWithStorage,
		"totalSlots", result.TotalStorageSlots,
		"totalSize", formatBytes(result.TotalStorageBytes),
		"duration", time.Since(startTime).Round(time.Second),
	)

	return result, nil
}

// analyzeStateWithTrie is the fallback when snapshots aren't available
func analyzeStateWithTrie(bc *core.BlockChain, cfg StateAnalysisConfig, stateRoot common.Hash, blockNumber uint64, outputPath, binaryPath string, startTime time.Time) (*RawStateData, error) {
	log.Warn("Falling back to trie iteration (slower)",
		"stateRoot", stateRoot.Hex(),
		"blockNumber", blockNumber,
	)

	// Import trie packages only when needed
	// This is a simplified version - just iterate trie directly
	result := &RawStateData{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		StateRoot:   stateRoot.Hex(),
		BlockNumber: blockNumber,
		Contracts:   make([]RawContractData, 0, 1000000),
	}

	// Use the existing trie-based implementation
	trieDB := bc.TrieDB()

	// Import trie package
	stateTrie, err := newStateTrie(stateRoot, trieDB)
	if err != nil {
		return nil, fmt.Errorf("failed to create state trie: %w", err)
	}

	accNodeIter, err := stateTrie.NodeIterator(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create account iterator: %w", err)
	}

	var accountsProcessed uint64
	lastLogTime := time.Now()

	for accNodeIter.Next(true) {
		if !accNodeIter.Leaf() {
			continue
		}

		acc, err := decodeAccount(accNodeIter.LeafBlob())
		if err != nil {
			continue
		}

		accountHash := common.BytesToHash(accNodeIter.LeafKey())
		result.TotalAccounts++
		accountsProcessed++

		hasCode := isAccountWithCode(acc)
		if hasCode {
			result.AccountsWithCode++
		}

		if hasStorageRoot(acc) {
			result.AccountsWithStorage++

			stats, err := countStorageWithTrie(trieDB, stateRoot, accountHash, getStorageRoot(acc))
			if err != nil {
				continue
			}

			result.TotalStorageSlots += stats.slots
			result.TotalStorageBytes += stats.bytes

			result.Contracts = append(result.Contracts, RawContractData{
				AddressHash:  accountHash.Hex(),
				StorageSlots: stats.slots,
				StorageBytes: stats.bytes,
				HasCode:      hasCode,
			})
		}

		if time.Since(lastLogTime) > cfg.ProgressInterval {
			elapsed := time.Since(startTime)
			rate := float64(accountsProcessed) / elapsed.Seconds()

			log.Info("State analysis progress (trie)",
				"accounts", accountsProcessed,
				"withStorage", result.AccountsWithStorage,
				"totalSlots", result.TotalStorageSlots,
				"rate", fmt.Sprintf("%.0f/s", rate),
				"elapsed", elapsed.Round(time.Second),
			)
			lastLogTime = time.Now()
		}
	}

	if err := accNodeIter.Error(); err != nil {
		return nil, fmt.Errorf("account iterator error: %w", err)
	}

	result.Complete = true
	result.AnalysisDurationMs = time.Since(startTime).Milliseconds()

	if err := writeJSON(result, outputPath); err != nil {
		log.Warn("Failed to write final JSON", "err", err)
	}

	log.Info("State analysis complete (trie)",
		"accounts", result.TotalAccounts,
		"withStorage", result.AccountsWithStorage,
		"totalSlots", result.TotalStorageSlots,
		"duration", time.Since(startTime).Round(time.Second),
	)

	return result, nil
}

// writeJSON writes the result to a JSON file atomically
func writeJSON(data *RawStateData, outputPath string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	tmpPath := outputPath + ".tmp"
	if err := os.WriteFile(tmpPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	log.Info("State analysis data written", "path", outputPath, "size", len(jsonData))
	return nil
}

// WriteRawData writes the raw state data to a JSON file (legacy function for compatibility)
func WriteRawData(data *RawStateData, outputDir string) error {
	outputPath := getOutputPath(outputDir, common.HexToHash(data.StateRoot))
	return writeJSON(data, outputPath)
}

// ============================================================================
// Post-processing functions - run these on the JSON output, not during counting
// ============================================================================

// AnalyzedResult holds derived metrics from raw data
type AnalyzedResult struct {
	RawStateData

	// Derived metrics
	TopContracts        []ContractSummary `json:"top_contracts"`
	StorageDistribution map[string]uint64 `json:"storage_distribution"`

	// EIP-8032 transition estimate
	TransitionEstimate TransitionEstimate `json:"transition_estimate"`
}

// ContractSummary holds per-contract summary for output
type ContractSummary struct {
	AddressHash    string  `json:"address_hash"`
	Address        string  `json:"address,omitempty"` // Resolved address from preimage (if available)
	StorageSlots   uint64  `json:"storage_slots"`
	StorageBytes   uint64  `json:"storage_bytes"`
	HasCode        bool    `json:"has_code"`
	SlotPercentage float64 `json:"slot_percentage"`
	BytePercentage float64 `json:"byte_percentage"`
}

// TransitionEstimate holds EIP-8032 transition time estimates
type TransitionEstimate struct {
	TotalSlots       uint64 `json:"total_slots"`
	SlotsPerBlock    uint64 `json:"slots_per_block"`
	BlockTimeSeconds uint64 `json:"block_time_seconds"`
	EstimatedBlocks  uint64 `json:"estimated_blocks"`
	EstimatedHours   uint64 `json:"estimated_hours"`
}

// AnalyzeRawData processes raw state data to derive metrics
// If db is provided, it will attempt to resolve address hashes to actual addresses via preimage lookup
func AnalyzeRawData(raw *RawStateData, topN int, db ethdb.Database) *AnalyzedResult {
	result := &AnalyzedResult{
		RawStateData:        *raw,
		StorageDistribution: make(map[string]uint64),
	}

	buckets := []string{"<10", "10-100", "100-1K", "1K-10K", "10K-100K", "100K-1M", "1M-10M", "10M+"}
	for _, b := range buckets {
		result.StorageDistribution[b] = 0
	}

	for _, c := range raw.Contracts {
		bucket := getStorageBucket(c.StorageSlots)
		result.StorageDistribution[bucket]++
	}

	if topN > 0 && len(raw.Contracts) > 0 {
		sorted := make([]RawContractData, len(raw.Contracts))
		copy(sorted, raw.Contracts)

		if topN < len(sorted) {
			partialSort(sorted, topN)
			sorted = sorted[:topN]
		} else {
			quickSortContracts(sorted, 0, len(sorted)-1)
		}

		result.TopContracts = make([]ContractSummary, len(sorted))
		for i, c := range sorted {
			slotPct := float64(0)
			bytePct := float64(0)
			if raw.TotalStorageSlots > 0 {
				slotPct = float64(c.StorageSlots) * 100 / float64(raw.TotalStorageSlots)
			}
			if raw.TotalStorageBytes > 0 {
				bytePct = float64(c.StorageBytes) * 100 / float64(raw.TotalStorageBytes)
			}

			// Try to resolve address from preimage
			var address string
			if db != nil {
				hash := common.HexToHash(c.AddressHash)
				if preimage := rawdb.ReadPreimage(db, hash); preimage != nil && len(preimage) == 20 {
					address = common.BytesToAddress(preimage).Hex()
				}
			}

			result.TopContracts[i] = ContractSummary{
				AddressHash:    c.AddressHash,
				Address:        address,
				StorageSlots:   c.StorageSlots,
				StorageBytes:   c.StorageBytes,
				HasCode:        c.HasCode,
				SlotPercentage: slotPct,
				BytePercentage: bytePct,
			}
		}
	}

	const (
		slotsPerBlock   = 1000
		blockTimeSecond = 2
	)
	if raw.TotalStorageSlots > 0 {
		blocks := (raw.TotalStorageSlots + slotsPerBlock - 1) / slotsPerBlock
		result.TransitionEstimate = TransitionEstimate{
			TotalSlots:       raw.TotalStorageSlots,
			SlotsPerBlock:    slotsPerBlock,
			BlockTimeSeconds: blockTimeSecond,
			EstimatedBlocks:  blocks,
			EstimatedHours:   (blocks * blockTimeSecond) / 3600,
		}
	}

	return result
}

func getStorageBucket(slots uint64) string {
	switch {
	case slots >= 10_000_000:
		return "10M+"
	case slots >= 1_000_000:
		return "1M-10M"
	case slots >= 100_000:
		return "100K-1M"
	case slots >= 10_000:
		return "10K-100K"
	case slots >= 1_000:
		return "1K-10K"
	case slots >= 100:
		return "100-1K"
	case slots >= 10:
		return "10-100"
	default:
		return "<10"
	}
}

func partialSort(contracts []RawContractData, n int) {
	for i := 0; i < n && i < len(contracts); i++ {
		maxIdx := i
		for j := i + 1; j < len(contracts); j++ {
			if contracts[j].StorageSlots > contracts[maxIdx].StorageSlots {
				maxIdx = j
			}
		}
		contracts[i], contracts[maxIdx] = contracts[maxIdx], contracts[i]
	}
}

func quickSortContracts(contracts []RawContractData, low, high int) {
	if low < high {
		p := partitionContracts(contracts, low, high)
		quickSortContracts(contracts, low, p-1)
		quickSortContracts(contracts, p+1, high)
	}
}

func partitionContracts(contracts []RawContractData, low, high int) int {
	pivot := contracts[high].StorageSlots
	i := low - 1
	for j := low; j < high; j++ {
		if contracts[j].StorageSlots > pivot {
			i++
			contracts[i], contracts[j] = contracts[j], contracts[i]
		}
	}
	contracts[i+1], contracts[high] = contracts[high], contracts[i+1]
	return i + 1
}

func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func LogAnalyzedResult(result *AnalyzedResult) {
	log.Info("=== State Analysis Summary ===")
	log.Info("Accounts",
		"total", result.TotalAccounts,
		"withStorage", result.AccountsWithStorage,
		"withCode", result.AccountsWithCode,
	)
	log.Info("Storage",
		"totalSlots", result.TotalStorageSlots,
		"totalSize", formatBytes(result.TotalStorageBytes),
	)

	log.Info("Storage slot distribution (by contract)",
		"<10", result.StorageDistribution["<10"],
		"10-100", result.StorageDistribution["10-100"],
		"100-1K", result.StorageDistribution["100-1K"],
		"1K-10K", result.StorageDistribution["1K-10K"],
		"10K-100K", result.StorageDistribution["10K-100K"],
		"100K-1M", result.StorageDistribution["100K-1M"],
		"1M-10M", result.StorageDistribution["1M-10M"],
		"10M+", result.StorageDistribution["10M+"],
	)

	if len(result.TopContracts) > 0 {
		log.Info("Top contracts by storage slots:")
		for i, c := range result.TopContracts {
			if i >= 10 {
				log.Info("...", "remaining", len(result.TopContracts)-10)
				break
			}
			log.Info("Contract",
				"rank", i+1,
				"hash", c.AddressHash,
				"address", c.Address,
				"slots", c.StorageSlots,
				"size", formatBytes(c.StorageBytes),
				"slotPct", fmt.Sprintf("%.2f%%", c.SlotPercentage),
				"sizePct", fmt.Sprintf("%.2f%%", c.BytePercentage),
				"hasCode", c.HasCode,
			)
		}
	}

	if result.TransitionEstimate.TotalSlots > 0 {
		log.Info("EIP-8032 transition estimate",
			"totalSlots", result.TransitionEstimate.TotalSlots,
			"estimatedBlocks", result.TransitionEstimate.EstimatedBlocks,
			"estimatedHours", result.TransitionEstimate.EstimatedHours,
		)
	}
}
