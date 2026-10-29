$ErrorActionPreference='Stop'
$appRoot=Split-Path $PSScriptRoot -Parent
Start-Process -FilePath (Join-Path $appRoot 'CodexQuota.exe') -ArgumentList '--background' -WorkingDirectory $appRoot -WindowStyle Hidden
$popupRoot=Join-Path $appRoot 'popup'
# This is the visible application explicitly opened by the desktop shortcut.
Start-Process -FilePath (Join-Path $popupRoot 'MakerPopup.exe') -ArgumentList '--show' -WorkingDirectory $popupRoot -WindowStyle Normal
