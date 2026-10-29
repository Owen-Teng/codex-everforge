param([ValidatePattern('^N54(?:\.[0-9]+)?$')][string]$Version='N54.5',[string]$OutputDirectory='')
$ErrorActionPreference='Stop'
$root=Split-Path $PSScriptRoot -Parent
$out=if($OutputDirectory){[IO.Path]::GetFullPath($OutputDirectory)}else{Join-Path $root 'dist/windows-x64'}
if(Test-Path -LiteralPath $out){throw 'Use an empty output directory for a reproducible package'}
New-Item -ItemType Directory -Path $out -Force|Out-Null
function Put([string]$Source,[string]$Relative){$dest=Join-Path $out $Relative;New-Item -ItemType Directory -Path (Split-Path $dest -Parent) -Force|Out-Null;Copy-Item -LiteralPath $Source -Destination $dest}
$compiler=Join-Path $env:WINDIR 'Microsoft.NET/Framework64/v4.0.30319/csc.exe'
if(!(Test-Path -LiteralPath $compiler)){throw '.NET Framework 4.x C# compiler required on Windows'}
$wpf=Join-Path (Split-Path $compiler -Parent) 'WPF'
Push-Location $root
try{
 & go build -trimpath -buildvcs=false -o (Join-Path $out 'quota-core.exe') ./cmd/quota-core
 if($LASTEXITCODE){throw 'Go build failed'}
 $sources=@('Program.cs','ContextMenuTheme.cs','WorkerTransport.cs','StartupIntegration.cs','StatusIcon.cs','TaskbarStatus.cs','DesktopIdentity.cs','DetailPopup.cs','SetupIntegration.cs','SetupForm.cs')|ForEach-Object{Join-Path $root ('windows/'+$_)}
 & $compiler /nologo /codepage:65001 /target:winexe /platform:anycpu /optimize+ "/out:$out/CodexQuota.exe" "/win32manifest:$root/windows/app.manifest" /r:System.dll /r:System.Core.dll /r:System.Drawing.dll /r:System.Windows.Forms.dll /r:System.Web.Extensions.dll "/r:$wpf/UIAutomationClient.dll" "/r:$wpf/UIAutomationTypes.dll" "/r:$wpf/WindowsBase.dll" @sources
 if($LASTEXITCODE){throw 'Native application build failed'}
 & $compiler /nologo /codepage:65001 /target:winexe /platform:anycpu /optimize+ "/out:$out/CodexQuotaLauncher.exe" /r:System.dll (Join-Path $root 'windows/Launcher.cs') (Join-Path $root 'windows/StartupIntegration.cs') (Join-Path $root 'windows/DesktopIdentity.cs')
 if($LASTEXITCODE){throw 'Launcher build failed'}
 & (Join-Path $root 'windows/popup/build.ps1') -OutputDirectory 'bin-public'
 if($LASTEXITCODE){throw 'Popup build failed'}
 foreach($p in @('MakerPopup.exe','Microsoft.Web.WebView2.Core.dll','Microsoft.Web.WebView2.WinForms.dll','WebView2Loader.dll','WebView2-LICENSE.txt')){Put (Join-Path $root ('windows/popup/bin-public/'+$p)) ('popup/'+$p)}
 foreach($p in @(Get-Content -LiteralPath 'windows/popup/package-files.txt'|Where-Object{$_.Trim() -and !$_.Trim().StartsWith('#')})){Put (Join-Path $root ('preview/desktop-popup/'+$p)) ('preview/desktop-popup/'+$p)}
 foreach($p in @('index.html','styles.css','app.js','translations.js','translations-r2.js','domain.js','live.js','assets/maker.png','assets/favicon.svg')){Put (Join-Path $root ('preview/community-mvp/'+$p)) ('preview/community-mvp/'+$p)}
 foreach($p in @('install-n54.ps1','open-everforge-n54.ps1')){Put (Join-Path $root ('scripts/'+$p)) ('scripts/'+$p)}
 foreach($p in @('go-LICENSE.txt','golang-x-sys-LICENSE.txt','fyne-systray-LICENSE.txt','godbus-LICENSE.txt')){Put (Join-Path $root ('licenses/'+$p)) ('licenses/'+$p)}
 foreach($p in @('LICENSE','ASSETS-LICENSE.md','THIRD_PARTY_NOTICES.md')){Put (Join-Path $root $p) $p}
 Put (Join-Path $root 'packaging/README-WINDOWS.md') 'README-WINDOWS.md'
 Put (Join-Path $root 'packaging/FEATURES.json') 'FEATURES.json'
 Put (Join-Path $root 'assets/everforge.ico') 'everforge.ico'
 [IO.File]::WriteAllText((Join-Path $out 'INSTALL.cmd'),'@echo off'+"`r`n"+'powershell.exe -NoProfile -File "%~dp0scripts\install-n54.ps1" -Action Install'+"`r`n"+'pause >nul'+"`r`n",[Text.ASCIIEncoding]::new())
 $files=@(Get-ChildItem -LiteralPath $out -Recurse -File|Sort-Object FullName|ForEach-Object{[ordered]@{path=$_.FullName.Substring($out.TrimEnd('\').Length+1).Replace('\','/');bytes=$_.Length;sha256=(Get-FileHash -LiteralPath $_.FullName).Hash.ToLowerInvariant()}})
 [ordered]@{schemaVersion=1;product='Codex Everforge';version=$Version;platform='windows-x64';channel='public-preview';codeSigning='unsigned';files=$files}|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $out 'package-manifest.json') -Encoding utf8
 Write-Output ('Built '+$out)
}finally{Pop-Location}
