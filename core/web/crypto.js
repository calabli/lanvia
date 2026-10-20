'use strict';

// 手机浏览器与电脑之间的内容加密。局域网走 http，浏览器的 crypto.subtle 不可用，所以自带 AES-256-GCM。
// 流格式与电脑之间的完全相同（见 core/crypto.go）：12 字节随机前缀，之后是若干帧
//   [4 字节：密文长度，最高位表示最后一帧][密文 + 16 字节校验]
// 第 i 帧的 nonce = 前缀的后 8 字节 XOR i，附加数据 = 8 字节帧序号 + 1 字节“最后一帧”标志。
// 这个文件不依赖其他脚本（测试时直接用 node 加载）。

const LC = (() => {
  const FRAME = 64 << 10, FINAL = 0x80000000, TAG = 16;

  /* ---- AES（只需要加密方向：GCM 解密也是用加密生成密钥流） ---- */
  const SBOX = new Uint8Array(256);
  const TE = [new Uint32Array(256), new Uint32Array(256), new Uint32Array(256), new Uint32Array(256)];
  (() => {
    const d = new Uint8Array(256), th = new Uint8Array(256);
    for (let i = 0; i < 256; i++) th[(d[i] = (i << 1) ^ ((i >> 7) * 283)) ^ i] = i;
    for (let x = 0, xInv = 0, x2; !SBOX[x]; x ^= x2 || 1, xInv = th[xInv] || 1) {
      let s = xInv ^ (xInv << 1) ^ (xInv << 2) ^ (xInv << 3) ^ (xInv << 4);
      s = (s >> 8) ^ (s & 255) ^ 99;
      SBOX[x] = s;
      x2 = d[x];
      let t = (d[s] * 0x101) ^ (s * 0x1010100);
      for (let i = 0; i < 4; i++) TE[i][x] = t = (t << 24) ^ (t >>> 8);
    }
  })();

  function expandKey(key) {
    const w = new Uint32Array(60);
    for (let i = 0; i < 8; i++) w[i] = (key[4 * i] << 24) | (key[4 * i + 1] << 16) | (key[4 * i + 2] << 8) | key[4 * i + 3];
    for (let i = 8, rcon = 1; i < 60; i++) {
      let t = w[i - 1];
      if (i % 8 === 0 || i % 8 === 4) {
        t = (SBOX[t >>> 24] << 24) ^ (SBOX[(t >> 16) & 255] << 16) ^ (SBOX[(t >> 8) & 255] << 8) ^ SBOX[t & 255];
        if (i % 8 === 0) { t = (t << 8) ^ (t >>> 24) ^ (rcon << 24); rcon = (rcon << 1) ^ ((rcon >> 7) * 283); }
      }
      w[i] = w[i - 8] ^ t;
    }
    return w;
  }

  // 加密一个块：输入 4 个 32 位字，结果写入 out
  function aesBlock(k, a, b, c, d, out) {
    const [t0, t1, t2, t3] = TE;
    a ^= k[0]; b ^= k[1]; c ^= k[2]; d ^= k[3];
    let ki = 4;
    for (let r = 0; r < 13; r++) {
      const a2 = t0[a >>> 24] ^ t1[(b >> 16) & 255] ^ t2[(c >> 8) & 255] ^ t3[d & 255] ^ k[ki];
      const b2 = t0[b >>> 24] ^ t1[(c >> 16) & 255] ^ t2[(d >> 8) & 255] ^ t3[a & 255] ^ k[ki + 1];
      const c2 = t0[c >>> 24] ^ t1[(d >> 16) & 255] ^ t2[(a >> 8) & 255] ^ t3[b & 255] ^ k[ki + 2];
      d = t0[d >>> 24] ^ t1[(a >> 16) & 255] ^ t2[(b >> 8) & 255] ^ t3[c & 255] ^ k[ki + 3];
      a = a2; b = b2; c = c2; ki += 4;
    }
    for (let i = 0; i < 4; i++) {
      out[i] = (SBOX[a >>> 24] << 24) ^ (SBOX[(b >> 16) & 255] << 16) ^ (SBOX[(c >> 8) & 255] << 8) ^ SBOX[d & 255] ^ k[ki++];
      const t = a; a = b; b = c; c = d; d = t;
    }
  }

  /* ---- GHASH：按字节查表（16 个位置 × 256 个取值，每项 128 位），每块只要 16 次查表 ---- */
  function ghashTable(h) {
    const V = new Uint32Array(128 * 4);
    V.set(h);
    for (let i = 1; i < 128; i++) { // V[i] = H · x^i：整体右移一位，移出 1 时异或 0xE1
      const p = (i - 1) * 4, q = i * 4, lsb = V[p + 3] & 1;
      V[q] = (V[p] >>> 1) ^ (lsb ? 0xe1000000 : 0);
      V[q + 1] = (V[p + 1] >>> 1) | (V[p] << 31);
      V[q + 2] = (V[p + 2] >>> 1) | (V[p + 1] << 31);
      V[q + 3] = (V[p + 3] >>> 1) | (V[p + 2] << 31);
    }
    const T = new Uint32Array(16 * 256 * 4);
    for (let j = 0; j < 16; j++) {
      for (let b = 1; b < 256; b++) {
        const low = b & -b, k = 7 - Math.log2(low), src = (j * 256 + (b ^ low)) * 4, dst = (j * 256 + b) * 4, v = (8 * j + k) * 4;
        for (let w = 0; w < 4; w++) T[dst + w] = T[src + w] ^ V[v + w];
      }
    }
    return T;
  }
  // y ← (y ⊕ 块) · H
  function ghashMul(T, y) {
    let z0 = 0, z1 = 0, z2 = 0, z3 = 0;
    for (let j = 0; j < 16; j++) {
      const byte = (y[j >> 2] >>> (24 - 8 * (j & 3))) & 255, o = (j * 256 + byte) * 4;
      z0 ^= T[o]; z1 ^= T[o + 1]; z2 ^= T[o + 2]; z3 ^= T[o + 3];
    }
    y[0] = z0; y[1] = z1; y[2] = z2; y[3] = z3;
  }

  const rd = (b, o) => ((b[o] << 24) | (b[o + 1] << 16) | (b[o + 2] << 8) | b[o + 3]) >>> 0;
  // 从 b[o..o+n) 读出最多 16 字节（不足补 0）作为 4 个字
  function rdBlock(b, o, n, out) {
    if (n === 16) { out[0] = rd(b, o); out[1] = rd(b, o + 4); out[2] = rd(b, o + 8); out[3] = rd(b, o + 12); return; }
    out[0] = out[1] = out[2] = out[3] = 0;
    for (let i = 0; i < n; i++) out[i >> 2] |= b[o + i] << (24 - 8 * (i & 3));
  }

  function newKey(raw) {
    const k = expandKey(raw), h = new Uint32Array(4);
    aesBlock(k, 0, 0, 0, 0, h);
    return { k, T: ghashTable(h) };
  }

  // GCM：plain 加密后写入 out[o..]（长度相同），返回 16 字节校验。decrypt 时 input 是密文。
  function gcm(key, nonce, aad, input, out, o, decrypt) {
    const { k, T } = key;
    const n0 = rd(nonce, 0), n1 = rd(nonce, 4), n2 = rd(nonce, 8);
    const y = new Uint32Array(4), blk = new Uint32Array(4), ks = new Uint32Array(4);
    // 附加数据
    for (let i = 0; i < aad.length; i += 16) {
      rdBlock(aad, i, Math.min(16, aad.length - i), blk);
      y[0] ^= blk[0]; y[1] ^= blk[1]; y[2] ^= blk[2]; y[3] ^= blk[3]; ghashMul(T, y);
    }
    const len = input.length;
    for (let i = 0, ctr = 2; i < len; i += 16, ctr++) {
      const n = Math.min(16, len - i);
      aesBlock(k, n0, n1, n2, ctr, ks);
      if (decrypt) { rdBlock(input, i, n, blk); y[0] ^= blk[0]; y[1] ^= blk[1]; y[2] ^= blk[2]; y[3] ^= blk[3]; ghashMul(T, y); }
      for (let j = 0; j < n; j++) out[o + i + j] = input[i + j] ^ (ks[j >> 2] >>> (24 - 8 * (j & 3)));
      if (!decrypt) { rdBlock(out, o + i, n, blk); y[0] ^= blk[0]; y[1] ^= blk[1]; y[2] ^= blk[2]; y[3] ^= blk[3]; ghashMul(T, y); }
    }
    y[1] ^= aad.length * 8; y[3] ^= len * 8; // 长度块（都远小于 2^32 位）
    ghashMul(T, y);
    aesBlock(k, n0, n1, n2, 1, ks);
    const tag = new Uint8Array(16);
    for (let j = 0; j < 16; j++) tag[j] = (y[j >> 2] ^ ks[j >> 2]) >>> (24 - 8 * (j & 3));
    return tag;
  }

  function frameNonce(base, i) {
    const n = base.slice();
    const v = (rd(n, 8) ^ i) >>> 0;
    n[8] = v >>> 24; n[9] = v >>> 16; n[10] = v >>> 8; n[11] = v;
    return n;
  }
  function frameAAD(i, final) {
    const a = new Uint8Array(9);
    a[4] = i >>> 24; a[5] = i >>> 16; a[6] = i >>> 8; a[7] = i; a[8] = final ? 1 : 0;
    return a;
  }

  // 加密器：按顺序喂入明文块，产出带帧的密文块。
  class Sealer {
    constructor(key) {
      this.key = key; this.i = 0;
      this.base = crypto.getRandomValues(new Uint8Array(12));
      this.started = false;
    }
    frame(plain, final) {
      const head = this.started ? 0 : 12;
      const out = new Uint8Array(head + 4 + plain.length + TAG);
      if (head) { out.set(this.base); this.started = true; }
      const len = plain.length + TAG, hdr = (final ? FINAL : 0) + len;
      out[head] = hdr >>> 24; out[head + 1] = hdr >>> 16; out[head + 2] = hdr >>> 8; out[head + 3] = hdr;
      const tag = gcm(this.key, frameNonce(this.base, this.i), frameAAD(this.i, final), plain, out, head + 4, false);
      out.set(tag, head + 4 + plain.length);
      this.i++;
      return out;
    }
  }

  // 一次性加密整段数据
  function seal(key, plain) {
    const s = new Sealer(key), parts = [];
    let total = 0;
    for (let o = 0; ; o += FRAME) {
      const end = Math.min(plain.length, o + FRAME);
      const f = s.frame(plain.subarray(o, end), end === plain.length);
      parts.push(f); total += f.length;
      if (end === plain.length) break;
    }
    return concat(parts, total);
  }

  // 解密器：可以分段喂入（边下载边解密），每次返回已解出的明文块。
  class Opener {
    constructor(key) { this.key = key; this.buf = new Uint8Array(0); this.base = null; this.i = 0; this.done = false; }
    push(chunk) {
      if (this.done) { if (chunk.length) throw new Error('加密数据有多余内容'); return []; }
      let b = this.buf.length ? concat([this.buf, chunk], this.buf.length + chunk.length) : chunk;
      const out = [];
      let o = 0;
      if (!this.base) {
        if (b.length < 12) { this.buf = b.slice(); return out; }
        this.base = b.slice(0, 12); o = 12;
      }
      while (!this.done && b.length - o >= 4) {
        const hdr = rd(b, o), final = hdr >= FINAL, n = hdr % FINAL;
        if (n < TAG || n > FRAME + TAG) throw new Error('加密帧长度不对');
        if (b.length - o - 4 < n) break;
        const ct = b.subarray(o + 4, o + 4 + n - TAG), tag = b.subarray(o + 4 + n - TAG, o + 4 + n);
        const plain = new Uint8Array(ct.length);
        const want = gcm(this.key, frameNonce(this.base, this.i), frameAAD(this.i, final), ct, plain, 0, true);
        let diff = 0;
        for (let j = 0; j < 16; j++) diff |= want[j] ^ tag[j];
        if (diff) throw new Error('加密数据校验失败（配对码不一致或数据被篡改）');
        out.push(plain);
        this.i++; this.done = final; o += 4 + n;
      }
      if (this.done && o < b.length) throw new Error('加密数据有多余内容');
      this.buf = b.slice(o);
      return out;
    }
    end() { if (!this.done) throw new Error('加密数据不完整'); }
  }

  function open(key, data) {
    const o = new Opener(key), parts = o.push(data);
    o.end();
    return concat(parts, parts.reduce((n, p) => n + p.length, 0));
  }

  function concat(parts, total) {
    if (parts.length === 1) return parts[0];
    const out = new Uint8Array(total);
    let o = 0;
    for (const p of parts) { out.set(p, o); o += p.length; }
    return out;
  }

  const b64 = u8 => {
    let s = '';
    for (let i = 0; i < u8.length; i += 0x8000) s += String.fromCharCode.apply(null, u8.subarray(i, i + 0x8000));
    return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  };
  const unb64 = s => {
    const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/'));
    const u8 = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
    return u8;
  };

  return { FRAME, newKey, Sealer, seal, Opener, open, b64, unb64, concat };
})();

if (typeof module !== 'undefined') module.exports = LC;
