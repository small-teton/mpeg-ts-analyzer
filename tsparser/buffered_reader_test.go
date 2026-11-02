package tsparser

import (
	"bytes"
	"io"
	"testing"
)

func TestBufferedReadSeekerReadsAndSeeks(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i)
	}
	b := newBufferedReadSeeker(bytes.NewReader(data))
	buf := make([]byte, 10)

	if _, err := io.ReadFull(b, buf); err != nil || buf[0] != 0 || buf[9] != 9 {
		t.Fatalf("first read: %v %v", buf, err)
	}
	// The whole input is buffered now, so SeekCurrent must count from the
	// caller's position, not from the underlying reader's.
	pos, err := b.Seek(5, io.SeekCurrent)
	if err != nil || pos != 15 {
		t.Fatalf("SeekCurrent = %d, %v; want 15", pos, err)
	}
	if _, err := io.ReadFull(b, buf); err != nil || buf[0] != 15 {
		t.Fatalf("read after SeekCurrent: %v %v", buf, err)
	}
	if pos, err = b.Seek(100, io.SeekStart); err != nil || pos != 100 {
		t.Fatalf("SeekStart = %d, %v; want 100", pos, err)
	}
	if _, err := io.ReadFull(b, buf); err != nil || buf[0] != 100 {
		t.Fatalf("read after SeekStart: %v %v", buf, err)
	}
	if pos, err = b.Seek(-4, io.SeekEnd); err != nil || pos != 996 {
		t.Fatalf("SeekEnd = %d, %v; want 996", pos, err)
	}
	if n, err := io.ReadFull(b, buf); err != io.ErrUnexpectedEOF || n != 4 || buf[0] != byte(996%256) {
		t.Fatalf("read at end = %d, %v; want 4 bytes and ErrUnexpectedEOF", n, err)
	}
}

func TestBufferedReadSeekerSeekError(t *testing.T) {
	data := []byte("0123456789")
	b := newBufferedReadSeeker(&errReadSeeker{data: data, failAt: -1, seekFailAt: 1})
	buf := make([]byte, 2)
	if _, err := io.ReadFull(b, buf); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Seek(0, io.SeekStart); err == nil {
		t.Fatal("expected seek error")
	}
	// A failed seek keeps the buffered data, so reading continues where it was.
	if _, err := io.ReadFull(b, buf); err != nil || string(buf) != "23" {
		t.Fatalf("read after failed seek: %q %v", buf, err)
	}
}
