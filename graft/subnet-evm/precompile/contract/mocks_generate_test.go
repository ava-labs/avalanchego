package contract

//go:generate go tool -modfile=../../../../tools/go.mod mockgen -package=$GOPACKAGE -destination=mocks.go . BlockContext,AccessibleState,StateDB
