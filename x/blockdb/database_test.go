package blockdb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Truncate(t *testing.T) {
	// Create initial database
	tempDir, err := os.MkdirTemp("", "blockdb_truncate_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	indexDir := filepath.Join(tempDir, "index")
	dataDir := filepath.Join(tempDir, "data")
	db, err := New(indexDir, dataDir, false, true, DefaultDatabaseConfig(), logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write some test data and close the database
	testBlock := []byte("test block data")
	err = db.WriteBlock(0, testBlock, 0)
	require.NoError(t, err)
	err = db.Close()
	require.NoError(t, err)

	// Reopen with truncate=true and verify data is gone
	db2, err := New(indexDir, dataDir, false, true, DefaultDatabaseConfig(), logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()
	readBlock2, err := db2.ReadBlock(1)
	require.NoError(t, err)
	require.Nil(t, readBlock2)
	_, found := db2.MaxContiguousHeight()
	require.False(t, found)
}

func TestNew_NoTruncate(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "blockdb_no_truncate_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	indexDir := filepath.Join(tempDir, "index")
	dataDir := filepath.Join(tempDir, "data")
	db, err := New(indexDir, dataDir, false, true, DefaultDatabaseConfig(), logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write some test data and close the database
	testBlock := []byte("test block data")
	err = db.WriteBlock(1, testBlock, 5)
	require.NoError(t, err)
	readBlock, err := db.ReadBlock(1)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock)
	err = db.Close()
	require.NoError(t, err)

	// Reopen with truncate=false and verify data is still there
	db2, err := New(indexDir, dataDir, false, false, DefaultDatabaseConfig(), logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()
	readBlock1, err := db2.ReadBlock(1)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock1)

	// Verify we can write additional data
	testBlock2 := []byte("test block data 3")
	err = db2.WriteBlock(2, testBlock2, 0)
	require.NoError(t, err)
	readBlock2, err := db2.ReadBlock(2)
	require.NoError(t, err)
	require.Equal(t, testBlock2, readBlock2)
}

func TestNew_Params(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "blockdb_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	tests := []struct {
		name        string
		indexDir    string
		dataDir     string
		syncToDisk  bool
		config      DatabaseConfig
		log         logging.Logger
		wantErr     error
		expectClose bool
	}{
		{
			name:     "default config",
			indexDir: tempDir,
			dataDir:  tempDir,
			config:   DefaultDatabaseConfig(),
		},
		{
			name:       "custom config",
			indexDir:   tempDir,
			dataDir:    tempDir,
			syncToDisk: true,
			config: DatabaseConfig{
				MinimumHeight:      100,
				MaxDataFileSize:    1024 * 1024 * 1024, // 1GB
				CheckpointInterval: 512,
			},
		},
		{
			name:     "empty index directory",
			indexDir: "",
			dataDir:  tempDir,
			config:   DefaultDatabaseConfig(),
			wantErr:  errors.New("both indexDir and dataDir must be provided"),
		},
		{
			name:     "empty data directory",
			indexDir: tempDir,
			dataDir:  "",
			config:   DefaultDatabaseConfig(),
			wantErr:  errors.New("both indexDir and dataDir must be provided"),
		},
		{
			name:     "both directories empty",
			indexDir: "",
			config:   DefaultDatabaseConfig(),
			dataDir:  "",
			wantErr:  errors.New("both indexDir and dataDir must be provided"),
		},
		{
			name:     "invalid config - zero checkpoint interval",
			indexDir: tempDir,
			dataDir:  tempDir,
			config: DatabaseConfig{
				MinimumHeight:      0,
				MaxDataFileSize:    DefaultMaxDataFileSize,
				CheckpointInterval: 0,
			},
			wantErr: errors.New("CheckpointInterval cannot be 0"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := New(tt.indexDir, tt.dataDir, tt.syncToDisk, true, tt.config, tt.log)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr.Error())
				return
			}

			require.NoError(t, err)
			require.NotNil(t, db)

			// Verify the database was created with correct configuration
			assert.Equal(t, tt.config.MinimumHeight, db.options.MinimumHeight)
			assert.Equal(t, tt.config.MaxDataFileSize, db.options.MaxDataFileSize)
			assert.Equal(t, tt.config.CheckpointInterval, db.options.CheckpointInterval)
			assert.Equal(t, tt.syncToDisk, db.syncToDisk)

			// Verify files were created
			indexPath := filepath.Join(tt.indexDir, indexFileName)
			dataPath := filepath.Join(tt.dataDir, dataFileName)
			assert.FileExists(t, indexPath)
			assert.FileExists(t, dataPath)

			// Test that we can close the database
			err = db.Close()
			require.NoError(t, err)
		})
	}
}

