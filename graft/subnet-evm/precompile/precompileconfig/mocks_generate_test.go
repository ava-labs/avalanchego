package precompileconfig

//go:generate go tool -modfile=../../../../go.mod mockgen -package=$GOPACKAGE -destination=mocks.go . Predicater,Config,ChainConfig,Accepter
