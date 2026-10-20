package core

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"testing"
)

func testKey() []byte { return deriveCreds("K7QFM2XD9P").enc }

func readAll(t *testing.T, enc, key []byte) ([]byte, error) {
	t.Helper()
	return io.ReadAll(newOpenReader(bytes.NewReader(enc), key))
}

func TestEncRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, encFrame - 1, encFrame, encFrame + 1, 3*encFrame + 123} {
		plain := make([]byte, size)
		rand.Read(plain)
		var b bytes.Buffer
		w := newSealWriter(&b, testKey())
		// 分成不规则的小块写入，模拟流式响应
		for p := plain; len(p) > 0; {
			n := min(len(p), 7777)
			w.Write(p[:n])
			p = p[n:]
		}
		w.Close()
		got, err := readAll(t, b.Bytes(), testKey())
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("size %d: err=%v equal=%v", size, err, bytes.Equal(got, plain))
		}
		if size > 64 && bytes.Contains(b.Bytes(), plain[:64]) {
			t.Fatalf("size %d: ciphertext contains plaintext", size)
		}
	}
}

func TestEncDetectsTampering(t *testing.T) {
	plain := make([]byte, 2*encFrame+10)
	rand.Read(plain)
	enc := sealAll(plain, testKey())

	// 改一个字节
	bad := append([]byte(nil), enc...)
	bad[len(bad)/2] ^= 1
	if _, err := readAll(t, bad, testKey()); err == nil {
		t.Fatal("tampered byte not detected")
	}
	// 截断：去掉最后一帧
	firstTwo := 12 + 2*(4+encFrame+16)
	if _, err := readAll(t, enc[:firstTwo], testKey()); err == nil {
		t.Fatal("truncation not detected")
	}
	// 调换前两帧
	f := 4 + encFrame + 16
	swapped := append([]byte(nil), enc[:12]...)
	swapped = append(swapped, enc[12+f:12+2*f]...)
	swapped = append(swapped, enc[12:12+f]...)
	swapped = append(swapped, enc[12+2*f:]...)
	if _, err := readAll(t, swapped, testKey()); err == nil {
		t.Fatal("reordering not detected")
	}
	// 把中间帧冒充成最后一帧
	fake := append([]byte(nil), enc[:12+f]...)
	binary.BigEndian.PutUint32(fake[12:], binary.BigEndian.Uint32(fake[12:])|finalFlag)
	if _, err := readAll(t, fake, testKey()); err == nil {
		t.Fatal("forged final flag not detected")
	}
	// 配对码不同
	if _, err := readAll(t, enc, deriveCreds("ABCDEFGHJK").enc); err == nil {
		t.Fatal("wrong key not detected")
	}
}

// 加密传输 16 MB（大文件中转的典型路径），关注每次操作的内存分配
func BenchmarkEncStream(b *testing.B) {
	plain := make([]byte, 16<<20)
	key := testKey()
	b.SetBytes(int64(len(plain)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w := newSealWriter(&buf, key)
		for p := plain; len(p) > 0; { // 按 32 KB 写入，和 io.Copy 一样
			n := min(len(p), 32<<10)
			w.Write(p[:n])
			p = p[n:]
		}
		w.Close()
		io.Copy(io.Discard, newOpenReader(&buf, key))
	}
}
