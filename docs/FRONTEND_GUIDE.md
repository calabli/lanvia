# Lanvia 前端重构设计指导文件

> **状态：已实现（v0.14.x 之后）。** 与本文的差异：托盘面板保留 0.14.2 的布局（没有底部快捷键提示条）；抽屉第三个分段沿用“评论”而不是“备注”；`--surface` 仍为不透明底色，毛玻璃效果用新增的 `--glass` 变量。
>
> 面向 `core/web/`（零构建原生 HTML / CSS / JS，通过 `go:embed` 内嵌于 Go 节点，同时服务于桌面主窗口、托盘快捷面板、独立预览窗与手机扫码浏览器）。

---

## 1. 设计目标与核心原则

Lanvia 的本质是**设备之间的静默局域网能量通道**——复制即流转、粘贴即送达，而不是一个厚重的邮箱或后台管理系统。本次交互与视觉重构围绕四条铁律展开：

1. **操作极少（Zero-Friction）**：最高频动作一步直达——**单击卡片即复制**、**任意位置 `⌘V` 或拖放即发送**、**清单项卡片内直接勾选**。
2. **交互自然（Ambient & Fluid）**：取消“搜索框 / 新建按钮 / 底部大输入框”的割裂感，用一根**全能感知输入栏（Omni-Bar）**统一搜索与发送；重操作（编辑、版本历史、评论、标签、过期时间）全部按需收进右侧滑出抽屉。
3. **克制的科技感（Refined Spatial Tech）**：用微弱点阵光场、毛玻璃层次、设备节点间的动态光束连线（Beam）以及等宽字体微标传达“局域网端到端加密直连”的精密感，避免花哨霓虹或低对比度暗黑风。
4. **元素做减法（Low Cognitive Load）**：从传统三栏邮箱式布局收缩为**单画布居中流**，主视图删除固定侧边栏、黑色割裂顶栏与常驻底部大输入框，首屏可交互控件数量减少 60% 以上。

---

## 2. 视觉系统与 Design Tokens

继续沿用无外部依赖的 CSS 变量体系，同时兼容现有 `--fs` 字体倍率与亮 / 暗双外观（`:root`、`[data-theme="dark"]` 与 `prefers-color-scheme: dark`）。

### 2.1 色彩与材质规范

```css
:root {
  --bg: #f6f7f9;
  --surface: rgba(255, 255, 255, 0.84);
  --surface-solid: #ffffff;
  --surface-2: #eff1f5;
  --border: rgba(15, 23, 42, 0.09);
  --border-strong: rgba(15, 23, 42, 0.16);
  --text: #111827;
  --muted: #6b7280;
  --accent: #0071e3;
  --accent-soft: rgba(0, 113, 227, 0.12);
  --ok: #16a34a;
  --warn: #d97706;
  --danger: #dc2626;
  --radius-card: 16px;
  --radius-pill: 999px;
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
}

:root[data-theme="dark"] {
  --bg: #090b10;
  --surface: rgba(22, 25, 34, 0.78);
  --surface-solid: #141720;
  --surface-2: #1d212e;
  --border: rgba(255, 255, 255, 0.09);
  --border-strong: rgba(255, 255, 255, 0.18);
  --text: #f3f4f6;
  --muted: #9ca3af;
  --accent: #3b82f6;
  --accent-soft: rgba(59, 130, 246, 0.18);
  --ok: #22c55e;
  --warn: #f59e0b;
  --danger: #ef4444;
}
```

### 2.2 科技感细节规范

- **空间点阵光场背景**：顶部极微弱的径向主色光晕 + 24px 细点阵纹理，不干扰正文阅读：
  ```css
  body.stream-mode {
    background-color: var(--bg);
    background-image:
      radial-gradient(ellipse 65% 38% at 50% -6%, color-mix(in srgb, var(--accent) 12%, transparent), transparent 70%),
      radial-gradient(color-mix(in srgb, var(--border-strong) 55%, transparent) 1px, transparent 1px);
    background-size: 100% 100%, 24px 24px;
  }
  ```
- **毛玻璃浮层（`.glass`）**：用于 Omni-Bar、设备节点胶囊、右侧抽屉与悬浮 HUD，`backdrop-filter: blur(18px) saturate(1.4)`。
- **动态光束连线**：设备节点之间使用细虚线 SVG 路径（`stroke-dasharray: 4 8`）做匀速流动动画；同步暂停时停止动画并降至 25% 透明度；遵循 `@media (prefers-reduced-motion: reduce)` 自动静止。
- **排版与信息密度**：
  - 最小可读字号不低于 `calc(12px * var(--fs))`（遵循 `--fs` 缩放）。
  - 元数据（时间、文件大小、版本号 `v3`、送达比 `3/3`、加密标识 `E2E`）使用等宽字体（`var(--mono)`）+ `font-variant-numeric: tabular-nums`。
