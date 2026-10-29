param(
 [ValidateSet('Check','Install','Rollback')][string]$Action='Install',
 [string]$SourceDirectory=(Split-Path $PSScriptRoot -Parent),
 [string]$BackupDirectory='',
 [string]$ReportPath=''
)
$ErrorActionPreference='Stop'

function Assert-EverforgeRoot([string]$Root) {
 $full=[IO.Path]::GetFullPath($Root).TrimEnd('\','/')
 if($full -eq [IO.Path]::GetPathRoot($full).TrimEnd('\','/')){throw 'A drive root is not an application directory'}
 $cursor=$full
 while($cursor){
  if((Test-Path -LiteralPath $cursor) -and ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'Linked application paths are not supported'}
  $parent=Split-Path $cursor -Parent
  if($parent -eq $cursor){break};$cursor=$parent
 }
 return $full
}
function Get-EverforgePath([string]$Root,[string]$Relative) {
 if(!$Relative -or [IO.Path]::IsPathRooted($Relative) -or $Relative -match '(^|[\\/])\.\.?([\\/]|$)|:|[\x00-\x1f]|[. ]($|[\\/])'){throw 'Invalid package path'}
 $base=Assert-EverforgeRoot $Root
 $full=[IO.Path]::GetFullPath((Join-Path $base $Relative))
 if(!$full.StartsWith($base+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Package path is outside its directory'}
 [void](Assert-EverforgeRoot $full)
 return $full
}
function Get-EverforgeRequiredPaths {
 @(
  'CodexQuota.exe',
  'CodexQuotaLauncher.exe',
  'popup/MakerPopup.exe',
  'popup/Microsoft.Web.WebView2.Core.dll',
  'popup/Microsoft.Web.WebView2.WinForms.dll',
  'popup/WebView2-LICENSE.txt',
  'popup/WebView2Loader.dll',
  'preview/community-mvp/app.js',
  'preview/community-mvp/assets/favicon.svg',
  'preview/community-mvp/assets/maker.png',
  'preview/community-mvp/domain.js',
  'preview/community-mvp/index.html',
  'preview/community-mvp/live.js',
  'preview/community-mvp/styles.css',
  'preview/community-mvp/translations-r2.js',
  'preview/community-mvp/translations.js',
  'preview/desktop-popup/app.js',
  'preview/desktop-popup/assets/characters/female-a-maker.png',
  'preview/desktop-popup/assets/characters/female-a-seeker.png',
  'preview/desktop-popup/assets/characters/female-a-shaper.png',
  'preview/desktop-popup/assets/characters/female-a-torchbearer.png',
  'preview/desktop-popup/assets/characters/female-a-worldbuilder.png',
  'preview/desktop-popup/assets/characters/female-a-worldweaver.png',
  'preview/desktop-popup/assets/characters/female-b-maker.png',
  'preview/desktop-popup/assets/characters/female-b-seeker.png',
  'preview/desktop-popup/assets/characters/female-b-shaper.png',
  'preview/desktop-popup/assets/characters/female-b-torchbearer.png',
  'preview/desktop-popup/assets/characters/female-b-worldbuilder.png',
  'preview/desktop-popup/assets/characters/female-b-worldweaver.png',
  'preview/desktop-popup/assets/characters/male-a-maker.png',
  'preview/desktop-popup/assets/characters/male-a-seeker.png',
  'preview/desktop-popup/assets/characters/male-a-shaper.png',
  'preview/desktop-popup/assets/characters/male-a-torchbearer.png',
  'preview/desktop-popup/assets/characters/male-a-worldbuilder.png',
  'preview/desktop-popup/assets/characters/male-a-worldweaver.png',
  'preview/desktop-popup/assets/characters/male-b-maker.png',
  'preview/desktop-popup/assets/characters/male-b-seeker.png',
  'preview/desktop-popup/assets/characters/male-b-shaper.png',
  'preview/desktop-popup/assets/characters/male-b-torchbearer.png',
  'preview/desktop-popup/assets/characters/male-b-worldbuilder.png',
  'preview/desktop-popup/assets/characters/male-b-worldweaver.png',
  'preview/desktop-popup/assets/favicon.svg',
  'preview/desktop-popup/assets/maker.png',
  'preview/desktop-popup/base.css',
  'preview/desktop-popup/character-ui.css',
  'preview/desktop-popup/character-ui.js',
  'preview/desktop-popup/domain.js',
  'preview/desktop-popup/growth.css',
  'preview/desktop-popup/growth.js',
  'preview/desktop-popup/inbox-ui.js',
  'preview/desktop-popup/index.html',
  'preview/desktop-popup/maker-home.css',
  'preview/desktop-popup/maker-home.js',
  'preview/desktop-popup/popup-battle.js',
  'preview/desktop-popup/popup-translations.js',
  'preview/desktop-popup/popup-view.js',
  'preview/desktop-popup/popup.css',
  'preview/desktop-popup/rules.js',
  'preview/desktop-popup/translations-base.js',
  'preview/desktop-popup/translations.js',
  'preview/desktop-popup/upgrade-motion.js',
  'quota-core.exe',
  'everforge.ico',
  'scripts/install-n54.ps1',
  'scripts/open-everforge-n54.ps1',
  'README-WINDOWS.md',
  'FEATURES.json',
  'THIRD_PARTY_NOTICES.md',
  'licenses/go-LICENSE.txt',
  'licenses/golang-x-sys-LICENSE.txt',
  'licenses/fyne-systray-LICENSE.txt',
  'licenses/godbus-LICENSE.txt'
 )
}
function Test-EverforgePackage([string]$Root) {
 $base=Assert-EverforgeRoot $Root
 $manifest=Get-Content -LiteralPath (Get-EverforgePath $base 'package-manifest.json') -Raw | ConvertFrom-Json
 if($manifest.schemaVersion -ne 1 -or $manifest.product -ne 'Codex Everforge' -or $manifest.platform -ne 'windows-x64' -or $manifest.version -notmatch '^N54(?:\.[0-9]+)?$'){throw 'Unsupported package manifest'}
 $files=@($manifest.files);if($files.Count -lt 60 -or $files.Count -gt 200){throw 'Unexpected package file count'}
 $seen=New-Object 'Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
 foreach($entry in $files){
  if(!$seen.Add([string]$entry.path)){throw 'Duplicate package path'}
  if($entry.path -match '(^|/)(auth\.json|settings\.json|\.env|.*\.log)$' -or $entry.sha256 -notmatch '^[0-9a-fA-F]{64}$' -or [long]$entry.bytes -le 0){throw 'Invalid package entry'}
  $file=Get-EverforgePath $base $entry.path
  if(!(Test-Path -LiteralPath $file -PathType Leaf) -or (Get-Item -LiteralPath $file).Length -ne [long]$entry.bytes -or (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -ne $entry.sha256){throw ('Package verification failed: '+$entry.path)}
 }
 foreach($required in (Get-EverforgeRequiredPaths)){if(!$seen.Contains($required)){throw ('Package is incomplete: '+$required)}}
 return $manifest
}
function Restore-EverforgeFiles([string]$Target,[string]$Backup,[switch]$ValidateOnly) {
 $targetRoot=Assert-EverforgeRoot $Target;$backupRoot=Assert-EverforgeRoot $Backup
 $record=Get-Content -LiteralPath (Get-EverforgePath $backupRoot 'rollback.json') -Raw | ConvertFrom-Json
 if($record.schemaVersion -ne 1 -or $record.target -ne $targetRoot -or @($record.files).Count -lt 1){throw 'Rollback target mismatch'}
 # Validate every backup before touching the application.
 foreach($entry in $record.files){
  [void](Get-EverforgePath $targetRoot $entry.path)
  if($entry.existed){$saved=Get-EverforgePath $backupRoot ('files/'+$entry.path);if((Get-FileHash -LiteralPath $saved).Hash -ne $entry.before){throw 'Rollback backup verification failed'}}
 }
 if($ValidateOnly){return}
 $failures=New-Object 'Collections.Generic.List[object]'
 foreach($entry in $record.files){
  try{
   $dest=Get-EverforgePath $targetRoot $entry.path
   if($entry.existed){New-Item -ItemType Directory -Path (Split-Path $dest -Parent) -Force|Out-Null;Copy-Item -LiteralPath (Get-EverforgePath $backupRoot ('files/'+$entry.path)) -Destination $dest -Force;if((Get-FileHash -LiteralPath $dest).Hash -ne $entry.before){throw 'Rollback verification failed'}}
   elseif(Test-Path -LiteralPath $dest -PathType Leaf){Remove-Item -LiteralPath $dest -Force}
  }catch{$failures.Add([ordered]@{path=$entry.path;error=$_.Exception.Message})}
 }
 $outcome=[ordered]@{at=[DateTimeOffset]::Now.ToString('o');status=if($failures.Count){'partial_rollback'}else{'restored'};failedFiles=@($failures.ToArray());restartAllowed=($failures.Count -eq 0)}
 $outcome|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $backupRoot 'rollback-result.json') -Encoding UTF8
 if($failures.Count){throw ('Partial rollback; do not restart. '+$failures.Count+' file(s) need attention. See '+(Join-Path $backupRoot 'rollback-result.json'))}
}
function Install-EverforgeFiles([string]$Source,[string]$Target,[string]$Backup,[scriptblock]$BeforeWrite={},[scriptblock]$AfterFile={}) {
 $manifest=Test-EverforgePackage $Source
 $sourceRoot=Assert-EverforgeRoot $Source;$targetRoot=Assert-EverforgeRoot $Target;$backupRoot=Assert-EverforgeRoot $Backup
 if($sourceRoot -eq $targetRoot -or $sourceRoot.StartsWith($targetRoot+'\',[StringComparison]::OrdinalIgnoreCase) -or $targetRoot.StartsWith($sourceRoot+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Extract the package outside the installed application'}
 if($backupRoot -eq $targetRoot -or $backupRoot.StartsWith($targetRoot+'\',[StringComparison]::OrdinalIgnoreCase) -or $targetRoot.StartsWith($backupRoot+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Backup must be outside the installed application'}
 if(Test-Path -LiteralPath $backupRoot){throw 'Backup directory must be new'}
 $entries=@($manifest.files)+@([pscustomobject]@{path='package-manifest.json';sha256=(Get-FileHash -LiteralPath (Join-Path $sourceRoot 'package-manifest.json')).Hash})
 foreach($entry in $entries){[void](Get-EverforgePath $targetRoot $entry.path)}
 New-Item -ItemType Directory -Path $backupRoot -Force|Out-Null
 $records=@()
 foreach($entry in $entries){
  $dest=Get-EverforgePath $targetRoot $entry.path;$exists=Test-Path -LiteralPath $dest -PathType Leaf;$hash=$null
  if($exists){$hash=(Get-FileHash -LiteralPath $dest).Hash;$saved=Get-EverforgePath $backupRoot ('files/'+$entry.path);New-Item -ItemType Directory -Path (Split-Path $saved -Parent) -Force|Out-Null;Copy-Item -LiteralPath $dest -Destination $saved;if((Get-FileHash -LiteralPath $saved).Hash -ne $hash){throw 'Backup verification failed'}}
  $staged=Get-EverforgePath $backupRoot ('stage/'+$entry.path);New-Item -ItemType Directory -Path (Split-Path $staged -Parent) -Force|Out-Null;Copy-Item -LiteralPath (Get-EverforgePath $sourceRoot $entry.path) -Destination $staged
  if((Get-FileHash -LiteralPath $staged).Hash -ne $entry.sha256){throw 'Staged package changed during installation'}
  $records+=@([ordered]@{path=$entry.path;existed=$exists;before=$hash;after=$entry.sha256})
 }
 [ordered]@{schemaVersion=1;target=$targetRoot;version=$manifest.version;createdAt=[DateTimeOffset]::Now.ToString('o');files=$records}|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $backupRoot 'rollback.json') -Encoding UTF8
 & $BeforeWrite
 try {
  foreach($entry in $records){
   $dest=Get-EverforgePath $targetRoot $entry.path;New-Item -ItemType Directory -Path (Split-Path $dest -Parent) -Force|Out-Null
   Copy-Item -LiteralPath (Get-EverforgePath $backupRoot ('stage/'+$entry.path)) -Destination $dest -Force
   if((Get-FileHash -LiteralPath $dest).Hash -ne $entry.after){throw 'Installed checksum mismatch'}
   & $AfterFile $entry.path
  }
 } catch {
  $failure=$_.Exception.Message
  try{Restore-EverforgeFiles $targetRoot $backupRoot}catch{throw ('Installation failed; rollback needs attention. Backup: '+$backupRoot+'. '+$failure+'; '+$_.Exception.Message)}
  throw ('Installation failed; application files restored. '+$failure)
 }
 return [pscustomobject]@{version=$manifest.version;verifiedFiles=$records.Count;backup=$backupRoot}
}
function Invoke-EverforgeProcess([string]$File,[string[]]$Arguments,[int]$TimeoutSeconds=15) {
 $p=Start-Process -FilePath $File -ArgumentList $Arguments -WorkingDirectory (Split-Path $File -Parent) -WindowStyle Hidden -PassThru
 try{if(!$p.WaitForExit($TimeoutSeconds*1000)){throw 'Application command did not finish; no process was forced to stop'};return $p.ExitCode}finally{$p.Dispose()}
}
function Stop-EverforgeRuntime([string]$Target) {
 try{$event=[Threading.EventWaitHandle]::OpenExisting('Local\CodexQuotaMonitor.Launcher.Stop.v1');try{[void]$event.Set()}finally{$event.Dispose()}}catch [Threading.WaitHandleCannotBeOpenedException]{}
 foreach($p in @(Get-Process CodexQuotaLauncher -ErrorAction SilentlyContinue|Where-Object{$_.Path -eq (Join-Path $Target 'CodexQuotaLauncher.exe')})){if(!$p.WaitForExit(6500)){throw 'Launcher did not exit normally'}}
 if(@(Get-Process MakerPopup -ErrorAction SilentlyContinue|Where-Object{$_.Path -eq (Join-Path $Target 'popup/MakerPopup.exe')}).Count){[void](Invoke-EverforgeProcess (Join-Path $Target 'popup/MakerPopup.exe') @('--close'))}
 if(@(Get-Process CodexQuota -ErrorAction SilentlyContinue|Where-Object{$_.Path -eq (Join-Path $Target 'CodexQuota.exe')}).Count){[void](Invoke-EverforgeProcess (Join-Path $Target 'CodexQuota.exe') @('--quit'))}
 foreach($p in @(Get-Process CodexQuota,quota-core,MakerPopup -ErrorAction SilentlyContinue|Where-Object{$_.Path -and $_.Path.StartsWith($Target+'\',[StringComparison]::OrdinalIgnoreCase)})){if(!$p.WaitForExit(6500)){throw 'Application is still closing; no files were replaced'}}
}
function Start-EverforgeRuntime([string]$Target,[bool]$Launcher) {
 Start-Process -FilePath (Join-Path $Target 'CodexQuota.exe') -ArgumentList '--background' -WorkingDirectory $Target -WindowStyle Hidden|Out-Null
 if($Launcher){Start-Process -FilePath (Join-Path $Target 'CodexQuotaLauncher.exe') -WorkingDirectory $Target -WindowStyle Hidden|Out-Null}
}
function Test-EverforgeStartupConfigured([string]$Target) {
 $value=Get-ItemPropertyValue -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'CodexQuotaMonitor.CodexStartup' -ErrorAction SilentlyContinue
 return [string]$value -eq ('"'+(Join-Path $Target 'CodexQuotaLauncher.exe')+'"')
}
function Test-EverforgeShortcutOwner($Link,[string]$Target) {
 $allowed=@((Join-Path $Target 'CodexQuota.exe'),(Join-Path $Target 'popup/MakerPopup.exe'),(Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'))
 return $Link.TargetPath -in $allowed -and $Link.WorkingDirectory -eq $Target -and $Link.IconLocation -eq ((Join-Path $Target 'everforge.ico')+',0')
}
function Set-EverforgeShortcut([string]$Target,[string]$Version,[string]$Backup) {
 $desktop=[Environment]::GetFolderPath('DesktopDirectory');$shell=New-Object -ComObject WScript.Shell
 $newPath=Join-Path $desktop ('Codex Everforge - '+$Version+'.lnk')
 if(Test-Path -LiteralPath $newPath){
  $oldSame=$shell.CreateShortcut($newPath);if(!(Test-EverforgeShortcutOwner $oldSame $Target)){throw 'An unrelated shortcut already uses this name; it was not overwritten'}
  $savedSame=Get-EverforgePath $Backup ('shortcuts/'+(Split-Path $newPath -Leaf));New-Item -ItemType Directory -Path (Split-Path $savedSame -Parent) -Force|Out-Null;Copy-Item -LiteralPath $newPath -Destination $savedSame
  if((Get-FileHash -LiteralPath $savedSame).Hash -ne (Get-FileHash -LiteralPath $newPath).Hash){throw 'Same-version shortcut backup failed'}
 }
 $link=$shell.CreateShortcut($newPath)
 $link.TargetPath=Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe';$link.Arguments='-NoProfile -NonInteractive -WindowStyle Hidden -File "'+(Join-Path $Target 'scripts/open-everforge-n54.ps1')+'"';$link.WorkingDirectory=$Target;$link.WindowStyle=7;$link.IconLocation=(Join-Path $Target 'everforge.ico')+',0';$link.Description='Codex Everforge '+$Version;$link.Save()
 $check=$shell.CreateShortcut($newPath);if($check.TargetPath -ne $link.TargetPath -or $check.Arguments -ne $link.Arguments -or $check.WorkingDirectory -ne $link.WorkingDirectory -or $check.IconLocation -ne $link.IconLocation){throw 'New desktop shortcut verification failed'}
 foreach($old in @(Get-ChildItem -LiteralPath $desktop -Filter 'Codex Everforge - *.lnk' -File)){
  if($old.FullName -eq $newPath){continue};$prior=$shell.CreateShortcut($old.FullName)
  if(!(Test-EverforgeShortcutOwner $prior $Target)){continue}
  $saved=Get-EverforgePath $Backup ('shortcuts/'+$old.Name);New-Item -ItemType Directory -Path (Split-Path $saved -Parent) -Force|Out-Null;Copy-Item -LiteralPath $old.FullName -Destination $saved
  if((Get-FileHash -LiteralPath $saved).Hash -eq (Get-FileHash -LiteralPath $old.FullName).Hash){Remove-Item -LiteralPath $old.FullName}
 }
 return $newPath
}
function Restore-EverforgeShortcuts([string]$Target,[string]$Backup) {
 $record=Get-Content -LiteralPath (Get-EverforgePath $Backup 'rollback.json') -Raw|ConvertFrom-Json
 $desktop=[Environment]::GetFolderPath('DesktopDirectory');$shell=New-Object -ComObject WScript.Shell
 $oldDirectory=Get-EverforgePath $Backup 'shortcuts';$restored=@()
 if(Test-Path -LiteralPath $oldDirectory){
  foreach($old in @(Get-ChildItem -LiteralPath $oldDirectory -Filter 'Codex Everforge - *.lnk' -File)){
   $link=$shell.CreateShortcut($old.FullName);if(!(Test-EverforgeShortcutOwner $link $Target)){throw 'Backup shortcut target mismatch'}
   $dest=Join-Path $desktop $old.Name;Copy-Item -LiteralPath $old.FullName -Destination $dest -Force
   if((Get-FileHash -LiteralPath $dest).Hash -ne (Get-FileHash -LiteralPath $old.FullName).Hash){throw 'Shortcut restore verification failed'}
   $restored+=@($dest)
  }
 }
 $newPath=Join-Path $desktop ('Codex Everforge - '+$record.version+'.lnk')
 if($newPath -notin $restored -and (Test-Path -LiteralPath $newPath)){$link=$shell.CreateShortcut($newPath);if(Test-EverforgeShortcutOwner $link $Target){Remove-Item -LiteralPath $newPath}}
}
function Get-EverforgeEnvironment {
 $build=[Environment]::OSVersion.Version.Build
 $architecture=[Environment]::GetEnvironmentVariable('PROCESSOR_ARCHITEW6432');if(!$architecture){$architecture=[Environment]::GetEnvironmentVariable('PROCESSOR_ARCHITECTURE')}
 $webview=$false
 foreach($base in @('HKCU:\Software\Microsoft\EdgeUpdate\Clients','HKLM:\Software\Microsoft\EdgeUpdate\Clients','HKLM:\Software\WOW6432Node\Microsoft\EdgeUpdate\Clients')){
  foreach($key in @(Get-ChildItem -LiteralPath $base -ErrorAction SilentlyContinue)){$p=Get-ItemProperty -LiteralPath $key.PSPath -ErrorAction SilentlyContinue;if($p.name -match 'WebView2' -and $p.pv -and $p.pv -ne '0.0.0.0'){$webview=$true}}
 }
 return [pscustomobject]@{windowsBuild=$build;architecture=$architecture;supported=($env:OS -eq 'Windows_NT' -and $build -ge 22000 -and $architecture -eq 'AMD64');webView2=$webview;cliOnPath=[bool](Get-Command codex -ErrorAction SilentlyContinue);dotNetFramework=Test-Path -LiteralPath (Join-Path $env:WINDIR 'Microsoft.NET/Framework64/v4.0.30319/clr.dll')}
}
function Save-EverforgeResult([System.Collections.IDictionary]$Result,[string]$RequestedPath,[string]$DefaultDirectory) {
 # Check stays read-only unless its caller explicitly asks for a report.
 if(!$RequestedPath -and $Result['action'] -eq 'Check'){return ($Result|ConvertTo-Json -Depth 6)}
 try {
  if(!$RequestedPath){
   if(!$DefaultDirectory){$DefaultDirectory=Join-Path $env:LOCALAPPDATA 'CodexEverforge/install-reports'}
   $directory=Assert-EverforgeRoot $DefaultDirectory
   [void][IO.Directory]::CreateDirectory($directory)
   $RequestedPath=Join-Path $directory ((Get-Date -Format 'yyyyMMdd-HHmmss')+'-'+[guid]::NewGuid().ToString('N').Substring(0,8)+'.json')
  }
  $full=Assert-EverforgeRoot $RequestedPath
  $Result['reportPath']=$full;$Result['reportSaved']=$true
  [IO.File]::WriteAllText($full,($Result|ConvertTo-Json -Depth 6),[Text.UTF8Encoding]::new($false))
 } catch {
  $Result['reportSaved']=$false;$Result['reportWarning']='Installation result could not be saved: '+$_.Exception.Message
 }
 return ($Result|ConvertTo-Json -Depth 6)
}

# Dot sourcing exposes only the file transaction for isolated installer tests.
if($MyInvocation.InvocationName -eq '.'){return}
$target=Assert-EverforgeRoot (Join-Path $env:LOCALAPPDATA 'Programs/CodexQuotaMonitor')
$result=[ordered]@{schemaVersion=1;action=$Action;product='Codex Everforge';at=[DateTimeOffset]::Now.ToString('o');status='checking';installed=$false;target=$target}
$exitCode=1
$installMutex=$null;$ownsInstallMutex=$false
try {
 if($Action -ne 'Check'){$installMutex=New-Object Threading.Mutex($false,'Local\CodexEverforge.Installer.v1');try{$ownsInstallMutex=$installMutex.WaitOne(0)}catch [Threading.AbandonedMutexException]{$ownsInstallMutex=$true};if(!$ownsInstallMutex){throw 'Another install or rollback is running; no files were changed'}}
 if($Action -eq 'Rollback'){
  $backup=Assert-EverforgeRoot $BackupDirectory;$base=Assert-EverforgeRoot (Join-Path $env:LOCALAPPDATA 'CodexEverforge/backups')
  if(!$backup.StartsWith($base+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Use a backup created by this installer'}
  Restore-EverforgeFiles $target $backup -ValidateOnly
  $wasLauncher=(@(Get-Process CodexQuotaLauncher -ErrorAction SilentlyContinue|Where-Object{$_.Path -eq (Join-Path $target 'CodexQuotaLauncher.exe')}).Count -gt 0) -or (Test-EverforgeStartupConfigured $target)
  Stop-EverforgeRuntime $target;Restore-EverforgeFiles $target $backup
  try{Restore-EverforgeShortcuts $target $backup}finally{if(Test-Path -LiteralPath (Join-Path $target 'CodexQuota.exe')){Start-EverforgeRuntime $target $wasLauncher}}
  $result.status='rolled_back';$exitCode=0
 }else{
  $manifest=Test-EverforgePackage $SourceDirectory;$environment=Get-EverforgeEnvironment;$result.version=$manifest.version;$result.environment=$environment;$result.verifiedFiles=@($manifest.files).Count
  if($Action -eq 'Check'){$result.status='package_verified';$exitCode=0}
  elseif(!$environment.supported -or !$environment.dotNetFramework){$result.status='unsupported_environment';$result.nextStep='This build requires Windows 11 x64 and the built-in .NET Framework.'}
  elseif(!$environment.webView2){$result.status='needs_webview2';$result.nextStep='Install Microsoft Edge WebView2 Evergreen Runtime from https://developer.microsoft.com/microsoft-edge/webview2/ and retry.'}
  else{
   $existing=Test-Path -LiteralPath (Join-Path $target 'CodexQuota.exe');$wasLauncher=(@(Get-Process CodexQuotaLauncher -ErrorAction SilentlyContinue|Where-Object{$_.Path -eq (Join-Path $target 'CodexQuotaLauncher.exe')}).Count -gt 0) -or (Test-EverforgeStartupConfigured $target)
   $backup=Join-Path (Join-Path $env:LOCALAPPDATA 'CodexEverforge/backups') ((Get-Date -Format 'yyyyMMdd-HHmmss')+'-'+[guid]::NewGuid().ToString('N').Substring(0,8));$result.backup=$backup
   try{$transaction=Install-EverforgeFiles $SourceDirectory $target $backup {Stop-EverforgeRuntime $target}}catch{if($existing -and (Test-Path -LiteralPath (Join-Path $target 'CodexQuota.exe')) -and $_.Exception.Message -notmatch 'rollback needs attention'){Start-EverforgeRuntime $target $wasLauncher};throw}
   $result.installed=$true;$result.verifiedFiles=$transaction.verifiedFiles
   try{$result.shortcut=Set-EverforgeShortcut $target $manifest.version $backup}catch{$result.shortcutWarning=$_.Exception.Message}
   if($existing){Start-EverforgeRuntime $target $wasLauncher;$result.status='updated_preserving_configuration';$exitCode=0}
   else{
    $configured=Invoke-EverforgeProcess (Join-Path $target 'CodexQuota.exe') @('--configure') 90
    $result.status=if($configured -eq 0){'installed_and_configured'}else{'installed_needs_attention'}
    $result.nextStep=if($configured -eq 0){'Open the quota indicator. Verify quota, messages, and dismissal on this computer.'}else{'Open CodexQuota.exe --setup to view the exact incomplete step. Use the official Codex login if quota is unavailable; never send credentials to anyone.'}
    $exitCode=if($configured -eq 0){0}else{2}
   }
   if($result.Contains('shortcutWarning')){$result.status='installed_needs_attention';$result.nextStep='Application files are installed; desktop shortcut needs attention. Start the installed CodexQuota.exe directly.';$exitCode=2}
  }
 }
}catch{$result.status='failed';$result.error=$_.Exception.Message}
finally{if($ownsInstallMutex){$installMutex.ReleaseMutex()};if($null -ne $installMutex){$installMutex.Dispose()}}
$json=Save-EverforgeResult $result $ReportPath ''
Write-Output $json
exit $exitCode
