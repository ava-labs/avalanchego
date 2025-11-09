// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Block Database Verifier - Continuously compares block data between two Avalanche C-Chain endpoints.
//
// The verifier starts from a specified block height (or latest) and continuously monitors for new blocks,
// verifying block headers, receipts, and transaction data between two RPC endpoints. All discrepancies
// are logged with detailed field-level information.
//
// Required flags: --endpoint1, --endpoint2
// Use Ctrl+C to gracefully stop the application.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// Retry configuration
	maxRetries              = 5
	maxRetryTime            = 45 * time.Minute // Total retry time: ~45 minutes
	baseRetryDelay          = 2 * time.Minute  // Base delay for exponential backoff
	maxEmptyResponseRetries = 3                // Max retries for empty/null responses

	// Progress reporting default
	defaultProgressInterval = 100 // Log progress every N blocks

	// Concurrency configuration
	defaultWorkers = 10   // Default number of concurrent workers
	queueSize      = 1000 // Size of the work queue

	// Default check interval for new blocks (2 minutes)
	defaultCheckInterval = 120

	// Default max transactions to verify per block (0 = unlimited)
	// Samples first N transactions to avoid overwhelming endpoints with large blocks
	defaultMaxTransactions = 5

	// Default max blocks per worker per second (rate limiting)
	defaultMaxBlocksPerSecond = 100

	// Default finality buffer - stay this many blocks behind chain tip
	defaultFinalityBuffer = 10

	// Default log directory
	defaultLogDirectory = "/var/log"

	// Truncation limits for log output to avoid excessive verbosity
	maxExtraDataDisplay = 66  // 0x prefix + 64 hex chars (32 bytes)
	maxInputDataDisplay = 130 // 0x prefix + 128 hex chars (64 bytes)
	maxLogDataDisplay   = 130 // 0x prefix + 128 hex chars (64 bytes)
)

type RPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type BlockData struct {
	Number           string   `json:"number"`
	Hash             string   `json:"hash"`
	ParentHash       string   `json:"parentHash"`
	Nonce            string   `json:"nonce"`
	Sha3Uncles       string   `json:"sha3Uncles"`
	LogsBloom        string   `json:"logsBloom"`
	TransactionsRoot string   `json:"transactionsRoot"`
	StateRoot        string   `json:"stateRoot"`
	ReceiptsRoot     string   `json:"receiptsRoot"`
	Miner            string   `json:"miner"`
	Difficulty       string   `json:"difficulty"`
	TotalDifficulty  string   `json:"totalDifficulty"`
	ExtraData        string   `json:"extraData"`
	Size             string   `json:"size"`
	GasLimit         string   `json:"gasLimit"`
	GasUsed          string   `json:"gasUsed"`
	Timestamp        string   `json:"timestamp"`
	Transactions     []string `json:"transactions"`
	Uncles           []string `json:"uncles"`
}

type ReceiptData struct {
	BlockHash         string `json:"blockHash"`
	BlockNumber       string `json:"blockNumber"`
	ContractAddress   string `json:"contractAddress"`
	CumulativeGasUsed string `json:"cumulativeGasUsed"`
	EffectiveGasPrice string `json:"effectiveGasPrice"`
	From              string `json:"from"`
	GasUsed           string `json:"gasUsed"`
	Logs              []struct {
		Address          string   `json:"address"`
		Topics           []string `json:"topics"`
		Data             string   `json:"data"`
		BlockNumber      string   `json:"blockNumber"`
		TransactionHash  string   `json:"transactionHash"`
		TransactionIndex string   `json:"transactionIndex"`
		BlockHash        string   `json:"blockHash"`
		LogIndex         string   `json:"logIndex"`
		Removed          bool     `json:"removed"`
	} `json:"logs"`
	LogsBloom        string `json:"logsBloom"`
	Status           string `json:"status"`
	To               string `json:"to"`
	TransactionHash  string `json:"transactionHash"`
	TransactionIndex string `json:"transactionIndex"`
	Type             string `json:"type"`
}

