//go:build darwin

package connection

import "golang.org/x/sys/unix"

func send(fd int, b []byte) (int, error) { return unix.Write(fd, b) }
func configurePlatform(fd int) error {
	for _, opt := range []struct{ level, name, value int }{
		{unix.SOL_SOCKET, unix.SO_NOSIGPIPE, 1},
		{unix.IPPROTO_TCP, unix.TCP_KEEPALIVE, 60},
		{unix.IPPROTO_TCP, unix.TCP_KEEPINTVL, 10},
		{unix.IPPROTO_TCP, unix.TCP_KEEPCNT, 3},
	} {
		if err := unix.SetsockoptInt(fd, opt.level, opt.name, opt.value); err != nil {
			return err
		}
	}
	return nil
}
