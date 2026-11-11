package connection

import (
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
)

const MAX_MSG_LEN = 64 * 1024

var ErrFrameLength = errors.New("invalid frame length")

type Socket struct{ FD int }

func (s *Socket) Close() { _ = unix.Close(s.FD) }
func (s *Socket) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	for {
		n, err := unix.Read(s.FD, buf)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}
func (s *Socket) Write(buf []byte) (int, error) {
	written := 0
	for written < len(buf) {
		n, err := send(s.FD, buf[written:])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
		written += n
	}
	return written, nil
}
func readMessage(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MAX_MSG_LEN {
		return nil, fmt.Errorf("%w: %d", ErrFrameLength, length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
func writeMessage(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MAX_MSG_LEN {
		return fmt.Errorf("%w: %d", ErrFrameLength, len(payload))
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	for len(frame) > 0 {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func configureSocket(fd int) error {
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &unix.Timeval{Sec: 5}); err != nil {
		return err
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_KEEPALIVE, 1); err != nil {
		return err
	}
	return configurePlatform(fd)
}