type TransactionData struct {
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	From             string `json:"from"`
	Gas              string `json:"gas"`
	GasPrice         string `json:"gasPrice"`
	Hash             string `json:"hash"`
	Input            string `json:"input"`
	Nonce            string `json:"nonce"`
	To               string `json:"to"`
	TransactionIndex string `json:"transactionIndex"`
	Value            string `json:"value"`
	Type             string `json:"type"`
	V                string `json:"v"`
	R                string `json:"r"`
	S                string `json:"s"`
}

type Verifier struct {
	endpoint1          string
	endpoint2          string
	logFile            string
	errorLog           *os.File
	httpClient         *http.Client
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	blocksProcessed    int64
	discrepancies      int64
	startTime          time.Time
	startHeight        int64
	currentHeight      int64
	workers            int
	checkInterval      int
	maxTransactions    int
	progressInterval   int
	maxBlocksPerSecond int
	finalityBuffer     int
	workQueue          chan int64
	wg                 sync.WaitGroup
}

func main() {
	// Parse command-line arguments
	var (
		endpoint1          = flag.String("endpoint1", "", "First RPC endpoint (required)")
		endpoint2          = flag.String("endpoint2", "", "Second RPC endpoint (required)")
		startHeight        = flag.Int64("start-height", -1, "Starting block height (optional, defaults to latest)")
		logFile            = flag.String("log-file", "", "Path to error log file (optional, defaults to timestamped file in /var/log)")
		workers            = flag.Int("workers", defaultWorkers, "Number of concurrent workers")
		checkInterval      = flag.Int("check-interval", defaultCheckInterval, "Interval between checks for new blocks in seconds")
		maxTransactions    = flag.Int("max-transactions", defaultMaxTransactions, "Maximum number of transactions to verify per block")
		progressInterval   = flag.Int("progress-interval", defaultProgressInterval, "Log progress every N blocks")
		maxBlocksPerSecond = flag.Int("max-blocks-per-second", defaultMaxBlocksPerSecond, "Maximum blocks per worker per second (rate limit)")
		finalityBuffer     = flag.Int("finality-buffer", defaultFinalityBuffer, "Number of blocks to stay behind chain tip to ensure finality")
	)
	flag.Parse()

	// Validate required arguments
	if *endpoint1 == "" || *endpoint2 == "" {
		log.Fatal("Both --endpoint1 and --endpoint2 are required")
	}

	// Validate optional arguments
	if *startHeight < -1 {
		log.Fatal("--start-height must be -1 (latest) or a positive block number")
	}
	if *workers < 1 {
		log.Fatal("--workers must be at least 1")
	}
	if *checkInterval < 1 {
		log.Fatal("--check-interval must be at least 1 second")
	}
	if *maxTransactions < 0 {
		log.Fatal("--max-transactions must be 0 (unlimited) or a positive number")
	}
	if *progressInterval < 1 {
		log.Fatal("--progress-interval must be at least 1")
	}
	if *maxBlocksPerSecond < 1 {
		log.Fatal("--max-blocks-per-second must be at least 1")
	}
	if *finalityBuffer < 0 {
		log.Fatal("--finality-buffer must be 0 or a positive number")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received interrupt signal, shutting down gracefully...")
		cancel()
	}()

	// Generate log file name if not provided
	if *logFile == "" {
		timestamp := time.Now().Format("20060102_150405")
		*logFile = fmt.Sprintf("%s/block_verification_errors_%s.log", defaultLogDirectory, timestamp)
	}

	// Open error log file
	errorLog, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open error log file %s: %v", *logFile, err)
	}
	defer errorLog.Close()

	verifier := &Verifier{
		endpoint1: *endpoint1,
		endpoint2: *endpoint2,
		logFile:   *logFile,
		errorLog:  errorLog,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				MaxConnsPerHost:       50,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second, // TCP connection timeout
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		ctx:                ctx,
		cancel:             cancel,
		blocksProcessed:    0,
		discrepancies:      0,
		startTime:          time.Now(),
		workers:            *workers,
		checkInterval:      *checkInterval,
		maxTransactions:    *maxTransactions,
		progressInterval:   *progressInterval,
		maxBlocksPerSecond: *maxBlocksPerSecond,
		finalityBuffer:     *finalityBuffer,
		workQueue:          make(chan int64, queueSize),
	}

	// Determine starting height
	var startingHeight int64
	if *startHeight >= 0 {
		startingHeight = *startHeight
		log.Printf("Start height: %s (specified)", formatBlockNumber(startingHeight))
	} else {
		// Get latest block height from first endpoint
		latestHeight, err := verifier.getLatestBlockHeight(verifier.endpoint1)
		if err != nil {
			log.Fatalf("Failed to get latest block height: %v", err)
		}
		startingHeight = latestHeight
		log.Printf("Start height: %s (latest)", formatBlockNumber(startingHeight))
	}

	verifier.startHeight = startingHeight
	verifier.currentHeight = startingHeight

	// Log startup information
	verifier.logStartup()

	// Start workers
	log.Printf("Starting %d worker threads for concurrent verification", verifier.workers)
	verifier.startWorkers()

	// Start continuous verification
	log.Printf("Verification loop started (check interval: %d seconds)", verifier.checkInterval)
	verifier.startContinuousVerification()

	// Wait for all workers to complete
	verifier.wg.Wait()
	verifier.logSummary()
}

