package contract

//go:generate go tool -modfile=../../../../go.mod mockgen -package=$GOPACKAGE -destination=mocks.go . BlockContext,AccessibleState,StateDB
