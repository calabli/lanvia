Lanvia 安装说明
================

双击“安装 Lanvia.pkg”，按提示一步步点“继续”即可。安装器会显示进度，
装好后 Lanvia 会出现在“应用程序”和启动台里，并自动打开。
Lanvia 只在屏幕顶部的菜单栏显示图标，不占程序坞。

这个安装包只适用于文件名里写的芯片（Apple 芯片或 Intel）。
不知道自己的 Mac 是哪种：点屏幕左上角的苹果菜单 → 关于本机，
“芯片”一栏写着 Apple M1/M2/M3/M4… 的是 Apple 芯片，写着 Intel 的是 Intel。
Apple 芯片版装在 Intel 的 Mac 上会提示不能安装；Intel 版在 Apple 芯片的 Mac 上也能
通过 Rosetta 运行，但会慢一些，建议换成 Apple 芯片版。

双击安装器时被 macOS 拦下怎么办
--------------------------------
Lanvia 目前没有经过 Apple 公证，第一次打开安装器时 macOS 会提示
“无法验证开发者”或“Apple 无法检查其是否包含恶意软件”。放行一次即可：

  macOS 15 及以后：
    在提示框里点“完成”，然后打开 系统设置 → 隐私与安全性，
    往下找到“已阻止打开‘安装 Lanvia.pkg’”，点“仍要打开”，再输入密码确认。

  macOS 13、14：
    按住 Control 键点“安装 Lanvia.pkg”（或右键）→ 打开 → 在提示框里再点“打开”。

装好之后打开 Lanvia 不会再有这个提示，以后自动更新也不会。
第一次同步时，macOS 可能会询问“是否允许 Lanvia 查找本地网络上的设备”，请点“允许”。


Installing Lanvia
=================

Double-click "安装 Lanvia.pkg" (Install Lanvia) and follow the steps. The installer shows its
progress, puts Lanvia in Applications and Launchpad, and opens it. Look for its icon in the menu bar.

This package is only for the chip named in the file name (Apple silicon or Intel). To check yours,
choose Apple menu → About This Mac. The Apple silicon package won't install on an Intel Mac;
the Intel package runs on Apple silicon through Rosetta, but more slowly.

If macOS blocks the installer (it isn't notarized yet), allow it once:
  macOS 15 and later: click Done, open System Settings → Privacy & Security, find the blocked
    installer, click "Open Anyway" and confirm with your password.
  macOS 13 and 14: Control-click the installer → Open → Open.

After installing, Lanvia opens without this prompt, including after automatic updates.
When syncing for the first time, allow Lanvia to find devices on your local network.