func (v *Verifier) startWorkers() {
	for i := 0; i < v.workers; i++ {
		v.wg.Add(1)
		go v.worker(i)
	}
}

func (v *Verifier) startContinuousVerification() {
	go func() {
		defer func() {
			queueLen := len(v.workQueue)
			if queueLen > 0 {
				log.Printf("Draining work queue: %d blocks remaining", queueLen)
			}
			close(v.workQueue)
		}()

		for {
			select {
			case <-v.ctx.Done():
				log.Printf("Verification loop stopped by context cancellation")
				return
			default:
			}

			// Log heartbeat before attempting to get latest block height
			v.mu.Lock()
			currentHeight := v.currentHeight
			v.mu.Unlock()
			log.Printf("Checking for new blocks (current height: %s)...", formatBlockNumber(currentHeight))

			// Get current latest block height
			latestHeight, err := v.getLatestBlockHeight(v.endpoint1)
			if err != nil {
				errMsg := fmt.Sprintf("Failed to get latest block height: %v", err)
				log.Print(errMsg)
				v.logConnectionIssue(errMsg)

				// Context-aware sleep to allow immediate shutdown
				select {
				case <-v.ctx.Done():
					return
				case <-time.After(time.Duration(v.checkInterval) * time.Second):
				}
				continue
			}

			// Apply finality buffer to avoid querying unfinalized blocks
			verifiableHeight := latestHeight - int64(v.finalityBuffer)
			// Safety: never verify blocks below 0
			if verifiableHeight < 0 {
				verifiableHeight = 0
			}

			// Check if already caught up to verifiable height
			if currentHeight >= verifiableHeight {
				log.Printf("Chain tip at block %s, verifiable up to %s (buffer: %d), checking again in %d seconds",
					formatBlockNumber(latestHeight), formatBlockNumber(verifiableHeight),
					v.finalityBuffer, v.checkInterval)
				// Wait before next check
				select {
				case <-v.ctx.Done():
					return
				case <-time.After(time.Duration(v.checkInterval) * time.Second):
				}
				continue
			}

			// Send blocks to verify
			blocksToVerify := verifiableHeight - currentHeight
			log.Printf("Queueing %s new blocks (%s to %s), chain tip: %s (buffer: %d)",
				formatBlockNumber(blocksToVerify),
				formatBlockNumber(currentHeight+1),
				formatBlockNumber(verifiableHeight),
				formatBlockNumber(latestHeight),
				v.finalityBuffer)

			for height := currentHeight + 1; height <= verifiableHeight; height++ {
				select {
				case <-v.ctx.Done():
					return
				case v.workQueue <- height:
				}
			}

			// Update current height
			v.mu.Lock()
			v.currentHeight = verifiableHeight
			v.mu.Unlock()

			// Wait after queueing to prevent immediate re-check
			select {
			case <-v.ctx.Done():
				return
			case <-time.After(time.Duration(v.checkInterval) * time.Second):
			}
		}
	}()
}

