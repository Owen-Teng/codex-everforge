# Windows安装

从 [GitHub Releases](https://github.com/Owen-Teng/codex-everforge/releases) 下载 Windows 11 x64 预发布 ZIP 与 SHA256SUMS.txt，核对哈希后解压，运行 INSTALL.cmd。安装器先检查环境与文件；升级会备份并保留原有配置。

首次使用需本人完成官方Codex登录；消息提醒默认关闭，请在设置中自选是否开启，也可选择随Codex启动。不需要购买X API或输入X密钥。源码构建环境不等于安装环境。

可直接发送给自己的Codex：

> 请从 https://github.com/Owen-Teng/codex-everforge/releases 选择最新的非草稿Windows x64体验版，读取该版本说明，下载ZIP及同版本SHA256SUMS.txt，核对文件名、大小与SHA256后解压。先运行包内 scripts/install-n54.ps1 -Action Check；通过后安装并保留原设置。若已装同一版本且文件完全匹配，只检查运行，不重复安装。只配置本工具，需要我本人登录时再提示，不读取或上传认证内容。完成后报告版本、桌面入口、真实额度状态、消息来源与尚需本人处理的项目。

Codex Resets公开监控记录按5分钟间隔检查，但上游可能延迟或漏报；这不是官方X实时消息或个人额度到账。首次开启提醒时，已有记录作为静默基线。会员注册、服务器社区和后台自动更新尚未上线。

安装结果自动保存至 LOCALAPPDATA/CodexEverforge/install-reports；需要恢复时按该报告的备份路径使用包内Rollback操作。退出工具后可删除安装目录；个人配置默认保留，不要删除Codex自身的数据目录。
