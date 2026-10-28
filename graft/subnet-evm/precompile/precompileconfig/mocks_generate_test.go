package precompileconfig

//go:generate go tool mockgen -package=$GOPACKAGE -destination=mocks.go . Config,ChainConfig,Accepter