func (v *Verifier) worker(workerID int) {
	defer v.wg.Done()

	for {
		select {
		case <-v.ctx.Done():
			return
		case height, ok := <-v.workQueue:
			if !ok {
				return // Queue closed
			}

			// Check for cancellation before processing
			select {
			case <-v.ctx.Done():
				return
			default:
			}

			// Track processing time for rate limiting
			startTime := time.Now()
			err := v.verifyBlock(height)
			processingTime := time.Since(startTime)

			// Rate limiting: ensure we don't exceed maxBlocksPerSecond
			if v.maxBlocksPerSecond > 0 {
				targetDuration := time.Second / time.Duration(v.maxBlocksPerSecond)
				if processingTime < targetDuration {
					// Context-aware sleep to allow immediate shutdown
					select {
					case <-v.ctx.Done():
						return
					case <-time.After(targetDuration - processingTime):
					}
				}
			}

			// Update counters and check if we should log progress
			v.mu.Lock()
			v.blocksProcessed++
			shouldLogProgress := v.blocksProcessed%int64(v.progressInterval) == 0
			blocksProcessed := v.blocksProcessed
			discrepancies := v.discrepancies

			if err != nil {
				// Only count as discrepancy if it's not a context cancellation
				if err != context.Canceled && err != context.DeadlineExceeded {
					v.discrepancies++
					discrepancies = v.discrepancies
				}
			}
			v.mu.Unlock()

			// Log errors and progress OUTSIDE the mutex to avoid deadlock
			if err != nil {
				if err != context.Canceled && err != context.DeadlineExceeded {
					v.logError(height, fmt.Sprintf("Verification failed: %v", err))
				} else {
					log.Printf("[Worker %d] Block %d verification cancelled by user", workerID, height)
				}
			} else if shouldLogProgress {
				log.Printf("Progress: %s blocks verified, %s discrepancies found (%.2f%% success rate)",
					formatBlockNumber(blocksProcessed), formatBlockNumber(discrepancies),
					float64(blocksProcessed-discrepancies)/float64(blocksProcessed)*100)
			}
		}
	}
}

func (v *Verifier) getLatestBlockHeight(url string) (int64, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_blockNumber",
		Params:  []interface{}{},
		ID:      1,
	}

	var res RPCResponse
	err := v.sendRPCRequestWithRetry(url, req, &res)
	if err != nil {
		return 0, err
	}

	var hexHeight string
	if err := json.Unmarshal(res.Result, &hexHeight); err != nil {
		return 0, err
	}

	return hexToInt64(hexHeight)
}

