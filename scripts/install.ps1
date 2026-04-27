# Install the latest release of `bb` from GitHub Releases.
# Usage:
#   irm https://raw.githubusercontent.com/hugs7/bitbucket-cli/main/scripts/install.ps1 | iex
#   $env:BB_VERSION="v0.2.0"; irm .../install.ps1 | iex

[CmdletBinding()]
param(
  [string]$Version = $env:BB_VERSION,
  [string]$BinDir  = (Join-Path $env:LOCALAPPDATA 'bb\bin')
)

$ErrorActionPreference = 'Stop'
$Repo = 'hugs7/bitbucket-cli'
$Bin  = 'bb'

# Resolve latest version from GitHub if not pinned.
if (-not $Version) {
  $rel = Invoke-RestMethod -Headers @{ 'User-Agent' = 'bb-installer' } `
    -Uri "https://api.github.com/repos/$Repo/releases/latest"
  $Version = $rel.tag_name
}
$Num = $Version.TrimStart('v')

# Map architecture.
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported arch: $env:PROCESSOR_ARCHITECTURE" }
}

$asset = "${Bin}_${Num}_windows_${arch}.zip"
$url   = "https://github.com/$Repo/releases/download/$Version/$asset"

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "bb-install-$([guid]::NewGuid())")
try {
  Write-Host "downloading $asset…"
  $zip = Join-Path $tmp $asset
  Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
  Expand-Archive -Path $zip -DestinationPath $tmp -Force

  if (-not (Test-Path $BinDir)) { New-Item -ItemType Directory -Path $BinDir | Out-Null }
  Copy-Item -Force -Path (Join-Path $tmp "$Bin.exe") -Destination (Join-Path $BinDir "$Bin.exe")

  # Add BinDir to user PATH if missing.
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($userPath -split ';') -notcontains $BinDir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$BinDir", 'User')
    Write-Host "added $BinDir to your user PATH (open a new terminal)"
  }

  Write-Host "installed $Bin $Version -> $(Join-Path $BinDir "$Bin.exe")"
  & (Join-Path $BinDir "$Bin.exe") version
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