- **动效反馈**：
  - 新条目到达：轻微下滑入场（`translateY(-8px) -> 0`，`280ms var(--ease-out)`）。
  - 单击复制成功：卡片边框短暂主色脉冲（`.copied-flash`，`480ms`）+ 底部极简悬浮胶囊提示。

---

## 3. 信息架构与三端布局

前端继续通过 URL 参数区分运行形态（定义在 `core/web/base.js`），共享同一套设计语言与状态机 `S`：

| 形态 | 判定条件 | 布局结构 |
| --- | --- | --- |
| **桌面主窗口 / 本机浏览器** | `!PANEL && !PREVIEW` | 单画布居中流（最大宽度 `780px`）+ 顶部设备光束栏 + Omni-Bar + 按需右侧毛玻璃抽屉（`420px`） |
| **托盘快捷面板** | `PANEL` (`?desktop=panel`, `380×600`) | 极简顶栏搜索/直发输入 + 纯净单列键盘列表 + 底部快捷键提示条，零多余按钮 |
| **独立悬浮预览窗** | `PREVIEW` (`?desktop=preview&id=...`) | 复用右侧抽屉内部内容区（`内容 / 历史 / 备注` 分段视图 + 置顶按钮 `#pvPin`） |
| **手机扫码端** | `!isLoopback` 且窄屏 (`≤650px`) | 轻顶栏（连接态 + 加密标记）+ 轻点复制卡片流 + 底部悬浮胶囊快发栏（适配 `env(safe-area-inset-bottom)`） |

### 3.1 桌面主界面线框结构

```text
┌──────────────────────────────────────────────────────────────────────┐
│  ● MacBook Pro ┄┄┄ ● iPhone 16 ┄┄┄ ● Studio PC   [+]    [● SYNC·E2E] │  ← 极简顶栏（含 Windows 原生控制区）
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│       ┌──────────────────────────────────────────────────────┐       │
│       │  🔍 输入即搜，回车即发，或 ⌘V 粘贴 / 拖入文件...  [📎 文件] │       │  ← Hero Omni-Bar（聚焦发光）
│       └──────────────────────────────────────────────────────┘       │
│       (全部)  文本  文件  清单  收藏            [多选]  CLICK TO COPY  │  ← 5 个轻量筛选胶囊
│                                                                      │
│       ┌──────────────────────────────────────────────────────┐       │
│       │ ━━ (最新条目顶部主色指示条)                            │       │
│       │ [Go] peers.go                          [★] [详情]    │       │  ← 悬停才出现 2 个轻按钮
│       │ since := peer.lastNow - 30_000 ...                   │       │  ← 单击卡片直接复制
│       │ MacBook Pro · 刚刚 · ● 3/3 · v2                      │       │
│       └──────────────────────────────────────────────────────┘       │
│                                                                      │
│       ┌──────────────────────────────────────────────────────┐       │
│       │ 今天下班前                                            │       │
│       │ [✓] 确认极简光束交互方案                              │       │  ← 卡片内直接勾选同步
│       │ [ ] 接入 core/web                                    │       │
│       │ MacBook Pro · 14 分钟前 · ● 3/3                      │       │
│       └──────────────────────────────────────────────────────┘       │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
  * 点击「详情」或双击卡片 -> 右侧滑出 420px 毛玻璃抽屉（不挤压或隐藏主画布）
```

---

## 4. 核心交互协议（减法规范）

### 4.1 全能感知输入栏（Omni-Bar）

合并原有的 `#search` 搜索框与 `#composer` 输入区，交互规则如下：

1. **输入即筛选**：输入字符时即时更新 `S.filter`，触发 `renderList()` 与长文本全文检索 `deepSearch()`。
2. **回车即发送**：
   - 当输入非空且用户按 `Enter`（非输入法组合态 `!e.isComposing`）或点击右侧浮现的 `发送 ↩` 按钮时，直接调用 `sendText()` 发出并清空输入框。
   - **智能清单识别**：输入包含多行且以 `- ` / `* ` 开头时，直接调用清单创建接口 `POST /api/todo`；或在发送按钮旁提供轻切换。
   - `Shift + Enter` 插入换行，文本框自动增高（`1 ~ 5` 行）。
