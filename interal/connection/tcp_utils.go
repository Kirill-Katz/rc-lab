package connection

import (
	"golang.org/x/sys/unix"
	"encoding/binary"
	"io"
	"fmt"
)

func readExactly(fd int, buffer []byte) error {
	for len(buffer) > 0 {
		n, err := unix.Read(fd, buffer)

		if err == unix.EINTR {
			continue
		}

		if err != nil {
			return err
		}

		if n == 0 {
			return io.ErrUnexpectedEOF
		}

		buffer = buffer[n:]
	}

	return nil
}

func readMessage(fd int) ([]byte, error) {
	header := make([]byte, 4)

	if err := readExactly(fd, header); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(header)

	if length == 0 || length > MAX_MSG_LEN {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}

	payload := make([]byte, int(length))

	if err := readExactly(fd, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

func writeMessage(fd int, message []byte) error {
	if len(message) == 0 || len(message) > MAX_MSG_LEN {
		return fmt.Errorf("invalid message size: %d", len(message))
	}

	buffer := make([]byte, 4 + len(message))
	binary.BigEndian.PutUint32(buffer[:4], uint32(len(message)))

	for len(buffer) > 0 {
		n, err := unix.Write(fd, buffer)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}

		buffer = buffer[n:]
	}

	return nil
}
