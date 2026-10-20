package core

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 网页里的 AES-GCM（web/crypto.js）必须和 Go 这边的帧格式互通：Go 加密的 JS 能解开，反过来也一样。
// 需要本机装有 node，否则跳过。
func TestWebCryptoInterop(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	key := testKey()
	type tc struct{ Plain, Sealed string }
	var cases []tc
	for _, n := range []int{0, 1, 15, 16, 17, encFrame - 1, encFrame, encFrame + 1, 3*encFrame + 5} {
		p := make([]byte, n)
		rand.Read(p)
		cases = append(cases, tc{hex.EncodeToString(p), hex.EncodeToString(sealAll(p, key))})
	}
	dir := t.TempDir()
	in, _ := json.Marshal(map[string]any{"key": hex.EncodeToString(key), "cases": cases})
	os.WriteFile(filepath.Join(dir, "in.json"), in, 0o600)
	script := `
const LC = require(process.argv[2]);
const fs = require('fs');
const h = s => Uint8Array.from(Buffer.from(s, 'hex'));
const { key, cases } = JSON.parse(fs.readFileSync(process.argv[3]));
const k = LC.newKey(h(key));
const out = cases.map(c => {
  const got = Buffer.from(LC.open(k, h(c.Sealed))).toString('hex');
  if (got !== c.Plain) throw new Error('open mismatch, len ' + c.Plain.length / 2);
  // 分段喂入（模拟边下载边解密）
  const o = new LC.Opener(k), parts = [], s = h(c.Sealed);
  for (let i = 0; i < s.length; i += 1000) parts.push(...o.push(s.subarray(i, i + 1000)));
  o.end();
  if (Buffer.concat(parts).toString('hex') !== c.Plain) throw new Error('chunked open mismatch');
  // 改一个字节必须报错
  if (s.length > 20) { const bad = s.slice(); bad[bad.length - 3] ^= 1; let ok = false; try { LC.open(k, bad); } catch { ok = true; } if (!ok) throw new Error('tamper not detected'); }
  return Buffer.from(LC.seal(k, h(c.Plain))).toString('hex');
});
process.stdout.write(JSON.stringify(out));
`
	sp := filepath.Join(dir, "t.js")
	os.WriteFile(sp, []byte(script), 0o600)
	abs, _ := filepath.Abs("web/crypto.js")
	res, err := exec.Command(node, sp, abs, filepath.Join(dir, "in.json")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, res)
	}
	var sealed []string
	if err := json.Unmarshal(res, &sealed); err != nil {
		t.Fatalf("bad output: %s", res)
	}
	for i, s := range sealed {
		b, _ := hex.DecodeString(s)
		got, err := io.ReadAll(newOpenReader(bytes.NewReader(b), key))
		if err != nil || hex.EncodeToString(got) != cases[i].Plain {
			t.Fatalf("case %d: Go cannot open JS output: %v", i, err)
		}
	}
}