3. **全局直发（零点击）**：
   - 页面任意非输入焦点处按 `⌘V` / `Ctrl+V`：文件/截图直接走 `uploadFile()`，纯文本直接发出（复用 `main.js` 现有全局 `paste` 逻辑）。
   - 拖入文件/文件夹到窗口任意处：全屏浮现毛玻璃能量投放层 `#drop`，松手直传（**必须保留** `main.js` 中在 `drop` 事件内同步读取 `webkitGetAsEntry()` 的目录上传逻辑）。
4. **手动发送本机剪贴板（仅按需出现）**：
   - 当本机关闭「主动复制」且本机剪贴板非空时，在 Omni-Bar 正下方紧凑展示一行 `#clipCard`（单行预览 + 右侧「发送」按钮），避免长期占空间。

### 4.2 卡片零步骤交互

- **单击卡片主体** = 立即执行主动作：
  - 文本 / 链接 / 清单：`copyItem(it)` 写入剪贴板 + `.copied-flash` 脉冲 + 底部悬浮 HUD 提示。
  - 若用户在卡片内拖选了部分文字（`getSelection().toString()` 非空），不触发整卡复制。
  - 点击卡片内 `<a>` 链接、验证码快捷按钮（`findCode`）、清单复选框（`[data-act="todo-check"]`）时阻断冒泡，仅执行对应动作。
  - 文件/图片卡片：桌面端点击「保存/打开」或进入预览，手机端触发解密下载/预览。
- **悬停卡片**：仅在右上角浮现 **2 个轻量按钮**（`★ 收藏`、`详情`），不展示 6~7 个排成一排的小图标。
- **双击卡片 / 点击「详情」**：打开右侧滑出抽屉（桌面宽屏）或全屏 Sheet（窄屏）。

### 4.3 顶部设备光束与单一状态胶囊

- **设备光束（替代侧边栏设备列表）**：
  - 遍历 `S.devices` 渲染在线设备胶囊，相邻节点间绘制细虚线流动 SVG；节点超过 4 台时折叠为 `+N` 胶囊，点击打开 `#devicesDlg`。
  - 点击某个设备节点 = 切换 `S.from` 筛选（高亮为主色，再点取消）。
  - 节点末尾保留一个圆形虚线 `+` 按钮，点击弹出扫码配对 `#pairingDlg`。
- **单一 `SYNC · E2E` 胶囊（替代顶栏 6 个分散按钮）**：
  - 点击弹出轻量悬浮面板（替代分散的 `#modeBtn` / `#themeBtn` / `#fontBtn` / `#settingsBtn`）：
    - 主动复制开关、主动粘贴开关、暂停同步（15m / 1h / 恢复）
    - 外观切换（跟随系统 / 浅色 / 深色）、字体大小
    - 标签筛选入口（当存在标签时）与「更多设置…」入口（打开现有 `#settingsDlg`）
  - **严格保留本地权限边界**：远程手机端（`!S.local`）不展示仅本机可用的同步开关与设置项。

### 4.4 按需右侧抽屉（Inspector Drawer）

- 仅当用户主动点击卡片「详情」/ 双击卡片 / 在搜索结果点击全文命中片段时滑出（`width: min(440px, 92vw)`），按 `Esc` 或点击遮罩关闭。
- 顶部采用清晰的三段切换：`内容` · `历史 (N)` · `备注 (N)`。
- 底部固定精简操作条：左侧放置次要菜单（标签、保留时长、重命名、清空已完成、删除），右侧放置主操作（`编辑` / `保存`、`复制`）。
- 若条目有未送达设备，在抽屉内容区底部展示送达回执卡片与「重试」按钮（复用 `delivery.js`）。

### 4.5 批量选择与清空

- 筛选栏右侧保留低调的「多选」文字按钮（本地与远程均可用）。
- 进入多选态后，单击卡片切换勾选态（不再触发复制），屏幕底部浮现紧凑悬浮操作胶囊：`已选 N 条 · 全选 · 批量删除 · 清空历史 · 完成`，不挤压列表顶部高度。

---

## 5. 与现有 `core/web/` 的模块改造映射