func (v *Verifier) verifyBlock(height int64) error {
	// Get block data from both endpoints
	block1, blockJSON1, err := v.getBlockByNumber(v.endpoint1, height)
	if err != nil {
		return fmt.Errorf("failed to get block %d from endpoint1: %v", height, err)
	}

	block2, blockJSON2, err := v.getBlockByNumber(v.endpoint2, height)
	if err != nil {
		return fmt.Errorf("failed to get block %d from endpoint2: %v", height, err)
	}

	// Validate blocks are not nil and have expected data
	if block1 == nil || block2 == nil {
		return fmt.Errorf("received null block data")
	}
	if block1.Hash == "" || block2.Hash == "" {
		return fmt.Errorf("received block with empty hash")
	}

	// Validate blocks are the requested height
	expectedHeight := fmt.Sprintf("0x%x", height)
	if block1.Number != expectedHeight {
		return fmt.Errorf("endpoint1 returned wrong block: requested %d (0x%x), got %s", height, height, block1.Number)
	}
	if block2.Number != expectedHeight {
		return fmt.Errorf("endpoint2 returned wrong block: requested %d (0x%x), got %s", height, height, block2.Number)
	}

	// Compare block data using normalized JSON
	normalized1, err := normalizeJSON(blockJSON1)
	if err != nil {
		return fmt.Errorf("failed to normalize block JSON from endpoint1: %v", err)
	}
	normalized2, err := normalizeJSON(blockJSON2)
	if err != nil {
		return fmt.Errorf("failed to normalize block JSON from endpoint2: %v", err)
	}

	if normalized1 != normalized2 {
		return fmt.Errorf("block data mismatch")
	}

	// Get and compare block receipts using raw JSON
	_, receiptsJSON1, err := v.getBlockReceipts(v.endpoint1, height)
	if err != nil {
		return fmt.Errorf("failed to get receipts for block %d from endpoint1: %v", height, err)
	}

	_, receiptsJSON2, err := v.getBlockReceipts(v.endpoint2, height)
	if err != nil {
		return fmt.Errorf("failed to get receipts for block %d from endpoint2: %v", height, err)
	}

	// Compare receipts using normalized JSON
	normalizedReceipts1, err := normalizeJSON(receiptsJSON1)
	if err != nil {
		return fmt.Errorf("failed to normalize receipts JSON from endpoint1: %v", err)
	}
	normalizedReceipts2, err := normalizeJSON(receiptsJSON2)
	if err != nil {
		return fmt.Errorf("failed to normalize receipts JSON from endpoint2: %v", err)
	}

	if normalizedReceipts1 != normalizedReceipts2 {
		return fmt.Errorf("receipts mismatch")
	}

	// Verify transaction count matches (from block data already fetched)
	if len(block1.Transactions) != len(block2.Transactions) {
		return fmt.Errorf("transaction count mismatch: endpoint1=%d, endpoint2=%d", len(block1.Transactions), len(block2.Transactions))
	}

	// Verify transaction hashes match
	for i := range block1.Transactions {
		if block1.Transactions[i] != block2.Transactions[i] {
			return fmt.Errorf("transaction hash mismatch at position %d: endpoint1=%s, endpoint2=%s", i, block1.Transactions[i], block2.Transactions[i])
		}
	}

	// Fetch and compare detailed transaction data for first N transactions only (sampling)
	txCount := len(block1.Transactions)
	txsToVerify := txCount
	if v.maxTransactions > 0 && txCount > v.maxTransactions {
		txsToVerify = v.maxTransactions
	}

	for i := 0; i < txsToVerify; i++ {
		txHash := block1.Transactions[i]

		_, txJSON1, err := v.getTransactionByHash(v.endpoint1, txHash)
		if err != nil {
			return fmt.Errorf("failed to get transaction %s for block %d from endpoint1: %v", txHash, height, err)
		}

		_, txJSON2, err := v.getTransactionByHash(v.endpoint2, txHash)
		if err != nil {
			return fmt.Errorf("failed to get transaction %s for block %d from endpoint2: %v", txHash, height, err)
		}

		// Compare transaction using normalized JSON
		normalizedTx1, err := normalizeJSON(txJSON1)
		if err != nil {
			return fmt.Errorf("failed to normalize transaction JSON from endpoint1: %v", err)
		}
		normalizedTx2, err := normalizeJSON(txJSON2)
		if err != nil {
			return fmt.Errorf("failed to normalize transaction JSON from endpoint2: %v", err)
		}

		if normalizedTx1 != normalizedTx2 {
			return fmt.Errorf("transaction %s mismatch", txHash)
		}
	}

	return nil
}

func (v *Verifier) getBlockByNumber(url string, number int64) (*BlockData, json.RawMessage, error) {
	hexNumber := fmt.Sprintf("0x%x", number)
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []interface{}{hexNumber, false},
		ID:      1,
	}

	var res RPCResponse
	err := v.sendRPCRequestWithRetry(url, req, &res)
	if err != nil {
		return nil, nil, err
	}

	var block BlockData
	if err := json.Unmarshal(res.Result, &block); err != nil {
		return nil, nil, err
	}

	return &block, res.Result, nil
}

