//go:build linux

package connection

import "golang.org/x/sys/unix"

func send(fd int, b []byte) (int, error) { return unix.SendmsgN(fd, b, nil, nil, unix.MSG_NOSIGNAL) }
func configurePlatform(fd int) error {
	for _, opt := range []struct{ name, value int }{
		{unix.TCP_KEEPIDLE, 60}, {unix.TCP_KEEPINTVL, 10}, {unix.TCP_KEEPCNT, 3},
	} {
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_TCP, opt.name, opt.value); err != nil {
			return err
		}
	}
	return nil
}
