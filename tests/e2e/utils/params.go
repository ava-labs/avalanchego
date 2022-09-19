package utils

import "sync"

var (
	mu sync.RWMutex

	outputFile string
	pluginDir  string

	// executable path for "avalanchego"
	execPath string

	vmGenesisPath string

	skipNetworkRunnerStart    bool
	skipNetworkRunnerShutdown bool
)

func SetOutputFile(filepath string) {
	mu.Lock()
	outputFile = filepath
	mu.Unlock()
}

func GetOutputPath() string {
	mu.RLock()
	e := outputFile
	mu.RUnlock()
	return e
}

// Sets the executable path for "avalanchego".
func SetExecPath(p string) {
	mu.Lock()
	execPath = p
	mu.Unlock()
}

// Loads the executable path for "avalanchego".
func GetExecPath() string {
	mu.RLock()
	e := execPath
	mu.RUnlock()
	return e
}

func SetPluginDir(dir string) {
	mu.Lock()
	pluginDir = dir
	mu.Unlock()
}

func GetPluginDir() string {
	mu.RLock()
	p := pluginDir
	mu.RUnlock()
	return p
}

func SetVmGenesisPath(p string) {
	mu.Lock()
	vmGenesisPath = p
	mu.Unlock()
}

func GetVmGenesisPath() string {
	mu.RLock()
	p := vmGenesisPath
	mu.RUnlock()
	return p
}

func SetSkipNetworkRunnerStart(b bool) {
	mu.Lock()
	skipNetworkRunnerStart = b
	mu.Unlock()
}

func GetSkipNetworkRunnerStart() bool {
	mu.RLock()
	b := skipNetworkRunnerStart
	mu.RUnlock()
	return b
}

func SetSkipNetworkRunnerShutdown(b bool) {
	mu.Lock()
	skipNetworkRunnerShutdown = b
	mu.Unlock()
}

func GetSkipNetworkRunnerShutdown() bool {
	mu.RLock()
	b := skipNetworkRunnerShutdown
	mu.RUnlock()
	return b
}
