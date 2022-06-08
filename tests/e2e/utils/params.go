package utils

var (
	outputFile string
	pluginDir  string
)

func SetOutputFile(filepath string) {
	outputFile = filepath
}

func GetOutputPath() string {
	return outputFile
}

func SetPluginDir(dir string) {
	pluginDir = dir
}

func GetPluginDir() string {
	return pluginDir
}
