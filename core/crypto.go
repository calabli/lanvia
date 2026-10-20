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

// 节点之间的内容加密（AES-256-GCM，密钥由配对码派生，与签名用的密钥不同）。
//
// 流格式：开头 12 字节随机 nonce 前缀，之后是若干帧：
//
//	[4 字节：密文长度，最高位表示是否最后一帧][密文]
//
// 第 i 帧的 nonce = 前缀的后 8 字节 XOR i；附加数据 = 8 字节帧序号 + 1 字节“最后一帧”标志。
// 所以帧被篡改、调换顺序、截断（缺最后一帧）都会被发现。按帧加密，大文件和 Range 请求不必整个读进内存。

const (
	encFrame  = 64 << 10
	finalFlag = 1 << 31
)

var errEncTruncated = errors.New("加密数据不完整")

func newAEAD(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // 密钥固定 32 字节，不会出错
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

// sealWriter 把写入的明文加密后写到 w；Close 时写出最后一帧（必须调用）。
type sealWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	base    [12]byte
	i       uint64
	buf     []byte // 待加密的明文，最多一帧，复用
	ct      []byte // 密文缓冲，复用
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
		if len(s.buf) == encFrame { // 缓冲满了，并且后面还有数据：这一帧不是最后一帧
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

// openReader 读取并校验加密流，返回明文。数据被篡改或截断时返回错误。
type openReader struct {
	r    io.Reader
	aead cipher.AEAD
	base [12]byte
	i    uint64
	buf  []byte // 尚未读走的明文
	ct   []byte // 密文缓冲，复用
	pt   []byte // 明文缓冲，复用（buf 读完后才会解下一帧）
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

// encResponseWriter 加密写给其他节点的响应。正文长度会变，所以去掉 Content-Length。
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