> **强约束（不可破坏）**：
> 1. **零构建原生 JS**：继续由 `index.html` 按顺序通过 `<script>` 加载，不引入 npm / bundler / CDN。
> 2. **保留所有安全与加密层**：`crypto.js`、`crypto-worker.js`、`base.js` 中的签名/加密/SSE/`webcrypto_test.go` 互操作逻辑**一字不改**。
> 3. **保留桌面壳集成**：Windows 定制标题栏（`window-chrome.js` + `#windowControls` + `.native-chrome` 拖拽区）、Wails 回调（`POST /api/desktop/*`）、托盘快捷键与焦点重置逻辑必须完整保留。
> 4. **保留中英文机制**：HTML 与 JS 源码文案以简体中文为源文本，新增或改动的固定文案同步补入 `core/web/i18n.js` 字典；用户内容节点继续标记 `data-raw` 防止被误翻译。

### 5.1 文件级改造清单

| 文件 | 改动等级 | 具体职责与修改要点 |
| --- | --- | --- |
| [index.html](../core/web/index.html) | **中改** | 将三栏 `<main class="workspace inbox-layout">` 重构为单画布结构：极简 `<header class="top">`（设备光束容器 `#beamDevices` + `+` 配对按钮 + `#syncCapsule` + 保留 `#windowControls`）、居中 `#omniBar`、轻量 `#kindFilters`、单列 `#list`、底部悬浮 `#selectBar`。保留 `#pairingDlg`、`#devicesDlg`、`#settingsDlg`、`#previewDlg`、`#askDlg`、`#drop`、`#toast` 等既有弹窗 ID。 |
| [style.css](../core/web/style.css) | **中改** | 更新 `:root` 与暗色变量为第 2 节 Token；加入 `.ambient-bg` 点阵光场、`.glass` 毛玻璃、`.omni-glow`、`.beam-line` 光束动画、`.copied-flash` 脉冲动画；统一卡片与弹窗圆角、边框与 `prefers-reduced-motion`。 |
| [inbox.css](../core/web/inbox.css) | **大改** | 移除黑色顶栏覆盖（`background: #000`）、三栏 Grid、侧边栏与常驻底部 `#readerComposerHost` 样式；改为单画布居中流 + `#previewDlg.drawer` 右侧滑出抽屉样式 + 手机端底部悬浮快发胶囊样式。 |
| [panel.css](../core/web/panel.css) | **轻改** | 同步新的视觉 Token、柔和毛玻璃质感与极简选中态，保留 `380×600` 固定视口、仅列表纵向滚动及底部单行输入行为。 |
| [inbox.js](../core/web/inbox.js) | **大改** | 废弃三栏常驻阅读器自动加载逻辑（`syncInboxReader` 不再强制首项常驻打开）和侧边栏/列表折叠逻辑；改为：1) `renderBeamDevices()` 渲染顶部设备光束；2) `openDrawer(it)` / `closeDrawer()` 控制 `#previewDlg` 滑出抽屉；3) 精简卡片模板 `streamItemHTML(it)`（单击复制、悬停仅收藏+详情、清单卡片内直接可勾选）；4) 保留批量选择 `bindPicking()` 与 `askClearAll()`。 |
| `splitters.js` | **移除或置空**（已删除） | 单画布模式不再需要三栏拖拽分割线；确认 `inbox.js` 不再调用 `bindInboxWidths` / `refreshInboxWidths` 后即可从 `index.html` 移除。 |
| [list.js](../core/web/list.js) | **轻改** | `renderList()` 接入新的卡片渲染函数；保留增量 DOM 复用、分页 `IntersectionObserver`、全文搜索高亮、`findCode()` 验证码提取与清单内联交互；发送成功时记录新条目 ID 以附加 `.card-enter` 入场动画。 |
| [main.js](../core/web/main.js) | **轻改** | 绑定 Omni-Bar 的“输入过滤 + 回车直发 + 智能多行清单识别”；绑定 `#syncCapsule` 悬浮面板开关；在 `handleItemAction` 中实现主画布点击卡片复制 + `.copied-flash` 反馈；保持 `bindPanel()`、`bindWindowChrome()`、全局 `paste` 与 `drop` 完整兼容。 |
| [preview.js](../core/web/preview.js) | **轻改** | 适配右侧抽屉模式（关闭按钮始终可见、`Esc` 关闭抽屉）；确保代码高亮、编辑保存、版本 Diff、评论列表在 `420px` 抽屉宽度内无横向溢出。 |
| [features.js](../core/web/features.js) / [settings.js](../core/web/settings.js) / [delivery.js](../core/web/delivery.js) | **微调** | 适配 `#syncCapsule` 状态文字（如 `SYNC · E2E` / `PAUSED`）；送达回执紧凑渲染为卡片底部的 `● 3/3`（异常时高亮橙/红并在抽屉内显示重试）。 |
| [i18n.js](../core/web/i18n.js) | **微调** | 补充新 UI 文案的英文翻译条目（如 Omni-Bar 占位符、`详情`、`多选` 等）。 |

