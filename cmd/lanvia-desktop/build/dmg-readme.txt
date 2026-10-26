Lanvia 安装说明
================

1. 把左边的 Lanvia 拖到右边的“应用程序”文件夹。
2. 在“应用程序”里双击打开 Lanvia。Lanvia 只在屏幕顶部的菜单栏显示图标，不占程序坞。

第一次打开时被 macOS 拦下怎么办
--------------------------------
Lanvia 目前没有经过 Apple 公证，第一次打开时 macOS 会提示“无法验证开发者”或“Apple 无法验证其是否包含恶意软件”。这是正常的，只需要放行一次：

  macOS 15 及以后：
    在提示框里点“完成”，然后打开 系统设置 → 隐私与安全性，
    往下找到“已阻止打开 Lanvia”，点“仍要打开”，再输入密码确认。

  macOS 13、14：
    在“应用程序”里按住 Control 键点 Lanvia（或右键）→ 打开 → 在提示框里再点“打开”。

如果提示“Lanvia 已损坏，无法打开”：
  打开“终端”，粘贴下面这行并回车（会要求输入开机密码，输入时不显示）：

    sudo xattr -dr com.apple.quarantine /Applications/Lanvia.app

  然后再打开 Lanvia。

放行一次以后，之后打开（包括自动更新到新版本后）都不会再提示。
第一次同步时，macOS 还可能询问“是否允许 Lanvia 查找本地网络上的设备”，请点“允许”。


Installing Lanvia
=================

1. Drag Lanvia onto the Applications folder.
2. Open Lanvia from Applications. It lives in the menu bar at the top of the screen, not in the Dock.

If macOS blocks it the first time
---------------------------------
Lanvia isn't notarized by Apple yet, so the first launch shows "developer cannot be verified". Allow it once:

  macOS 15 and later: click Done, open System Settings → Privacy & Security,
    find "Lanvia was blocked", click "Open Anyway" and confirm with your password.
  macOS 13 and 14: Control-click Lanvia in Applications → Open → Open.

If it says "Lanvia is damaged and can't be opened", run this in Terminal, then open it again:

    sudo xattr -dr com.apple.quarantine /Applications/Lanvia.app

When syncing for the first time, allow Lanvia to find devices on your local network.
