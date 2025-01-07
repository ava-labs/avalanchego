package contract

//go:generate go run go.uber.org/mock/mockgen -package=$GOPACKAGE -destination=mocks.go . BlockContext,AccessibleState,StateDB
