package connection

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type shortReader struct{ io.Reader }

func (r shortReader) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return r.Reader.Read(b)
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return w.Buffer.Write(b)
}

func TestFraming(t *testing.T) {
	var stream bytes.Buffer
	for _, payload := range []string{`{"type":"menu","id":"1"}`, `{"type":"menu","id":"2"}`} {
		if err := writeMessage(&stream, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	r := shortReader{&stream}
	for _, want := range []string{`{"type":"menu","id":"1"}`, `{"type":"menu","id":"2"}`} {
		got, err := readMessage(r)
		if err != nil || string(got) != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	if _, err := readMessage(r); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: %v", err)
	}
	w := &shortWriter{}
	if err := writeMessage(w, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if got, err := readMessage(&w.Buffer); err != nil || string(got) != "abc" {
		t.Fatalf("short write: %q %v", got, err)
	}
}
func TestInvalidFrames(t *testing.T) {
	for _, length := range []uint32{0, MAX_MSG_LEN + 1, ^uint32(0)} {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], length)
		if _, err := readMessage(bytes.NewReader(header[:])); !errors.Is(err, ErrFrameLength) {
			t.Fatalf("length %d: %v", length, err)
		}
	}
	for _, data := range [][]byte{{0}, {0, 0, 0, 2, 'a'}} {
		if _, err := readMessage(bytes.NewReader(data)); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("partial frame: %v", err)
		}
	}
	for _, payload := range [][]byte{nil, make([]byte, MAX_MSG_LEN+1)} {
		if err := writeMessage(io.Discard, payload); !errors.Is(err, ErrFrameLength) {
			t.Fatalf("write length: %v", err)
		}
	}
	payload := bytes.Repeat([]byte("a"), MAX_MSG_LEN)
	var buf bytes.Buffer
	if err := writeMessage(&buf, payload); err != nil {
		t.Fatal(err)
	}
	if got, err := readMessage(&buf); err != nil || !bytes.Equal(got, payload) {
		t.Fatal("maximum frame failed", err)
	}
}
