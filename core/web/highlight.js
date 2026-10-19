'use strict';

/* 轻量代码高亮（预览窗口用）：识别常见语言，按 token 着色，再按行拆分以配合行号。
   不依赖外部库，局域网离线也能用。token 类型：k 关键字、s 字符串、c 注释、n 数字、
   f 函数名、p 属性 / 键名、t 标签、v 变量。 */

const HL = (() => {
  const words = s => new Set(s.split(/\s+/).filter(Boolean));
  const LANGS = {
    js: {
      name: 'JavaScript', line: '//', block: ['/*', '*/'], quotes: '"\'`',
      kw: words(`break case catch class const continue debugger default delete do else export extends finally for function if
        import in instanceof let new return super switch this throw try typeof var void while with yield async await of static
        get set null undefined true false interface type enum implements declare readonly public private protected namespace
        as from`),
    },
    go: {
      name: 'Go', line: '//', block: ['/*', '*/'], quotes: '"`\'',
      kw: words(`break case chan const continue default defer else fallthrough for func go goto if import interface map package
        range return select struct switch type var nil true false iota string int int8 int16 int32 int64 uint uint8 uint16
        uint32 uint64 byte rune error bool float32 float64 any make new len cap append panic recover`),
    },
    py: {
      name: 'Python', line: '#', quotes: '"\'', triple: true,
      kw: words(`False None True and as assert async await break class continue def del elif else except finally for from
        global if import in is lambda nonlocal not or pass raise return try while with yield self print`),
    },
    sh: {
      name: 'Shell', line: '#', quotes: '"\'', vars: true,
      kw: words(`if then else elif fi for while until do done case esac in function return export local echo cd sudo set
        unset exit source alias read shift trap`),
    },
    sql: {
      name: 'SQL', line: '--', block: ['/*', '*/'], quotes: '\'"', ci: true,
      kw: words(`select from where and or not insert into values update set delete create table drop alter index join left
        right inner outer full on group by order having limit offset as distinct union all null is like in between case
        when then else end primary key foreign references default asc desc count sum avg min max exists with returning`),
    },
    css: { name: 'CSS', block: ['/*', '*/'], quotes: '"\'', css: true, kw: new Set() },
    yaml: { name: 'YAML', line: '#', quotes: '"\'', yaml: true, kw: words('true false null yes no on off ~') },
    json: { name: 'JSON', quotes: '"', json: true, kw: words('true false null') },
    html: { name: 'HTML', markup: true },
  };

  function detect(t) {
    const s = t.trim();
    if (!s) return null;
    if (/^[[{]/.test(s)) { try { JSON.parse(s); return 'json'; } catch {} }
    if (/^<(!doctype|html|\?xml|[a-z][\w:-]*[\s>/])/i.test(s) && /<\/[a-z][\w:-]*>|\/>/i.test(s)) return 'html';
    if (/^\s*package \w+/m.test(s) || /^\s*func (\(\w+ \*?\w+\) )?\w+\(/m.test(s)) return 'go';
    if (/^\s*(def \w+\(|class \w+[(:]|from [\w.]+ import |import \w+\s*$)/m.test(s)) return 'py';
    if (/^\s*(select|insert|update|delete|create|alter|with)\b/i.test(s) && /\b(from|into|set|table|where|as)\b/i.test(s)) return 'sql';
    if (/^#!.*\b(ba|z)?sh\b/.test(s) || /^\s*(\$ |sudo |cd |ls |git |npm |pnpm |yarn |brew |curl |docker |kubectl |export \w+=)/m.test(s)) return 'sh';
    if (/^[^{}\n]+\{[^{}]*\b[\w-]+\s*:[^{}]*\}/m.test(s) && !/\b(function|const|let|var|return)\b|=>/.test(s)) return 'css';
    const ls = s.split('\n');
    if (ls.length >= 2 && /^[\w.-]+:(\s|$)/m.test(s) && !/[;{}]\s*$/m.test(s) &&
      ls.filter(l => !l.trim() || /^\s*([\w.-]+:(\s|$)|-\s|#)/.test(l)).length / ls.length > 0.75) return 'yaml';
    if (/\b(function|const|let|var|import|export|console)\b|=>/.test(s) && /[;{}()]/.test(s)) return 'js';
    return null;
  }

  // 通用分词：注释、字符串、数字、标识符，其余原样保留
  function tokenize(src, L) {
    if (L.markup) return tokenizeMarkup(src);
    const out = [];
    const push = (type, text) => {
      if (!text) return;
      const last = out[out.length - 1];
      if (last && last[0] === type) last[1] += text; else out.push([type, text]);
    };
    let i = 0;
    const n = src.length;
    let lineStart = true;
    while (i < n) {
      const c = src[i];
      const rest = (len) => src.substr(i, len);
      if (L.block && rest(L.block[0].length) === L.block[0]) {
        let j = src.indexOf(L.block[1], i + L.block[0].length);
        j = j < 0 ? n : j + L.block[1].length;
        push('c', src.slice(i, j)); i = j; continue;
      }
      if (L.line && rest(L.line.length) === L.line && !(L.vars && c === '#' && i > 0 && /[\w$]/.test(src[i - 1]))) {
        let j = src.indexOf('\n', i); if (j < 0) j = n;
        push('c', src.slice(i, j)); i = j; continue;
      }
      if (L.triple && (rest(3) === '"""' || rest(3) === "'''")) {
        let j = src.indexOf(rest(3), i + 3); j = j < 0 ? n : j + 3;
        push('s', src.slice(i, j)); i = j; continue;
      }
      if (L.quotes && L.quotes.includes(c)) {
        let j = i + 1;
        while (j < n && src[j] !== c && !(src[j] === '\n' && c !== '`')) j += src[j] === '\\' ? 2 : 1;
        j = Math.min(n, j + 1);
        const text = src.slice(i, j);
        // JSON / YAML / CSS 里冒号前的字符串是键名
        const isKey = (L.json || L.yaml) && /^\s*:/.test(src.slice(j, j + 20));
        push(isKey ? 'p' : 's', text); i = j; continue;
      }
      if (/[0-9]/.test(c) && (i === 0 || !/[\w$]/.test(src[i - 1]))) {
        const m = /^(0[xX][\da-fA-F_]+|\d[\d_]*(\.\d+)?([eE][+-]?\d+)?)/.exec(src.slice(i, i + 40));
        push('n', m[0]); i += m[0].length; continue;
      }
      if (L.css && c === '#' && /[\da-fA-F]/.test(src[i + 1] || '')) {
        const m = /^#[\da-fA-F]{3,8}/.exec(src.slice(i, i + 10));
        if (m) { push('n', m[0]); i += m[0].length; continue; }
      }
      if (L.vars && c === '$') {
        const m = /^\$(\{[^}\n]*\}|[\w@*#?$!-]+)/.exec(src.slice(i, i + 80));
        if (m) { push('v', m[0]); i += m[0].length; continue; }
      }
      if (/[A-Za-z_$@-]/.test(c) && (c !== '-' || L.css || L.yaml)) {
        const m = /^[A-Za-z_$@][\w$-]*/.exec(src.slice(i, i + 200)) || ['-'];
        let w = m[0];
        if (!L.css && !L.yaml) w = w.replace(/-.*$/, '') || w; // 普通语言里 - 是运算符
        const after = src.slice(i + w.length, i + w.length + 20);
        const key = L.ci ? w.toLowerCase() : w;
        let type = '';
        if (L.kw.has(key)) type = 'k';
        else if (L.css && (w[0] === '@')) type = 'k';
        else if ((L.css || L.yaml) && /^\s*:/.test(after) && (L.yaml ? lineStart : true)) type = 'p';
        else if (!L.css && /^\s*\(/.test(after)) type = 'f';
        push(type, w); i += w.length; lineStart = false; continue;
      }
      push('', c);
      if (c === '\n') lineStart = true; else if (!/\s/.test(c) && c !== '-') lineStart = false;
      i++;
    }
    return out;
  }

  function tokenizeMarkup(src) {
    const out = [];
    const re = /<!--[\s\S]*?(-->|$)|<\/?[A-Za-z][\w:-]*|\/?>|"[^"]*"|'[^']*'|\s+|[\w:-]+(?==)|[^<>"'=\s]+|[\s\S]/g;
    let inTag = false, m;
    while ((m = re.exec(src))) {
      const t = m[0];
      if (t.startsWith('<!--')) out.push(['c', t]);
      else if (t[0] === '<') { out.push(['t', t]); inTag = true; }
      else if (t === '>' || t === '/>') { out.push(['t', t]); inTag = false; }
      else if (inTag && (t[0] === '"' || t[0] === "'")) out.push(['s', t]);
      else if (inTag && /^[\w:-]+$/.test(t) && src[re.lastIndex] === '=') out.push(['p', t]);
      else out.push(['', t]);
    }
    return out;
  }

  const escape = s => s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

  // 返回每一行的 HTML（跨行的注释 / 字符串会在每行分别闭合）
  function lines(src, lang) {
    const L = LANGS[lang];
    const tokens = L && src.length <= 200000 ? tokenize(src, L) : [['', src]];
    const rows = [''];
    for (const [type, text] of tokens) {
      const parts = text.split('\n');
      parts.forEach((p, k) => {
        if (k > 0) rows.push('');
        if (p) rows[rows.length - 1] += type ? `<span class="tk-${type}">${escape(p)}</span>` : escape(p);
      });
    }
    return rows;
  }

  return { detect, lines, name: lang => LANGS[lang]?.name || '' };
})();
