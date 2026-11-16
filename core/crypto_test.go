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
		// Write it in irregular small chunks, simulating a streaming response
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

	// Flip one byte
	bad := append([]byte(nil), enc...)
	bad[len(bad)/2] ^= 1
	if _, err := readAll(t, bad, testKey()); err == nil {
		t.Fatal("tampered byte not detected")
	}
	// Truncation: drop the last frame
	firstTwo := 12 + 2*(4+encFrame+16)
	if _, err := readAll(t, enc[:firstTwo], testKey()); err == nil {
		t.Fatal("truncation not detected")
	}
	// Swap the first two frames
	f := 4 + encFrame + 16
	swapped := append([]byte(nil), enc[:12]...)
	swapped = append(swapped, enc[12+f:12+2*f]...)
	swapped = append(swapped, enc[12:12+f]...)
	swapped = append(swapped, enc[12+2*f:]...)
	if _, err := readAll(t, swapped, testKey()); err == nil {
		t.Fatal("reordering not detected")
	}
	// Pass a middle frame off as the last one
	fake := append([]byte(nil), enc[:12+f]...)
	binary.BigEndian.PutUint32(fake[12:], binary.BigEndian.Uint32(fake[12:])|finalFlag)
	if _, err := readAll(t, fake, testKey()); err == nil {
		t.Fatal("forged final flag not detected")
	}
	// Different pairing code
	if _, err := readAll(t, enc, deriveCreds("ABCDEFGHJK").enc); err == nil {
		t.Fatal("wrong key not detected")
	}
}

// Encrypt and transfer 16 MB (a typical path for relaying large files), watching the memory allocations of each operation
func BenchmarkEncStream(b *testing.B) {
	plain := make([]byte, 16<<20)
	key := testKey()
	b.SetBytes(int64(len(plain)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w := newSealWriter(&buf, key)
		for p := plain; len(p) > 0; { // written in 32 KB chunks, like io.Copy
			n := min(len(p), 32<<10)
			w.Write(p[:n])
			p = p[n:]
		}
		w.Close()
		io.Copy(io.Discard, newOpenReader(&buf, key))
	}
}
