// +build windows

// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"github.com/Microsoft/go-winio"
)

// Listen starts listening on the socket for new connection
func (s *Socket) Listen() error {
	l, err := winio.ListenPipe(windowsPipeName(s.addr), nil)
	if err != nil {
		return err
	}
	// Start a loop that accepts new connections to told to quit
	go func() {
		for {
			select {
			case <-s.quitCh:
				close(s.doneCh)
				return
			default:
				conn, err := l.Accept()
				if err != nil {
					s.log.Error("socket accept error: %s", err.Error())
				}
				s.connLock.Lock()
				s.conns = append(s.conns, conn)
				s.connLock.Unlock()
			}
		}
	}()

	return nil
}

func Dial(addr string) (*Client, error) {
	c, err := winio.DialPipe(windowsPipeName(addr), nil)
	if err != nil {
		return nil, err
	}

	return &Client{c}, nil
}

func windowsPipeName(addr string) string {
	return `\\.pipe\` + addr
}
