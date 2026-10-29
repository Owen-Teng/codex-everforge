# Third-party notices

Dependencies are pinned in go.mod / go.sum. No code from CodexBar or CodexVisual is included.

| Component | Version | License | Used by |
|---|---|---|---|
| Go runtime / standard library | Build toolchain 1.26.4 | BSD-style | All Go binaries |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | Windows process cleanup |
| fyne.io/systray | v1.12.2 | Apache-2.0 | macOS and Linux system tray |
| github.com/godbus/dbus/v5 | v5.1.0 | BSD-2-Clause | Linux D-Bus tray |
| Microsoft WebView2 SDK | 1.0.4314.35 | Microsoft WebView2 SDK license | Windows popup host; full license in popup/WebView2-LICENSE.txt |

The complete upstream license texts are included in `licenses/`. The Windows frontend uses the installed Microsoft .NET Framework and Windows system libraries; those libraries are not redistributed in this package.

Original application source code and documentation are licensed under MIT; see LICENSE. Artwork and marks are outside that grant; see ASSETS-LICENSE.md. The project owner selected this scope on 2026-10-29. Dependencies retain the licenses listed above.

Build workflow actions are pinned to upstream commits. They are build services, not bundled runtime code. The CLI executable and its authentication state are supplied by the user and are not distributed with this tool.

Taskbar integration research: the TrafficMonitor project was consulted as an architectural reference for taskbar child windows. No TrafficMonitor code is copied, linked or bundled. Our renderer is restricted to this application's single Shell notification button, using independently implemented Windows API calls.