func TestNew_IndexFileErrors(t *testing.T) {
	tests := []struct {
		name       string
		setup      func() (string, string)
		wantErrMsg string
	}{
		{
			name: "corrupted index file",
			setup: func() (string, string) {
				tempDir, _ := os.MkdirTemp("", "blockdb_test_*")
				indexDir := filepath.Join(tempDir, "index")
				dataDir := filepath.Join(tempDir, "data")
				os.MkdirAll(indexDir, 0755)
				os.MkdirAll(dataDir, 0755)

				// Create a corrupted index file
				indexPath := filepath.Join(indexDir, indexFileName)
				corruptedData := []byte("corrupted index file data")
				err := os.WriteFile(indexPath, corruptedData, 0666)
				if err != nil {
					return "", ""
				}

				return indexDir, dataDir
			},
			wantErrMsg: "failed to read index header",
		},
		{
			name: "version mismatch in existing index file",
			setup: func() (string, string) {
				tempDir, _ := os.MkdirTemp("", "blockdb_test_*")
				indexDir := filepath.Join(tempDir, "index")
				dataDir := filepath.Join(tempDir, "data")

				// Create directories
				os.MkdirAll(indexDir, 0755)
				os.MkdirAll(dataDir, 0755)

				// Create a valid index file with wrong version
				indexPath := filepath.Join(indexDir, indexFileName)
				header := indexFileHeader{
					Version:             999, // Wrong version
					MinHeight:           0,
					MaxDataFileSize:     DefaultMaxDataFileSize,
					MaxHeight:           unsetHeight,
					MaxContiguousHeight: unsetHeight,
					DataFileSize:        0,
				}

				headerBytes, err := header.MarshalBinary()
				if err != nil {
					return "", ""
				}
				err = os.WriteFile(indexPath, headerBytes, 0666)
				if err != nil {
					return "", ""
				}

				return indexDir, dataDir
			},
			wantErrMsg: "mismatched index file version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexDir, dataDir := tt.setup()
			if indexDir == "" || dataDir == "" {
				t.Skip("Setup failed, skipping test")
			}
			defer os.RemoveAll(filepath.Dir(indexDir))
			defer os.RemoveAll(filepath.Dir(dataDir))

			_, err := New(indexDir, dataDir, false, false, DefaultDatabaseConfig(), logging.NoLog{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestIndexFileHeaderAlignment(t *testing.T) {
	if sizeOfIndexFileHeader%sizeOfIndexEntry != 0 {
		t.Errorf("sizeOfIndexFileHeader (%d) is not a multiple of sizeOfIndexEntry (%d)",
			sizeOfIndexFileHeader, sizeOfIndexEntry)
	}
}

func TestNew_IndexFileConfigPrecedence(t *testing.T) {
	// set up db
	initialConfig := DatabaseConfig{
		MinimumHeight:      100,
		MaxDataFileSize:    1024 * 1024, // 1MB limit
		CheckpointInterval: 1024,
	}
	tempDir, err := os.MkdirTemp("", "blockdb_config_precedence_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	db, err := New(tempDir, tempDir, false, true, initialConfig, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write a block at height 100 and close db
	testBlock := []byte("test block data")
	err = db.WriteBlock(100, testBlock, 0)
	require.NoError(t, err)
	readBlock, err := db.ReadBlock(100)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock)
	err = db.Close()
	require.NoError(t, err)

	// Reopen with different config that has higher minimum height and smaller max data file size
	differentConfig := DatabaseConfig{
		MinimumHeight:      200,        // Higher minimum height
		MaxDataFileSize:    512 * 1024, // 512KB limit (smaller than original 1MB)
		CheckpointInterval: 512,
	}
	db2, err := New(tempDir, tempDir, false, false, differentConfig, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()

	// The database should still accept blocks between 100 and 200
	testBlock2 := []byte("test block data 2")
	err = db2.WriteBlock(150, testBlock2, 0)
	require.NoError(t, err)
	readBlock2, err := db2.ReadBlock(150)
	require.NoError(t, err)
	require.Equal(t, testBlock2, readBlock2)

	// Verify that writing below initial minimum height fails
	err = db2.WriteBlock(50, []byte("invalid block"), 0)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidBlockHeight))

	// Write a large block that would exceed the new config's 512KB limit
	// but should succeed because we use the original 1MB limit from index file
	largeBlock := make([]byte, 768*1024) // 768KB block
	err = db2.WriteBlock(200, largeBlock, 0)
	require.NoError(t, err)
	readLargeBlock, err := db2.ReadBlock(200)
	require.NoError(t, err)
	require.Equal(t, largeBlock, readLargeBlock)
}
