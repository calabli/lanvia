package core

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
)

// Content encryption between the nodes (AES-256-GCM, the key is derived from the pairing code and differs from the signing key).
//
// Stream format: a 12 byte random nonce prefix, followed by frames:
//
//	[4 bytes: ciphertext length, the top bit marks the last frame][ciphertext]
//
// The nonce of frame i = the last 8 bytes of the prefix XOR i; the additional data = 8 bytes of frame number + 1 byte "last frame" flag.
// So tampering with a frame, reordering the frames or truncating the stream (missing the last frame) is always detected. Encrypting per frame keeps large files and Range requests from being read into memory entirely.

const (
	encFrame  = 64 << 10
	finalFlag = 1 << 31
)

var errEncTruncated = errors.New("加密数据不完整")

func newAEAD(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // the key is a fixed 32 bytes, this cannot fail
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}

func frameNonce(base [12]byte, i uint64) []byte {
	n := base
	binary.BigEndian.PutUint64(n[4:], binary.BigEndian.Uint64(base[4:])^i)
	return n[:]
}

func frameAAD(i uint64, final bool) []byte {
	a := make([]byte, 9)
	binary.BigEndian.PutUint64(a, i)
	if final {
		a[8] = 1
	}
	return a
}

// sealWriter encrypts the plaintext written to it and writes it to w; Close writes the last frame (it must be called).
type sealWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	base    [12]byte
	i       uint64
	buf     []byte // plaintext waiting to be encrypted, at most one frame, reused
	ct      []byte // ciphertext buffer, reused
	started bool
	closed  bool
}

func newSealWriter(w io.Writer, key []byte) *sealWriter {
	s := &sealWriter{w: w, aead: newAEAD(key), buf: make([]byte, 0, encFrame)}
	rand.Read(s.base[:])
	return s
}

func (s *sealWriter) frame(plain []byte, final bool) error {
	if !s.started {
		if _, err := s.w.Write(s.base[:]); err != nil {
			return err
		}
		s.started = true
	}
	s.ct = s.aead.Seal(s.ct[:0], frameNonce(s.base, s.i), plain, frameAAD(s.i, final))
	ct := s.ct
	hdr := uint32(len(ct))
	if final {
		hdr |= finalFlag
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], hdr)
	if _, err := s.w.Write(h[:]); err != nil {
		return err
	}
	if _, err := s.w.Write(ct); err != nil {
		return err
	}
	s.i++
	return nil
}

func (s *sealWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if len(s.buf) == encFrame { // the buffer is full and more data follows: this frame is not the last one
			if err := s.frame(s.buf, false); err != nil {
				return 0, err
			}
			s.buf = s.buf[:0]
		}
		k := copy(s.buf[len(s.buf):encFrame], p)
		s.buf = s.buf[:len(s.buf)+k]
		p = p[k:]
	}
	return n, nil
}

func (s *sealWriter) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.frame(s.buf, true)
}

func sealAll(plain, key []byte) []byte {
	var b bytes.Buffer
	w := newSealWriter(&b, key)
	w.Write(plain)
	w.Close()
	return b.Bytes()
}

// openReader reads and verifies an encrypted stream and returns the plaintext. It returns an error when the data was tampered with or truncated.
type openReader struct {
	r    io.Reader
	aead cipher.AEAD
	base [12]byte
	i    uint64
	buf  []byte // plaintext not read yet
	ct   []byte // ciphertext buffer, reused
	pt   []byte // plaintext buffer, reused (the next frame is only decrypted once buf is read)
	head bool
	done bool
}

func newOpenReader(r io.Reader, key []byte) *openReader {
	return &openReader{r: r, aead: newAEAD(key)}
}

func (o *openReader) Read(p []byte) (int, error) {
	for len(o.buf) == 0 {
		if o.done {
			return 0, io.EOF
		}
		if !o.head {
			if _, err := io.ReadFull(o.r, o.base[:]); err != nil {
				return 0, errEncTruncated
			}
			o.head = true
		}
		var h [4]byte
		if _, err := io.ReadFull(o.r, h[:]); err != nil {
			return 0, errEncTruncated
		}
		hdr := binary.BigEndian.Uint32(h[:])
		final, n := hdr&finalFlag != 0, int(hdr&^finalFlag)
		if n > encFrame+o.aead.Overhead() {
			return 0, errors.New("加密帧过大")
		}
		if cap(o.ct) < n {
			o.ct = make([]byte, n)
		}
		ct := o.ct[:n]
		if _, err := io.ReadFull(o.r, ct); err != nil {
			return 0, errEncTruncated
		}
		plain, err := o.aead.Open(o.pt[:0], frameNonce(o.base, o.i), ct, frameAAD(o.i, final))
		if err != nil {
			return 0, errors.New("加密数据校验失败（配对码不一致或数据被篡改）")
		}
		o.i++
		o.pt, o.buf, o.done = plain, plain, final
	}
	n := copy(p, o.buf)
	o.buf = o.buf[n:]
	return n, nil
}

type openReadCloser struct {
	*openReader
	c io.Closer
}

func (o openReadCloser) Close() error { return o.c.Close() }

// encResponseWriter encrypts the response written to the other nodes. The body length changes, so Content-Length is removed.
type encResponseWriter struct {
	http.ResponseWriter
	seal        *sealWriter
	wroteHeader bool
}

func newEncResponseWriter(w http.ResponseWriter, key []byte) *encResponseWriter {
	return &encResponseWriter{ResponseWriter: w, seal: newSealWriter(w, key)}
}

func (e *encResponseWriter) WriteHeader(code int) {
	if e.wroteHeader {
		return
	}
	e.wroteHeader = true
	e.Header().Del("Content-Length")
	e.Header().Del("Content-Disposition") // the file name is plaintext and the peer already knows it
	e.Header().Set("X-Lanvia-Enc", "1")
	e.Header().Set("Content-Type", "application/octet-stream")
	e.ResponseWriter.WriteHeader(code)
}

func (e *encResponseWriter) Write(p []byte) (int, error) {
	if !e.wroteHeader {
		e.WriteHeader(http.StatusOK)
	}
	return e.seal.Write(p)
}

func (e *encResponseWriter) Close() {
	if !e.wroteHeader {
		e.WriteHeader(http.StatusOK)
	}
	e.seal.Close()
}