### 5.2 必须保留的关键 DOM ID 契约

为了避免破坏既有事件绑定与设置回显，重构 `index.html` 时建议保留以下核心 ID（哪怕改变了视觉位置）：

- 连接与配对：`#pair`、`#pairForm`、`#pairInput`、`#pairError`、`#pairingDlg`、`#qr`、`#pairCode`、`#urlSelect`、`#devicesDlg`、`#deviceList`
- 列表与输入：`#list`、`#listMore`、`#listMoreBtn`、`#empty`、`#fileInput`、`#folderInput`、`#uploads`、`#sendStatus`、`#clipCard`、`#clipText`、`#clipSend`
- 批量与预览：`#selectBar`、`#selectCount`、`#selectAll`、`#selectDelete`、`#selectDone`、`#previewDlg`、`#pvMeta`、`#pvTabs`、`#pvBody`、`#pvHistList`、`#pvCom`、`#pvComList`、`#pvComForm`、`#pvComInput`、`#pvFoot`
- 设置与系统：`#settingsDlg` 内部所有 `#opt*` / `#nodeName` / `#block*` / `#manual*` / `#upd*`、`#windowControls`、`#windowMaximise`、`#askDlg`、`#toast`、`#drop`

---

## 6. 分阶段实施路线

### Phase 1：视觉底座与单画布骨架
1. 更新 `style.css` 变量、点阵背景、毛玻璃与动效类。
2. 重构 `index.html` 与 `inbox.css`，落地极简顶栏 + 设备光束 + 单列居中卡片流，移除侧边栏与 `splitters.js`。
3. 校验 Windows 定制标题栏（`?desktop=main&chrome=windows`）在极简顶栏中的拖拽区与最小化/最大化/关闭按钮对齐。

### Phase 2：Omni-Bar 与零步骤交互
1. 在 `main.js` 与 `list.js` 中打通 Omni-Bar 的“输入过滤 / 回车直发 / 附件按钮 / 全局 `⌘V` 与拖放”。
2. 改造卡片渲染，落地“单击即复制 + `.copied-flash` 反馈”、“清单卡片内直接勾选”以及“悬停仅 2 个轻按钮”。
3. 落地 `#syncCapsule` 弹出层，收口同步开关、暂停、主题、字号、标签与设置入口。

### Phase 3：右侧毛玻璃抽屉与三端微调
1. 将 `#previewDlg` 改造为按需滑出的右侧抽屉（宽屏）与底部/全屏 Sheet（窄屏），验证编辑、Diff、评论与送达回执重试。
2. 同步微调 `panel.css`（托盘 380×600）与移动端窄屏样式（底部悬浮快发栏 + Safe Area）。
3. 补全 `i18n.js` 中英文词条，检查亮/暗模式、大字体（`--fs: 1.15`）与无障碍焦点可见性。

---

## 7. 验收与自检清单

每次修改 `core/web/` 后执行以下检查：

```bash
# 1. 所有前端脚本语法检查
for f in core/web/*.js; do node --check "$f"; done

# 2. Go 编译与加密/后端测试（含 Go <-> JS 加解密互通）
go vet ./...
go test -race ./core ./internal/...
```

**人工交互验收点**：
- [ ] **桌面主窗口**：顶栏元素克制，设备节点在线/离线切换与光束暂停状态正确；点击设备节点可筛选。
- [ ] **Omni-Bar**：输入即时筛选、按回车直发、未聚焦时按 `⌘V` 直发、拖入单个文件和文件夹均正常上传。
- [ ] **卡片交互**：单击文本卡片即复制且有视觉反馈；拖选文字不误触发复制；清单项直接在卡片内勾选并跨端同步；点击「详情」滑出抽屉，`Esc` 关闭。
- [ ] **托盘快捷面板**（`?desktop=panel`）：弹出自动聚焦搜索、`↑↓` 选择、`↩` 复制并收起、`Space` 预览、窗口无横向滚动条。
- [ ] **手机扫码端**（非 loopback + 窄屏）：扫码配对流程正常，底部快发栏不被系统手势条遮挡，本机专属设置项已正确隐藏。
- [ ] **亮 / 暗主题与双语**：浅色/深色模式对比度清晰，切换英文后无残留未翻译 UI 文本，用户数据（`data-raw`）不被误翻。
