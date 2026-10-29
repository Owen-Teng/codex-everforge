# 功能状态 / Feature status

[中文首页](README.md) · [English](README.en.md) · [Windows安装](INSTALL-WINDOWS.md) · [v0.8.0-beta.1下载](https://github.com/Owen-Teng/codex-everforge/releases/tag/v0.8.0-beta.1)

本页对应Windows 11 x64未签名预发布版。官网是静态交互展示，示例身份、卡片和数值不代表已开放云端服务。

This page describes the unsigned Windows 11 x64 pre-release. Website interactions use example data and do not establish that a cloud feature is available.

| 能力 / Capability | 状态 / Status |
| --- | --- |
| 任务栏额度 / Taskbar quota | 可用，通过本机官方CLI读取 / Available through the local official CLI |
| 桌面浮窗、三主题与原生菜单 / Panel, themes and native menu | 已提供；本机菜单观感获确认 / Available; menu appearance confirmed on one PC |
| 公开重置消息 / Public reset messages | 默认Codex Resets，每5分钟检查；有上游延迟与漏报风险 / Codex Resets, five-minute checks; upstream delays and missed messages are possible |
| 消息提醒 / Notifications | 默认关闭，首次自选开启；已有消息静默 / Off by default, opt in on first use; existing messages form a silent baseline |
| 资料与作品链接 / Profile and work links | 本机保存 / Local only |
| 云账号、OAuth、付费会员 / Cloud accounts, OAuth, paid membership | 未上线 / Not available |
| 在线社区与盲比 / Online community and blind comparison | 未上线；官网相关交互为示例 / Not available; website previews use example data |
| 云端自动升级 / Cloud automatic software updates | 未上线；通过发布安装包升级 / Not available; upgrade using a published installer |

公共消息不是官方X实时流，也不代表个人额度恢复。无需X密钥，公开包不包含AIHOT内部配置。Public messages are not an official real-time X feed or confirmation of personal quota recovery. No X key is needed; internal AIHOT configuration is excluded.

已知本机缺口：未知错误/部分读数可能清比较基线；回升通知尚无独立可靠队列，可能被来源路径覆盖。第二台Windows、实际睡眠恢复、多屏及未来自然新事件仍待验证。Local recovery can lose its comparison baseline after an unknown error or partial reading, and its notification can be overwritten by the source path. A second PC, sleep/resume, multi-monitor behavior and future natural events still need verification.

源码与文档采用[MIT](LICENSE)；[美术与品牌许可](ASSETS-LICENSE.md)另列。Code and documentation use MIT; art and branding have separate terms.
