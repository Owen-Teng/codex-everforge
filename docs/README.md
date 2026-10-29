# Website presentation / 官网展示

[Open the HTTPS preview / 打开官网预览](https://owen-teng.github.io/codex-everforge/)

This directory is a static, interactive presentation candidate for Codex Everforge, published from `main:/docs` by GitHub Pages. Open `index.html` through a static HTTPS host; there is no Node build, database, OAuth backend, analytics or notification service in this directory.

## 可操作内容

- 原创人物正反卡、旋转与拖拽惯性、暂停和减少动态。
- 额度、公开重置消息和社区构想的功能浮层。
- 示例身份切换、加载/空白/失败状态预演；不会收集账户、密码或个人额度。
- 固定版本下载、安装说明及复制给 Codex 的安装指令。
- 窄屏布局与键盘操作；不是手机原生应用。

## 当前展示与桌面功能

网页卡片、额度和身份均为示例；真正的 Windows 功能及限制见[功能状态](../FEATURES.md)。下载链接指向现有 `v0.8.0-beta.1` 安装包。网站上线不会启用云端注册、联网盲比、官方 X 实时流或自动升级。

## 场景差异和许可

该公开候选使用项目自编 CSS 3D 卡片与 SVG 剑/烛光装饰，保留原网站的角色、双面卡、构图和交互。它不是原 ThreeUI WebGL 剑冢的原样导出：该具体示例及模型的再分发许可尚未核实，因此未公开那些资源。原版未被此候选覆盖。视觉取舍仍需产品所有者确认。

原创代码和文档采用 [MIT](LICENSE)；人物和品牌见[素材许可](ASSETS-LICENSE.md)。字体采用 SIL OFL，原文及来源见[第三方声明](THIRD_PARTY_NOTICES.md)和[CREDITS](CREDITS.md)。

## 文件组织

`index.html` 是入口；`app.js` 负责项目自编兼容场景；`account.js` 和 `profiles.js` 仅处理内存中的虚构身份；`feature-previews.js` 负责功能浮层；`install-prompt.js` 与 `download-config.js` 指向真实的固定版本安装说明。字体和自有图片在 `assets/`，所有运行路径均为相对路径。

请不要把用户资料、私有截图、密钥、缓存、日志或原帖抓取内容放进这个公开目录。
