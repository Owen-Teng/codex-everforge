# Codex Everforge

**A Windows taskbar companion for Codex quota and public reset notices.**
See your remaining quota at a glance, open a compact desktop panel, and follow public reset updates with links to their sources.

[Download Windows 11 x64 beta](https://github.com/Owen-Teng/codex-everforge/releases/tag/v0.8.0-beta.1) · [Installation guide](INSTALL-WINDOWS.md) · [Feature status](FEATURES.md) · [中文](README.md)

Windows 11 x64 · v0.8.0-beta.1 · Unsigned pre-release · Independent project, not affiliated with or endorsed by OpenAI.

[Explore the website compatibility preview](https://owen-teng.github.io/codex-everforge/) · [Website source and capabilities](docs/README.md)

> Interactive presentation candidate with a project-authored scene and sample data. Accounts and online community services are not live.

## Keep quota in view

Checking quota should take a glance. Everforge puts an icon and a number in the taskbar, with a compact panel for the details. Public reset messages stay separate from changes in your own quota: a public announcement does not mean your account has already recovered.

- **Taskbar quota:** reads quota through your own signed-in official Codex CLI.
- **Public reset messages:** view public observations, summaries, and source links. Notifications are optional.
- **Three coordinated themes:** the panel and native right-click menu follow the selected theme.
- **Local preferences:** keep your theme, profile text, and optional work links on your computer; optionally start with Codex.

## Get started

1. Download the Windows x64 ZIP and `SHA256SUMS.txt` from the same release.
2. Verify the ZIP checksum, extract it, and run `INSTALL.cmd`. Requirements: Windows 11 x64, .NET Framework 4.x, WebView2 Runtime, and your own signed-in official Codex CLI.
3. Open Everforge. In Settings, choose whether to enable message notifications and start with Codex. Notifications are off by default; existing messages form a silent baseline when you first enable them.

No X developer account or key is required. The package is unsigned; check the release and checksum before installing. Installation and rollback details are in the [guide](INSTALL-WINDOWS.md).

**Ask your Codex to help install:**

> Install Codex Everforge v0.8.0-beta.1 from https://github.com/Owen-Teng/codex-everforge/releases/tag/v0.8.0-beta.1. Read the release instructions, download the Windows x64 ZIP and matching SHA256SUMS.txt, and verify the checksum. Extract it and run scripts/install-n54.ps1 -Action Check before installing. Preserve existing settings; if this exact version already matches, check it without reinstalling. Ask me to complete any required Codex login myself. Do not read or upload authentication contents. Report the installed version and anything I still need to do.

## What is available

| Capability | Status |
| --- | --- |
| Taskbar quota and desktop panel | Available in this Windows beta |
| Public reset messages | Third-party source, normally checked every five minutes |
| Three themes and matching native menu | Available; menu appearance confirmed on the developer's PC |
| Profile text and work links | Local only; no account registration required |
| Cloud accounts, OAuth, paid membership | Not available |
| Online community and blind comparison | Not available; website previews use example data |
| Cloud automatic software updates | Not available; use the published installer for upgrades |
| Other operating systems | Not included in this release |

## Message timing and beta limitations

The default public source is [Codex Resets](https://codex-resets.com/), checked every five minutes. Upstream collection, caching, outages, network access, and a sleeping or offline PC can add delay or cause missed messages. This is not an official real-time X feed, a complete author timeline, or a guarantee that every reset will be reported. Internal AIHOT configuration and cached source text are excluded from the public package.

Two local quota-recovery issues remain: an unknown error or partial reading may clear the comparison baseline; recovery notifications do not yet have an independent reliable queue and may be overwritten by the source-notification path. This beta does not claim to fix all missed notifications.

Validation so far is on one Windows PC. A second PC, actual sleep/resume, multi-monitor behavior, and future naturally arriving messages still need verification. Native menu images are isolated WinForms renders unless explicitly labeled as installed-app captures.

## Privacy and development

Quota is read locally through your CLI. Everforge does not upload your authentication files, API keys, or personal quota to a community service. Public-message checks connect to a third-party site. Local profile information and preferences remain on your PC.

To build on Windows, use Go 1.26.x and the .NET Framework 4.x C# compiler, then run `powershell -NoProfile -File scripts/build.ps1`. See the [build script](scripts/build.ps1). Source code and documentation use the [MIT license](LICENSE); artwork and brand assets have [separate terms](ASSETS-LICENSE.md). See [third-party notices](THIRD_PARTY_NOTICES.md). Please include your version and reproduction steps when [reporting an issue](https://github.com/Owen-Teng/codex-everforge/issues), and remove private data from logs or screenshots.
