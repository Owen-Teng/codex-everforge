# Codex Everforge · Windows

额度抬眼可见，重置消息轻声提醒。

本包是 Windows 11 x64 验收候选。包含主程序、启动组件、额度核心、WebView2 应用库、完整页面和四角色图片，不需要安装 Go 或 Node.js，也不需要提交 API Key。与 OpenAI 无隶属关系。

## 安装

完整解压后双击 `INSTALL.cmd`，或让本机 Codex 执行 `scripts/install-n54.ps1 -Action Install`。脚本先逐文件校验，安装到当前用户的 `%LOCALAPPDATA%\Programs\CodexQuotaMonitor`，不会更改 Codex 自身文件。不要把解压目录放在已安装目录内部。

新装会调用应用配置：随 Codex 启动、尝试右侧显示和检查真实额度。覆盖升级保留已有设置、角色和资料。桌面会生成本版品牌入口，先备份再删除该产品旧版入口；官方 Codex 图标保留。

真实额度需要本机可用的官方 Codex CLI 及 ChatGPT 登录。只有桌面应用不一定满足条件。缺少时按官方说明安装/登录：https://developers.openai.com/codex/cli 。不要向应用开发者、聊天或他人发送认证文件或密码。

牌桌需要 Microsoft Edge WebView2 Evergreen Runtime；Windows 11 通常已有，脚本会检查，缺少时从微软官方入口安装：https://developer.microsoft.com/microsoft-edge/webview2/ 。本包未包含完整 WebView2 Runtime。

PowerShell 若受组织策略限制，请遵循本机策略，由管理员允许经过校验的安装脚本。不要关闭安全软件、证书校验或系统执行策略。本候选尚未代码签名，签名与正式发布需另行完成。

## 结果

- `installed_and_configured`：新装与应用内配置通过；仍需在本机确认实际图标、额度和开合体验。
- `updated_preserving_configuration`：升级文件校验通过，保留原设置。它不等于云服务或未来消息已经送达。
- `installed_needs_attention`：文件已安装，但有配置项目未通过。打开程序的 `--setup` 查看具体缺项；官方登录需要本人完成。
- `needs_webview2` / `unsupported_environment` / `failed`：未完成安装，按结果处理；不要把它报告为成功。

`-Action Check` 只做包校验和环境检查。`-ReportPath <绝对路径>` 可另存机器可读结果。

双击安装入口或不指定报告路径运行安装/回退时，结果会自动保存到 `%LOCALAPPDATA%\CodexEverforge\install-reports`。关闭窗口后仍可找到本次状态及备份路径；控制台也会显示 `reportPath`。若保存失败，会明确显示 `reportWarning`，请先保留控制台结果。只做 Check 且未指定报告路径时不创建报告。

## 更新与恢复

获取新版完整包后执行同一个安装入口。新包先验证，旧文件自动备份到 `%LOCALAPPDATA%\CodexEverforge\backups`；不覆盖 `%APPDATA%\CodexQuotaMonitor` 的个人设置和资料。不在安装中强制结束 Explorer 或其他软件。

文件写入失败会尝试恢复已替换的应用文件；若文件仍被占用，可能只完成部分恢复。先到上述 `install-reports` 找到本次报告，再查看其中 `backup` 目录内的 `rollback-result.json`：`restored` 表示文件恢复完成，`partial_rollback` 或 `restartAllowed:false` 表示仍需处理，请先不要启动工具。正常退出占用本工具文件的程序后，可让 Codex 按本次报告继续恢复；不要强制结束 Explorer。

需要手动回退时，将报告中的真实 `backup` 路径传入 `scripts/install-n54.ps1 -Action Rollback -BackupDirectory <绝对备份路径>`，再核对文件和入口。配置或快捷方式异常会报告 `installed_needs_attention`，应按结果处理，不能当作整个安装已自动回退。备份保留供核验，不自动清理用户资料。正式公网自动更新尚待服务器部署；当前是一键运行新版包升级，不是后台自动更新。

## 使用范围

首页只聚焦额度。消息区区分预告、作者声明、外部观测和本机恢复；看到公告不代表你的额度已经到账。公开监控源可能延迟或不可用，历史内容保留其时间和出处。首次读到历史消息不补发成新通知。

同一消息源已正常启用后，短时断网或重启可追补24小时内尚未处理的重置阶段；公告仍保留原发布时间，提醒另记本次处理时间。升级识别规则会重新判断已保存的近期内容，但保留原有通知收据；已经处理、首次使用或重新开启提醒时的历史基线、过期内容不会反复提醒。Windows接受通知请求不等于已在屏幕上显示，系统通知设置仍会影响可见性。

公开重置记录由 [Codex Resets](https://codex-resets.com/) 提供，应用保留来源署名。通知与消息阅读是两件事：首次使用请在设置中选择是否开启“消息提醒”；升级保留该选择。中文转述仅在来源提供中文时展示，否则给出中文摘要并保留原文，不把规则摘要称作全文翻译。

消息会自动检查。Windows版优先遵循进程已有代理配置，无环境代理时沿用Windows已启用的手动系统代理。普通网络中断后自动恢复检查；网站明确要求等待时按其时间重试。消息页会区分已保存内容、部分来源可用及同步成功，并显示有效的下次自动检查时间。旧版无法判断原因的等待预约会保留到期一次，升级不会提前重发请求。

盲比仍待内测，会员注册、上传与云社区尚未上线。当前包不附带个人消息缓存、认证、用户资料或服务密钥。其他系统没有本次验收包。

## 停用与卸载

在应用设置中关闭随 Codex 启动并退出工具。可删除上面的应用安装目录及其桌面/开始菜单入口。个人数据目录默认保留；需要清除时先导出你要保留的资料，再删除本工具自己的数据目录，绝不要删除 Codex 自身目录。遇到文件仍被占用，先正常退出，不终止 Explorer。
