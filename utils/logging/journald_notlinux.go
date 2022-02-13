//go:build !linux

package logging

func IsJournal(fd int) (bool, error) {
	return false, nil
}
