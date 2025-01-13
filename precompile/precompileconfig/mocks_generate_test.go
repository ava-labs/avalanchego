package precompileconfig

//go:generate go run go.uber.org/mock/mockgen -package=$GOPACKAGE -destination=mocks.go . Predicater,Config,ChainConfig,Accepter