func (v *Verifier) getBlockReceipts(url string, number int64) ([]ReceiptData, json.RawMessage, error) {
	hexNumber := fmt.Sprintf("0x%x", number)
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBlockReceipts",
		Params:  []interface{}{hexNumber},
		ID:      1,
	}

	var res RPCResponse
	err := v.sendRPCRequestWithRetry(url, req, &res)
	if err != nil {
		return nil, nil, err
	}

	var receipts []ReceiptData
	if err := json.Unmarshal(res.Result, &receipts); err != nil {
		return nil, nil, err
	}

	return receipts, res.Result, nil
}

func (v *Verifier) getTransactionByHash(url string, txHash string) (*TransactionData, json.RawMessage, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_getTransactionByHash",
		Params:  []interface{}{txHash},
		ID:      1,
	}

	var res RPCResponse
	err := v.sendRPCRequestWithRetry(url, req, &res)
	if err != nil {
		return nil, nil, err
	}

	var tx TransactionData
	if err := json.Unmarshal(res.Result, &tx); err != nil {
		return nil, nil, err
	}

	return &tx, res.Result, nil
}

func (v *Verifier) sendRPCRequestWithRetry(url string, req RPCRequest, res *RPCResponse) error {
	var lastErr error
	startTime := time.Now()

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check if we've exceeded max retry time
		if time.Since(startTime) > maxRetryTime {
			return fmt.Errorf("exceeded maximum retry time of %v for %s, last error: %v", maxRetryTime, req.Method, lastErr)
		}

		// Check if context is cancelled
		select {
		case <-v.ctx.Done():
			return v.ctx.Err()
		default:
		}

		err := v.sendRPCRequest(url, req, res)
		if err == nil {
			// Check for RPC error in response
			if res.Error != nil {
				return fmt.Errorf("RPC error %d: %s", res.Error.Code, res.Error.Message)
			}

			// Check if result is null/empty
			if res.Result == nil || string(res.Result) == "null" || string(res.Result) == `""` {
				// Handle empty response with specific retry logic
				if attempt < maxEmptyResponseRetries {
					// Retry without logging to avoid clutter
					select {
					case <-time.After(2 * time.Second):
					case <-v.ctx.Done():
						return v.ctx.Err()
					}
					continue
				} else {
					// After maxEmptyResponseRetries, return error indicating empty response
					return fmt.Errorf("empty response from node for %s after %d retries", req.Method, maxEmptyResponseRetries)
				}
			}
			return nil
		}

		lastErr = err

		// Don't retry on context cancellation errors
		if err == context.Canceled || err == context.DeadlineExceeded ||
			strings.Contains(err.Error(), "context canceled") {
			return err
		}

		// Calculate backoff delay using exponential backoff
		delay := time.Duration(1<<uint(attempt)) * baseRetryDelay
		if delay > maxRetryTime/2 {
			delay = maxRetryTime / 2
		}

		// Don't log individual retry attempts to avoid clutter
		// Only final failure will be logged after all retries exhausted

		// Wait with context cancellation support
		select {
		case <-time.After(delay):
		case <-v.ctx.Done():
			return v.ctx.Err()
		}
	}

	return fmt.Errorf("failed after %d attempts for %s, last error: %v", maxRetries, req.Method, lastErr)
}

func (v *Verifier) sendRPCRequest(url string, req RPCRequest, res *RPCResponse) error {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(v.ctx, "POST", url, strings.NewReader(string(reqBody)))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Validate response is JSON before attempting to unmarshal
	if len(body) > 0 && body[0] != '{' && body[0] != '[' {
		preview := string(body)
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		return fmt.Errorf("received non-JSON response (starts with '%c'): %s", body[0], preview)
	}

	return json.Unmarshal(body, res)
}

func (v *Verifier) logError(height int64, message string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMsg := fmt.Sprintf("[%s] Block %d: %s\n", timestamp, height, message)
	log.Print(logMsg)

	if _, err := v.errorLog.WriteString(logMsg); err != nil {
		log.Printf("[%s] Failed to write to error log file: %v", timestamp, err)
	}
	v.errorLog.Sync() // Flush to disk
}

