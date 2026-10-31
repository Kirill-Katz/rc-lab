package connection

import (
	"golang.org/x/sys/unix"
	"encoding/binary"
	"io"
	"fmt"
)

type Socket struct {
	FD int
}

func (s *Socket) Close() {
	unix.Close(s.FD)
}

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
		n, err := unix.Write(s.FD, buf[written:])

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
		return nil, fmt.Errorf("invalid message length: %d", length)
	}

	payload := make([]byte, int(length))

	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	return payload, nil
}

func writeMessage(w io.Writer, message []byte) error {
	if len(message) == 0 || len(message) > MAX_MSG_LEN {
		return fmt.Errorf("invalid message size: %d", len(message))
	}

	buffer := make([]byte, 4 + len(message))
	binary.BigEndian.PutUint32(buffer[:4], uint32(len(message)))
	copy(buffer[4:], message)

	_, err := w.Write(buffer)

	if err != nil {
		return err
	}

	return nil
}
