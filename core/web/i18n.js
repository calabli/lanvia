'use strict';

/* 界面中英文。中文是源文本；切换到英文时，按词典翻译页面上的文字节点和 placeholder / title / aria-label，
   新插入的内容由 MutationObserver 自动翻译。带变量的文本用 {0} {1} 占位，捕获的部分会再翻译一次。
   用户内容（剪贴板文本、收藏名称、文件名、设备名等）所在元素带 data-raw，不会被翻译。 */

const I18N = (() => {
  const EN = {
    // ---- 配对页 ----
    '让设备，': 'Your devices, ', '默契相连。': 'in sync.',
    '输入电脑上显示的配对码，': 'Enter the pairing code shown on your computer ',
    '让文字与文件在你的设备间自由流转。': 'to move text and files freely between your devices.',
    '连接空间': 'Connect', '同一网络，即刻连接 · 无需注册': 'Same network, instant connection · No sign-up',
    '配对码不正确，或无法连接到这台电脑': "Wrong pairing code, or this computer can't be reached",
    '配对已失效，请重新输入配对码': 'Pairing expired. Please enter the pairing code again.',
    // ---- 顶栏与首页 ----
    '你的设备，流畅相连': 'Your devices, seamlessly connected', '连接中': 'Connecting',
    '随手复制，': 'Copy here, ', '随处接续。': 'continue anywhere.',
    '文字、链接与文件，在你的设备之间轻盈流转。': 'Text, links and files, flowing lightly between your devices.',
    '正在连接你的空间': 'Connecting to your space', '你的空间，已连接': 'Your space is connected', '正在重新连接': 'Reconnecting',
    '下一台设备，扫码即连。': 'Scan to connect your next device.', '打开手机相机，扫描二维码': 'Open your phone camera and scan the QR code',
    '配对码': 'Pairing code', '连接地址': 'Address', '仅与信任的设备分享配对码。': 'Only share the pairing code with devices you trust.',
    '设备之间，保持默契。': 'Keep your devices in step.', '查看在线设备': 'See online devices',
    '局域网直连': 'Direct over LAN', '无需云端中转': 'No cloud relay',
    '让工作流动起来。': 'Keep your work flowing.', '从一次简单的复制开始。': 'It starts with a simple copy.',
    '让设备之间，少一点距离。': 'Bringing your devices a little closer.',
    '{0} 台设备': '{0} devices', '仅本机': 'Only this device', '重新连接中': 'Reconnecting',
    // ---- 输入区 ----
    '分享一点灵感': 'Share something', '随时，随手。': 'Anytime, anywhere.',
    '粘贴': 'Paste', '添加文件': 'Add files', '添加文件夹': 'Add folder', '发送': 'Send',
    '粘贴文字、链接，或拖入文件…': 'Paste text or links, or drop files…', '要分享的内容': 'Content to share',
    'Enter 发送 · Shift+Enter 换行': 'Enter to send · Shift+Enter for a new line',
    '长按此处粘贴，或输入要分享的内容…': 'Long-press to paste, or type something to share…',
    '无法读取剪贴板': "Can't read the clipboard", '松开即可分享文件': 'Drop to share files',
    '正在打包…': 'Packing…', '正在打包 {0}%': 'Packing {0}%', '打包失败': 'Packing failed', '读取文件夹失败': "Couldn't read the folder",
    '{0}/ · {1} 个文件': '{0}/ · {1} files', '文件夹': 'Folder',
    '上传失败，请检查网络': 'Upload failed. Check your network.', '上传失败：HTTP {0}': 'Upload failed: HTTP {0}',
    '发送失败：{0}': 'Send failed: {0}',
    // ---- 列表 ----
    '最近分享': 'Recent', '全部': 'All', '文本与链接': 'Text & links', '文件': 'Files', '收藏': 'Favorites',
    '所有设备': 'All devices', '全部时间': 'Any time', '今天': 'Today', '最近 7 天': 'Last 7 days', '最近 30 天': 'Last 30 days',
    '每一份灵感，都有迹可循。': 'Every idea leaves a trace.', '没有完全匹配的内容，以下是相近的结果': 'No exact matches. Showing similar results.',
    '好想法，从这里开始。': 'Great ideas start here.', '还没有找到这份灵感。': 'Nothing found.',
    '在已连接的设备上复制文字，或发送你的第一条内容。': 'Copy text on a connected device, or send your first item.',
    '试试其他关键词，或切换内容类型。': 'Try other keywords or another content type.',
    '搜索内容': 'Search', '搜索历史': 'Search history', '内容类型': 'Content type', '按设备筛选': 'Filter by device', '按时间筛选': 'Filter by time',
    '搜索，↑↓ 选择': 'Search, ↑↓ to select',
    '最新': 'Latest', '已编辑': 'Edited', '已收藏': 'Favorite', '复制': 'Copy', '下载': 'Download', '预览': 'Preview', '编辑': 'Edit',
    '删除': 'Delete', '保留时长': 'Retention', '打开链接': 'Open link', '复制验证码 {0}': 'Copy code {0}',
    '展开全部': 'Show all', '收起': 'Collapse', '{0} 字': '{0} chars', '{0} 行 · {1} 字': '{0} lines · {1} chars',
    '刚刚': 'just now', '{0} 分钟前': '{0} min ago', '昨天 {0}': 'Yesterday {0}', '{0}月{1}日 {2}': '{0}/{1} {2}', '· {0}': '· {0}',
    '{0} 修改': 'edited by {0}', '{0} 修改于 {1}': 'edited by {0}, {1}',
    '已复制': 'Copied', '复制失败': 'Copy failed', '复制失败，请长按文字手动复制': 'Copy failed. Long-press the text to copy it manually.',
    '已保存到 下载/Lanvia': 'Saved to Downloads/Lanvia', '显示': 'Show', '保存失败：{0}': 'Save failed: {0}', '删除失败：{0}': 'Delete failed: {0}',
    '来自 {0}：已写入剪贴板': 'From {0}: copied to your clipboard', '{0} 发来文件 {1}': '{0} sent a file: {1}',
    '已发送': 'Sent', '已发送到所有设备': 'Sent to all devices',
    // ---- 设备 ----
    '在线设备': 'Online devices', '本机': 'This device', '暂无': 'None', '这台设备的名称': 'Name of this device', '退出配对': 'Unpair',
    '运行 Lanvia': 'Runs Lanvia', '运行 Lanvia · 已加密': 'Runs Lanvia · Encrypted', '运行 Lanvia · 未加密（旧版本）': 'Runs Lanvia · Not encrypted (older version)',
    '你正通过它连接 · 运行 Lanvia': "You're connected through it · Runs Lanvia", '浏览器 · 经由 {0}': 'Browser · via {0}',
    '安卓手机': 'Android phone', 'Windows 电脑': 'Windows PC', 'Linux 电脑': 'Linux PC', '浏览器': 'Browser',
    '退出后需要重新输入配对码。确定吗？': "You'll need to enter the pairing code again. Continue?",
    // ---- 外观 ----
    '跟随系统': 'System', '浅色': 'Light', '深色': 'Dark', '切换外观': 'Switch appearance',
    '外观：{0}，切换为{1}': 'Appearance: {0}. Switch to {1}', '外观：{0}，点击切换': 'Appearance: {0}. Click to switch',
    // ---- 发送模式 ----
    '切换发送模式': 'Switch send mode', '主动': 'Active', '被动': 'Passive', '复制即发送': 'Copy to send', '手动发送': 'Send manually',
    '发送模式': 'Send mode', '被动模式 · 本机剪贴板': 'Passive mode · This clipboard', '发送剪贴板': 'Send clipboard',
    '在这台电脑上复制文字，会自动发送到所有设备。': 'Text you copy on this computer is sent to all devices automatically.',
    '复制不会自动发送。需要时点“发送剪贴板”，或在托盘菜单里选“发送当前剪贴板”（桌面版）。':
      "Copying doesn't send anything. Use “Send clipboard”, or “Send current clipboard” in the tray menu (desktop app).",
    '主动模式：复制即发送。点击切换为被动模式': 'Active mode: copying sends. Click for passive mode',
    '被动模式：手动发送。点击切换为主动模式': 'Passive mode: send manually. Click for active mode',
    '也可以在托盘菜单里选“发送当前剪贴板”': 'Or choose “Send current clipboard” in the tray menu',
    '已切换为主动模式：复制即发送': 'Switched to active mode: copying sends', '已切换为被动模式：需要手动发送': 'Switched to passive mode: send manually',
    '这台电脑不支持读取剪贴板': "This computer can't read the clipboard", '剪贴板里是文件或敏感内容，不会发送': "The clipboard holds a file or sensitive content and won't be sent",
    '读取剪贴板失败': "Couldn't read the clipboard", '剪贴板里没有文字': 'No text on the clipboard', '内容太长': 'Too long',
    // ---- 设置 ----
    '设置': 'Settings', '本机名称': 'Computer name', '语言': 'Language',
    '自动写入本机剪贴板': 'Write to this clipboard automatically', '其他设备发来的文字直接可以 ⌘V / Ctrl+V 粘贴': 'Text from other devices is ready to paste with ⌘V / Ctrl+V',
    '未找到系统剪贴板工具，只能通过网页手动复制。': 'No system clipboard tool found. Copy manually from the page.',
    '历史保留时间': 'Keep history for', '1 天': '1 day', '7 天': '7 days', '30 天': '30 days', '永久': 'Forever', '{0} 天': '{0} days',
    '到期的内容、文件和历史版本会从这台电脑上删除，也不会再从其他设备同步回来。只对这台电脑生效。':
      'Expired items, files and versions are deleted from this computer and won’t sync back from other devices. Applies to this computer only.',
    '将立即删除这台电脑上 {0} 条超过 {1} 天的内容，确定吗？': '{0} items older than {1} days will be deleted from this computer now. Continue?',
    '自动下载其他设备发来的文件': 'Auto-download files from other devices', '关闭（需要时再从对方获取）': 'Off (fetch when needed)',
    '10 MB 以下': 'Up to 10 MB', '100 MB 以下': 'Up to 100 MB', '1 GB 以下': 'Up to 1 GB', '{0} MB 以下': 'Up to {0} MB',
    '在本机留一份副本，发送方关机后仍能打开。删除或过期时副本一起清理。': 'Keeps a copy here so files open even when the sender is off. Copies are removed with their items.',
    '文件占用上限': 'Storage limit', '不限': 'Unlimited', '清理全部副本': 'Clean up all copies',
    '超出上限时只清理其他设备文件的副本（从最旧的开始），本机发出的原件不会删除；条目都会保留，需要时再从其他设备获取。':
      'Over the limit, only copies of other devices’ files are removed (oldest first). Originals sent from here are never deleted, and items stay — files are fetched again when needed.',
    '当前文件占用 {0}，超过新上限，将清理较旧的副本。确定吗？': 'Files currently use {0}, over the new limit. Older copies will be cleaned up. Continue?',
    '没有副本需要清理': 'No copies to clean up', '删除 {0} 个副本，释放 {1}？条目会保留，需要时再从其他设备获取。': 'Delete {0} copies and free {1}? Items stay, and files are fetched from other devices when needed.',
    '清理': 'Clean up', '已清理 {0} 个副本，释放 {1}': 'Cleaned up {0} copies, freed {1}',
    '已用 {0} / {1} · 原件 {2} · 副本 {3}（{4} 个）': 'Used {0} / {1} · Originals {2} · Copies {3} ({4})',
    '已用 {0} · 原件 {1} · 副本 {2}（{3} 个）': 'Used {0} · Originals {1} · Copies {2} ({3})',
    '加入另一台电脑': 'Join another computer', '对方的配对码': "The other computer's pairing code", '加入': 'Join',
    '两台电脑使用同一个配对码后会自动同步。加入后，本机原来的配对码失效。': "Computers with the same pairing code sync automatically. After joining, this computer's old code stops working.",
    '在同一网络，但配对码不同': 'is on this network with a different pairing code',
    '配对码应为 10 位': 'The pairing code has 10 characters', '已加入，正在寻找其他设备…': 'Joined. Looking for other devices…',
    '加密': 'Encryption',
    '电脑之间、手机浏览器与电脑之间的内容、历史和文件都用由配对码派生的密钥加密（AES-256-GCM），每个请求只能使用一次，截获后无法重放。超过 256 MB 的文件在手机上明文传输，以免占用同样大的内存。':
      'Content, history and files are encrypted between computers and between phone browsers and computers, with a key derived from the pairing code (AES-256-GCM). Each request can be used only once, so captured requests can\'t be replayed. On phones, files over 256 MB are sent unencrypted to avoid using as much memory.',
    '只允许加密连接': 'Encrypted connections only',
    '拒绝不支持加密的旧版本设备（v0.4 及更早），也防止同一网络里有人截获并去掉加密；手机上的大文件也加密传输': 'Refuse older devices without encryption (v0.4 and earlier), stop anyone on the network from stripping encryption, and encrypt large files on phones too',
    '允许用 token 访问': 'Allow token access',
    '给 iOS 快捷指令、安卓 HTTP Shortcuts、脚本使用。token 和通过它收发的内容都是明文传输，不用时建议关闭':
      "For iOS Shortcuts, HTTP Shortcuts on Android and scripts. The token and everything sent with it travel unencrypted, so turn this off if you don't use it",
    '重新生成 token': 'Regenerate token',
    '怀疑 token 泄露时使用，旧 token 立即失效，需要更新快捷指令和脚本。': 'Use this if the token may have leaked. The old token stops working right away, so update your shortcuts and scripts.',
    '重新生成后，旧 token 立即失效，使用它的快捷指令和脚本需要更新。确定吗？': 'The old token will stop working right away, and shortcuts and scripts using it need updating. Continue?',
    '已重新生成 token': 'Token regenerated', '操作失败：{0}': 'Failed: {0}',
    '正在加载…': 'Loading…', '正在加载 {0}%': 'Loading {0}%', '正在下载 {0}%': 'Downloading {0}%', '正在加密 {0}%': 'Encrypting {0}%',
    '下载失败：{0}': 'Download failed: {0}',
    '新建清单': 'New list', '清单': 'Lists', '添加一项…': 'Add an item…', '添加一项': 'Add an item', '清除已完成': 'Clear completed',
    '修改这一项': 'Edit item', '删除这一项': 'Delete item', '重命名': 'Rename', '清单名称': 'List name', '例如：周末购物': 'e.g. Weekend shopping',
    '创建': 'Create', '创建失败：{0}': "Couldn't create: {0}",
    '新建清单：输入框里有多行文字时，第一行作为标题、其余每行一项': 'New list: with several lines in the box, the first line becomes the title and each other line an item',
    '清单不存在': "The list doesn't exist", '这一项不存在': "The item doesn't exist", '清单最多 300 项': 'A list can have at most 300 items', '清单不能为空': "A list can't be empty",
    '评论': 'Comments', '{0} 条评论': '{0} comments', '添加评论…': 'Add a comment…',
    '还没有评论。评论会同步到所有设备。': 'No comments yet. Comments sync to all devices.',
    '删除评论': 'Delete comment', '删除这条评论？所有设备上都会删除。': 'Delete this comment? It will be deleted on all devices.',
    '评论失败：{0}': "Couldn't add the comment: {0}", '读取文件失败': "Couldn't read the file", '播放': 'Play',
    '{0} 是旧版本，不支持加密。开启后将断开与它们的同步，确定吗？': '{0} run an older version without encryption. Turning this on stops syncing with them. Continue?',
    '开启': 'Turn on',
    '手动添加设备': 'Add device manually',
    '配对码相同却一直找不到对方时使用，例如路由器屏蔽了广播、两台电脑不在同一网段。只需在一台电脑上添加，对方的地址可以在它的主窗口里看到。':
      "Use this when computers with the same pairing code can't find each other, e.g. the router blocks broadcast or they're on different subnets. Add it on one computer only; the address is shown in the other computer's main window.",
    '例如 192.168.1.20 或 192.168.1.20:8686': 'e.g. 192.168.1.20 or 192.168.1.20:8686', '添加': 'Add', '移除': 'Remove',
    '正在连接…': 'Connecting…', '已连接：{0}': 'Connected: {0}', '已添加，正在连接…': 'Added, connecting…',
    '已连接 · {0}': 'Connected · {0}', '未连接，会自动重试': 'Not connected, retrying automatically',
    '自动化': 'Automation', 'iOS 快捷指令、安卓 HTTP Shortcuts、脚本可用这个令牌调用接口：': 'iOS Shortcuts, HTTP Shortcuts on Android and scripts can call the API with this token:',
    '危险操作': 'Danger zone', '重新生成配对码': 'Generate new pairing code', '清空所有设备的历史': 'Clear history on all devices',
    '所有已配对的手机和电脑都需要重新配对。确定重新生成吗？': 'All paired phones and computers will need to pair again. Generate a new code?',
    '重新生成': 'Regenerate', '已生成新的配对码': 'New pairing code generated',
    '将清空所有已连接设备上的历史记录和文件。确定吗？': 'This clears history and files on all connected devices. Continue?', '清空': 'Clear',
    '数据目录：{0}': 'Data folder: {0}', '未检测到局域网地址': 'No LAN address found',
    '连接与配对': 'Connection & pairing', '收起配对信息': 'Collapse pairing info', '配对二维码': 'Pairing QR code', '局域网地址': 'LAN address',
    '查看所有在线设备': 'See all online devices',
    // ---- 收藏 ----
    '给这条收藏起个名字，方便搜索（例如“家里 WiFi”）': 'Name this favorite to find it easily (e.g. “Home Wi-Fi”)',
    '名称（可留空）': 'Name (optional)', '已收藏：不会过期，在所有设备上都能找到': 'Favorited: never expires and is on all your devices',
    '命名': 'Name', '命名…': 'Name…', '重命名…': 'Rename…', '取消收藏': 'Unfavorite', '已取消收藏': 'Removed from favorites', '收藏 · {0}': 'Favorite · {0}',
    // ---- 保留时长 ----
    '1 小时后删除': 'Delete in 1 hour', '1 天后删除': 'Delete in 1 day', '7 天后删除': 'Delete in 7 days', '30 天后删除': 'Delete in 30 days',
    '永久保留': 'Keep forever', '跟随全局设置': 'Follow global setting', '· 同步到所有设备': '· synced to all devices', '当前：{0}': 'Current: {0}',
    '{0} 分钟后删除': '{0} min left', '{0} 小时后删除': '{0} h left', '{0} 天后删除': '{0} d left', '{0} 删除': 'Deletes {0}',
    '不受全局保留时间影响': 'Not affected by global retention',
    '已设为永久保留': 'Set to keep forever', '已设置：{0}': 'Set: {0}', '已恢复跟随全局设置': 'Now follows the global setting', '设置失败：{0}': 'Failed: {0}',
    // ---- 预览、编辑、历史 ----
    '自动换行': 'Wrap lines', '等宽字体与行号': 'Monospace & line numbers', '历史版本': 'Version history', '悬浮在最上层': 'Keep on top',
    '格式化': 'Format', '原文': 'Original', '看全文': 'Full text', '看改动': 'Changes', '恢复此版本': 'Restore this version',
    '复制全部': 'Copy all', '复制此版本': 'Copy this version', '保存': 'Save', '取消': 'Cancel', '关闭': 'Close', '返回': 'Back',
    '复制全部并关闭': 'Copy all and close', '选中文字后': 'With text selected,', '只复制选中部分': 'copies just the selection',
    '保存为新版本': 'Save as new version', '保存后会同步到所有设备，旧内容保留在历史版本里': 'Saving syncs to all devices; old content stays in version history',
    '切换版本': 'Switch version', '这条内容已被删除': 'This item was deleted', '编辑内容': 'Edit content',
    '放弃这次修改吗？': 'Discard your changes?', '放弃修改': 'Discard',
    '已保存 · 第 {0} 版': 'Saved · version {0}', '读取历史失败：{0}': "Couldn't load history: {0}",
    '原始内容': 'Original', '第 {0} 版': 'Version {0}', '当前': 'Current', '行': 'lines',
    '内容太长，无法逐行对比，显示全文。': 'Too long to compare line by line; showing full text.', '这是原始内容。': 'This is the original.',
    '与上一版内容相同。': 'Same as the previous version.', '⋯ {0} 行未改动 ⋯': '⋯ {0} unchanged lines ⋯',
    '已恢复为新版本': 'Restored as a new version', '恢复失败：{0}': 'Restore failed: {0}', '已复制选中': 'Copied selection',
    '{0} 刚刚修改了这条内容。保存后你的版本会成为最新版本，对方的修改会保留在历史版本中。':
      '{0} just changed this item. If you save, your version becomes the latest and theirs stays in version history.',
    '其他设备': 'Another device',
    // ---- 快捷面板 ----
    '选择': 'select', '空格': 'Space',
    // ---- 对话框 ----
    '确定': 'OK',
    // ---- 服务端返回的提示 ----
    '这台电脑只允许加密连接，请把对方升级到最新版本': 'This computer only allows encrypted connections. Update the other device to the latest version.',
    '评论不能为空': "A comment can't be empty", '评论过长': 'The comment is too long', '评论不存在': "The comment doesn't exist",
    '重复的请求（可能被截获后重放），已拒绝': 'Duplicate request rejected (it may have been captured and replayed)',
    '这台电脑已关闭 token 访问': 'Token access is turned off on this computer',
    '文件占用上限无效': 'Invalid storage limit', '自动下载上限无效': 'Invalid auto-download limit', '保留天数应在 0 到 3650 之间': 'Retention must be between 0 and 3650 days',
    '对方的配对码和本机不同（未保存）。请先在“加入另一台电脑”里统一配对码': 'The other computer uses a different pairing code (not saved). Use “Join another computer” to share one code first.',
    '暂时连不上（地址已保存，会自动重试）。请确认对方的 Lanvia 正在运行、IP 和端口正确，以及防火墙允许连接':
      "Can't connect yet (address saved, retrying automatically). Check that Lanvia is running there, the IP and port are right, and the firewall allows it.",
    '对方的配对码和本机不同': 'The other computer uses a different pairing code', '这是本机的地址': "That's this computer's address",
    '对方未加密（旧版本），而本机设置为只允许加密连接': "The other computer doesn't encrypt (older version), and this computer only allows encrypted connections",
    '请填写对方的 IP 地址': "Enter the other computer's IP address", '地址格式不对，例如 192.168.1.20 或 192.168.1.20:8686': 'Invalid address, e.g. 192.168.1.20 or 192.168.1.20:8686',
    '内容不能为空': "Content can't be empty", '内容过长': 'Content is too long', '内容不存在或不能编辑': "Item doesn't exist or can't be edited",
    '内容不存在': "Item doesn't exist", '设置失败，请重试': 'Failed, please try again', '只能复制文本': 'Only text can be copied',
    '文件不存在': "File doesn't exist", '来源设备不在线': 'The source device is offline',
    '发送方和其他设备都不在线，或都没有这个文件': "Neither the sender nor any other device is online with this file",
    '快捷指令': 'Shortcuts',
    '正在加载全文…': 'Loading the full text…', '全文已加载（{0} 字），点此复制': 'Full text loaded ({0} chars). Tap to copy',
    '加载失败：{0}': 'Loading failed: {0}', '显示更多（还有 {0} 条）': 'Show more ({0} left)', '在全文第 {0} 行找到：': 'Found on line {0} of the full text:', '{0}+ 行 · {1} 字': '{0}+ lines · {1} chars',
    // ---- 暂停、黑名单、通知 ----
    '暂停同步': 'Pause syncing', '15 分钟': '15 minutes', '1 小时': '1 hour', '直到手动恢复': 'Until I resume',
    '暂停期间本机复制的不发送，收到的不写入剪贴板（仍会出现在列表里）。': "While paused, copies here aren't sent and incoming items aren't written to the clipboard (they still appear in the list).",
    '已暂停到 {0}': 'Paused until {0}', '已暂停，直到手动恢复': 'Paused until you resume', '恢复同步': 'Resume syncing',
    '已暂停': 'Paused', '· 到 {0}': '· until {0}', '同步已暂停，点击恢复': 'Syncing is paused. Click to resume',
    '已恢复同步': 'Syncing resumed', '已暂停同步': 'Syncing paused',
    '不同步这些应用里复制的内容': "Don't sync content copied in these apps",
    '应用名或 ID，例如 1Password、com.apple.keychainaccess、chrome.exe': 'App name or ID, e.g. 1Password, com.apple.keychainaccess, chrome.exe',
    '还没有添加': 'None yet', '最近复制来源：': 'Recently copied from:',
    '收到内容时弹出通知': 'Show a notification for new items', '仅桌面版；有 Lanvia 窗口在前台时不弹': 'Desktop app only; not shown while a Lanvia window is in front',
  };

  // 带占位符的条目编译成正则
  const PATS = Object.entries(EN).filter(([k]) => /\{\d\}/.test(k)).map(([k, v]) => {
    const re = new RegExp('^' + k.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{(\d)\}/g, '(.+?)') + '$', 's');
    return [re, v, k.replace(/\{\d\}/g, '').length];
  }).sort((a, b) => b[2] - a[2]); // 固定文字越多越具体，先匹配
  const CJK = /[㐀-鿿＀-￯]/;

  let lang = 'zh';

  function tr(s) {
    if (lang === 'zh' || !s || !CJK.test(s)) return s;
    const key = s.replace(/\s+/g, ' ').trim();
    let out = EN[key];
    if (out === undefined) {
      for (const [re, v] of PATS) {
        const m = re.exec(key);
        if (m) { out = v.replace(/\{(\d)\}/g, (_, i) => tr(m[+i + 1])); break; }
      }
    }
    if (out === undefined) return s;
    // 保留原文前后的空白
    const lead = s.match(/^\s*/)[0], tail = s.match(/\s*$/)[0];
    return lead + out + tail;
  }

  const ATTRS = ['placeholder', 'title', 'aria-label'];
  const origText = new WeakMap(), wroteText = new WeakMap();
  const origAttr = new WeakMap(), wroteAttr = new WeakMap();
  const skip = el => el?.closest?.('[data-raw], script, style, textarea, code, pre.mono');

  function doText(n) {
    if (skip(n.parentElement)) return;
    const cur = n.nodeValue;
    if (wroteText.get(n) !== cur) origText.set(n, cur); // 应用自己写入的新文本
    const next = tr(origText.get(n));
    wroteText.set(n, next);
    if (next !== cur) n.nodeValue = next;
  }

  function doAttrs(el) {
    if (el?.closest?.('[data-raw]')) return; // textarea 的内容不译，但它的 placeholder 要译
    let o = origAttr.get(el), w = wroteAttr.get(el);
    if (!o) { o = {}; w = {}; origAttr.set(el, o); wroteAttr.set(el, w); }
    for (const a of ATTRS) {
      const cur = el.getAttribute(a);
      if (cur == null) continue;
      if (w[a] !== cur) o[a] = cur;
      const next = tr(o[a]);
      w[a] = next;
      if (next !== cur) el.setAttribute(a, next);
    }
  }

  function walk(root) {
    if (root.nodeType === 3) { doText(root); return; }
    if (root.nodeType !== 1) return;
    doAttrs(root);
    if (skip(root)) return;
    const tw = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
    while (tw.nextNode()) {
      const n = tw.currentNode;
      if (n.nodeType === 3) doText(n); else doAttrs(n);
    }
  }

  const obs = new MutationObserver(list => {
    for (const m of list) {
      if (m.type === 'childList') m.addedNodes.forEach(walk);
      else if (m.type === 'characterData') doText(m.target);
      else if (m.type === 'attributes') doAttrs(m.target);
    }
  });

  function setLang(l) {
    lang = l === 'en' ? 'en' : 'zh';
    document.documentElement.lang = lang === 'en' ? 'en' : 'zh-CN';
    walk(document.body);
  }

  function start(l) {
    obs.observe(document.body, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ATTRS });
    setLang(l);
  }

  // 首选语言：手动设置 > 系统语言
  const auto = () => ((navigator.language || '').toLowerCase().startsWith('zh') ? 'zh' : 'en');

  return { start, setLang, t: tr, auto, lang: () => lang };
})();