func (v *Verifier) logSummary() {
	v.mu.Lock()
	defer v.mu.Unlock()

	duration := time.Since(v.startTime)
	successRate := float64(0)
	if v.blocksProcessed > 0 {
		successRate = float64(v.blocksProcessed-v.discrepancies) / float64(v.blocksProcessed) * 100
	}

	// Calculate blocks per minute
	blocksPerMin := float64(0)
	if duration.Minutes() > 0 {
		blocksPerMin = float64(v.blocksProcessed) / duration.Minutes()
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	summary := fmt.Sprintf(`
[%s] === VERIFICATION SUMMARY ===
Endpoint 1: %s
Endpoint 2: %s
Start Height: %s
Current Height: %s
Blocks Processed: %s
Discrepancies Found: %s
Session Duration: %v
Blocks per Minute: %.2f
Success Rate: %.2f%%
Log File: %s
============================
`, timestamp, v.endpoint1, v.endpoint2, formatBlockNumber(v.startHeight), formatBlockNumber(v.currentHeight), formatBlockNumber(v.blocksProcessed), formatBlockNumber(v.discrepancies), duration, blocksPerMin, successRate, v.logFile)

	log.Print(summary)

	// Write full summary to error log file as well
	if _, err := v.errorLog.WriteString(summary); err != nil {
		log.Printf("[%s] Failed to write summary to error log file: %v", timestamp, err)
	}
	v.errorLog.Sync() // Flush to disk
}

func (v *Verifier) logStartup() {
	v.mu.Lock()
	defer v.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	startupMsg := fmt.Sprintf(`
[%s] === VERIFIER STARTED ===
Endpoint 1: %s
Endpoint 2: %s
Start Height: %s
Workers: %d
Check Interval: %d seconds
Max Transactions per Block: %d
Max Blocks per Second: %d (per worker)
Finality Buffer: %d blocks
Progress Interval: %d blocks
Error Log File: %s
============================
`, timestamp, v.endpoint1, v.endpoint2, formatBlockNumber(v.startHeight), v.workers, v.checkInterval, v.maxTransactions, v.maxBlocksPerSecond, v.finalityBuffer, v.progressInterval, v.logFile)

	log.Print(startupMsg)

	// Write full startup message to error log for context
	if _, err := v.errorLog.WriteString(startupMsg); err != nil {
		log.Printf("Failed to write startup to error log: %v", err)
	}
	v.errorLog.Sync()
}

func (v *Verifier) logConnectionIssue(message string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMsg := fmt.Sprintf("[%s] CONNECTION: %s\n", timestamp, message)
	log.Print(logMsg)

	// Also write to error log file
	if _, err := v.errorLog.WriteString(logMsg); err != nil {
		log.Printf("[%s] Failed to write connection issue to error log file: %v", timestamp, err)
	}
	v.errorLog.Sync() // Flush to disk
}

// normalizeJSON normalizes JSON for consistent comparison by unmarshaling and remarshaling.
// This ensures consistent field ordering and formatting between two JSON responses.
func normalizeJSON(data json.RawMessage) (string, error) {
	var normalized interface{}
	if err := json.Unmarshal(data, &normalized); err != nil {
		return "", fmt.Errorf("failed to unmarshal JSON: %v", err)
	}

	// Re-marshal with consistent formatting (Go's json.Marshal sorts map keys)
	normalized_bytes, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}

	return string(normalized_bytes), nil
}

func hexToInt64(hexStr string) (int64, error) {
	// Remove 0x prefix
	hexStr = strings.TrimPrefix(hexStr, "0x")

	// Parse as big.Int first to handle large numbers
	bigInt := new(big.Int)
	bigInt, ok := bigInt.SetString(hexStr, 16)
	if !ok {
		return 0, fmt.Errorf("invalid hex string: %s", hexStr)
	}

	// Check if it fits in int64
	if !bigInt.IsInt64() {
		return 0, fmt.Errorf("hex value too large for int64: %s", hexStr)
	}

	return bigInt.Int64(), nil
}

// formatBlockNumber formats a block number with commas for readability
// Example: 71470054 -> "71,470,054"
func formatBlockNumber(n int64) string {
	if n < 0 {
		return "-" + formatBlockNumber(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatBlockNumber(n/1000) + fmt.Sprintf(",%03d", n%1000)
}
