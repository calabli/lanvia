'use strict';

/* 在浏览器里把文件夹打包成 zip（不压缩）。
   只为计算 CRC 流式读一遍文件，最终的 Blob 直接引用原文件，不会把整个文件夹读进内存。
   不支持 Zip64：总大小不超过 4 GB、文件数不超过 65535（上传本身也限制在 4 GB）。 */

const Zip = (() => {
  const TABLE = new Uint32Array(256).map((_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });

  async function crc32(file, onBytes) {
    let c = 0xffffffff;
    const reader = file.stream().getReader();
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      for (let i = 0; i < value.length; i++) c = TABLE[(c ^ value[i]) & 0xff] ^ (c >>> 8);
      onBytes(value.length);
    }
    return (c ^ 0xffffffff) >>> 0;
  }

  function dosDateTime(ms) {
    const d = new Date(ms || Date.now());
    const time = (d.getHours() << 11) | (d.getMinutes() << 5) | (d.getSeconds() >> 1);
    const date = (Math.max(0, d.getFullYear() - 1980) << 9) | ((d.getMonth() + 1) << 5) | d.getDate();
    return [time, date];
  }

  // 系统自动生成、对方用不到的文件
  const JUNK = /(^|\/)(\.DS_Store|Thumbs\.db|desktop\.ini|__MACOSX(\/.*)?|\._[^/]*)$/i;

  /**
   * entries: [{ path: 'Folder/a/b.txt', file: File }]，目录项 path 以 / 结尾、file 为空
   * onProgress(doneBytes, totalBytes)
   */
  async function build(entries, onProgress = () => {}) {
    entries = entries.filter(e => !JUNK.test(e.path.replace(/\/$/, '')));
    if (entries.length > 65535) throw new Error('文件太多（超过 65535 个），请分开发送');
    const total = entries.reduce((s, e) => s + (e.file?.size || 0), 0);
    if (total > 0xffffffff - 1e7) throw new Error('文件夹超过 4 GB，请分开发送');
    const enc = new TextEncoder();
    const parts = [], central = [];
    let offset = 0, done = 0;
    for (const e of entries) {
      const name = enc.encode(e.path);
      const isDir = !e.file;
      const size = isDir ? 0 : e.file.size;
      const crc = isDir ? 0 : await crc32(e.file, n => { done += n; onProgress(done, total); });
      const [time, date] = dosDateTime(e.file?.lastModified);

      const lh = new DataView(new ArrayBuffer(30));
      lh.setUint32(0, 0x04034b50, true);
      lh.setUint16(4, 20, true);
      lh.setUint16(6, 0x0800, true); // 文件名是 UTF-8
      lh.setUint16(8, 0, true);      // 不压缩
      lh.setUint16(10, time, true);
      lh.setUint16(12, date, true);
      lh.setUint32(14, crc, true);
      lh.setUint32(18, size, true);
      lh.setUint32(22, size, true);
      lh.setUint16(26, name.length, true);
      lh.setUint16(28, 0, true);
      parts.push(lh.buffer, name);
      if (!isDir) parts.push(e.file);

      const ch = new DataView(new ArrayBuffer(46));
      ch.setUint32(0, 0x02014b50, true);
      ch.setUint16(4, 20, true);
      ch.setUint16(6, 20, true);
      ch.setUint16(8, 0x0800, true);
      ch.setUint16(10, 0, true);
      ch.setUint16(12, time, true);
      ch.setUint16(14, date, true);
      ch.setUint32(16, crc, true);
      ch.setUint32(20, size, true);
      ch.setUint32(24, size, true);
      ch.setUint16(28, name.length, true);
      ch.setUint32(38, isDir ? 0x10 : 0, true); // 目录属性
      ch.setUint32(42, offset, true);
      central.push(ch.buffer, name);

      offset += 30 + name.length + size;
    }
    const cdSize = central.reduce((s, b) => s + b.byteLength, 0);
    const end = new DataView(new ArrayBuffer(22));
    end.setUint32(0, 0x06054b50, true);
    end.setUint16(8, entries.length, true);
    end.setUint16(10, entries.length, true);
    end.setUint32(12, cdSize, true);
    end.setUint32(16, offset, true);
    return new Blob([...parts, ...central, end.buffer], { type: 'application/zip' });
  }

  // 读取拖进来的目录（webkitGetAsEntry），空目录也保留
  async function readEntry(entry, prefix, out) {
    if (entry.isFile) {
      const file = await new Promise((res, rej) => entry.file(res, rej));
      out.push({ path: prefix + entry.name, file });
      return;
    }
    const reader = entry.createReader();
    const dir = prefix + entry.name + '/';
    let count = 0;
    for (;;) {
      const batch = await new Promise((res, rej) => reader.readEntries(res, rej));
      if (!batch.length) break;
      for (const child of batch) { count++; await readEntry(child, dir, out); }
    }
    if (!count) out.push({ path: dir, file: null });
  }

  return { build, readEntry };
})();

if (typeof module !== 'undefined') module.exports = Zip;
