param([ValidatePattern('^bin(?:-[a-zA-Z0-9-]+)?$')][string]$OutputDirectory='bin')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$version='1.0.4314.35'
$package=Join-Path $PSScriptRoot "vendor/microsoft.web.webview2.$version.nupkg"
$bin=Join-Path $PSScriptRoot $OutputDirectory
New-Item -ItemType Directory -Path $bin -Force | Out-Null
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip=[IO.Compression.ZipFile]::OpenRead($package)
try {
 $mapping=@{'lib/net462/Microsoft.Web.WebView2.Core.dll'='Microsoft.Web.WebView2.Core.dll';'lib/net462/Microsoft.Web.WebView2.WinForms.dll'='Microsoft.Web.WebView2.WinForms.dll';'runtimes/win-x64/native/WebView2Loader.dll'='WebView2Loader.dll';'LICENSE.txt'='WebView2-LICENSE.txt'}
 foreach($entry in $mapping.Keys){$item=$zip.GetEntry($entry);if(!$item){throw "Missing official SDK entry: $entry"};[IO.Compression.ZipFileExtensions]::ExtractToFile($item,(Join-Path $bin $mapping[$entry]),$true)}
}finally{$zip.Dispose()}
$compiler=Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
$output=Join-Path $bin 'MakerPopup.exe'
$sources=@('Host.cs','PopupForm.cs','UiThemePreference.cs','Native.cs','Assets.cs','Interaction.cs','Diagnostics.cs','ForegroundTicket.cs','QuotaBridge.cs') | ForEach-Object { Join-Path $PSScriptRoot $_ }
& $compiler /nologo /codepage:65001 /target:winexe /platform:x64 /optimize+ "/out:$output" "/win32manifest:$projectRoot\windows\app.manifest" /r:System.dll /r:System.Core.dll /r:System.Drawing.dll /r:System.Windows.Forms.dll /r:System.Web.Extensions.dll /r:System.Net.Http.dll "/r:$bin\Microsoft.Web.WebView2.Core.dll" "/r:$bin\Microsoft.Web.WebView2.WinForms.dll" @sources
if($LASTEXITCODE -ne 0){throw 'Native host build failed'}
Write-Output "Built isolated native host: $output"
