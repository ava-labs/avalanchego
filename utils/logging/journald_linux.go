//go:build linux

package logging

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// IsJournal returns true if fd is connected to the systemd journal,
// false otherwise
func IsJournal(fd int) (bool, error) {
	// When a processes is started by systemd and standard error is connected
	// to the journal, then $JOURNAL_STREAM contains the device and inode
	// number of the standard error file descriptor (e.g. "123:789").
	// https://www.freedesktop.org/software/systemd/man/systemd.exec.html#%24JOURNAL_STREAM
	js := os.Getenv("JOURNAL_STREAM")
	parts := strings.SplitN(js, ":", 2)
	if len(parts) != 2 {
		return false, errors.New(js)
	}

	journalDev, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return false, err
	}

	journalIno, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return false, err
	}

	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return false, err
	}

	return journalDev == stat.Dev && journalIno == stat.Ino, nil
}
