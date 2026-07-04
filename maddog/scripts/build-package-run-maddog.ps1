[CmdletBinding()]
param(
  [string]$GoExe = "",
  [string]$WailsExe = "",
  [string]$Version = "dev",
  [string]$PackageName = "",
  [switch]$NoClean,
  [switch]$SkipBindings,
  [switch]$NoPackage,
  [switch]$NoLaunch,
  [switch]$UseUserConfig,
  [string]$LaunchProfileDir = "",
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$ExtraWailsArgs = @()
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$DesktopRoot = Join-Path $RepoRoot "desktop"
$BuildBin = Join-Path $DesktopRoot "build\bin"
$DefaultLaunchProfileDir = Join-Path $RepoRoot ".maddog\desktop-run-profile"
$OutputName = "maddog"
$ProductName = "Maddog"

$IsWindowsHost = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)
$IsMacOSHost = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)
$IsLinuxHost = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Linux)

function Resolve-Native {
  param([string]$Name)
  $cmd = Get-Command $Name -ErrorAction SilentlyContinue
  if (!$cmd) {
    return ""
  }
  return $cmd.Source
}

function Resolve-GoExe {
  param([string]$Requested)
  if ($Requested -ne "") {
    if (!(Test-Path -LiteralPath $Requested)) {
      throw "Go executable not found: $Requested"
    }
    return (Resolve-Path -LiteralPath $Requested).Path
  }

  $cmd = Resolve-Native "go"
  if ($cmd -ne "") {
    return $cmd
  }

  $bundled = "C:\Dev2\.tools\go1.26.4\bin\go.exe"
  if (Test-Path -LiteralPath $bundled) {
    return $bundled
  }

  throw "Go executable not found. Install Go or pass -GoExe C:\path\to\go.exe."
}

function Resolve-WailsExe {
  param([string]$Requested)
  if ($Requested -ne "") {
    if (!(Test-Path -LiteralPath $Requested)) {
      throw "Wails executable not found: $Requested"
    }
    return (Resolve-Path -LiteralPath $Requested).Path
  }

  $cmd = Resolve-Native "wails"
  if ($cmd -ne "") {
    return $cmd
  }

  $userWails = Join-Path $env:USERPROFILE "go\bin\wails.exe"
  if (Test-Path -LiteralPath $userWails) {
    return $userWails
  }

  throw "Wails CLI not found. Install it with: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
}

function Invoke-Native {
  param(
    [string]$FilePath,
    [string[]]$Arguments
  )

  Write-Host "==> $FilePath $($Arguments -join ' ')"
  $global:LASTEXITCODE = 0
  & $FilePath @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "$FilePath $($Arguments -join ' ') exited with code $LASTEXITCODE"
  }
}

function Get-HostArch {
  $arch = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString().ToLowerInvariant()
  switch ($arch) {
    "x64" { return "amd64" }
    "arm64" { return "arm64" }
    "x86" { return "386" }
    default { return $arch }
  }
}

function Get-PlatformName {
  $arch = Get-HostArch
  if ($IsWindowsHost) {
    return "windows-$arch"
  }
  if ($IsMacOSHost) {
    return "darwin-$arch"
  }
  if ($IsLinuxHost) {
    return "linux-$arch"
  }
  return "unknown-$arch"
}

function Get-DesktopArtifact {
  if ($IsWindowsHost) {
    $expected = Join-Path $BuildBin "$OutputName.exe"
    if (Test-Path -LiteralPath $expected) {
      return $expected
    }
    $exe = Get-ChildItem -LiteralPath $BuildBin -Filter "*.exe" -File -ErrorAction SilentlyContinue |
      Where-Object { $_.Name -notlike "*installer*" } |
      Select-Object -First 1
    if ($exe) {
      return $exe.FullName
    }
  }

  if ($IsMacOSHost) {
    $expected = Join-Path $BuildBin "$OutputName.app"
    if (Test-Path -LiteralPath $expected) {
      return $expected
    }
    $app = Get-ChildItem -LiteralPath $BuildBin -Filter "*.app" -Directory -ErrorAction SilentlyContinue |
      Select-Object -First 1
    if ($app) {
      return $app.FullName
    }
  }

  $expectedBinary = Join-Path $BuildBin $OutputName
  if (Test-Path -LiteralPath $expectedBinary) {
    return $expectedBinary
  }

  throw "Desktop build artifact not found under $BuildBin."
}

function New-PortablePackage {
  param([string]$ArtifactPath)

  $distDir = Join-Path $RepoRoot "dist"
  New-Item -ItemType Directory -Force -Path $distDir | Out-Null

  $name = $PackageName
  if ($name -eq "") {
    $safeVersion = if ($Version -ne "") { $Version -replace '[^A-Za-z0-9_.-]+', '-' } else { "dev" }
    $name = "$ProductName-$(Get-PlatformName)-$safeVersion.zip"
  }
  if (![System.IO.Path]::GetExtension($name)) {
    $name = "$name.zip"
  }

  $packagePath = Join-Path $distDir $name
  if (Test-Path -LiteralPath $packagePath) {
    Remove-Item -LiteralPath $packagePath -Force
  }

  Compress-Archive -LiteralPath $ArtifactPath -DestinationPath $packagePath -Force
  return $packagePath
}

function Resolve-LaunchProfileDir {
  if ($LaunchProfileDir -ne "") {
    if ([System.IO.Path]::IsPathRooted($LaunchProfileDir)) {
      return $LaunchProfileDir
    }
    return (Join-Path $RepoRoot $LaunchProfileDir)
  }
  return $DefaultLaunchProfileDir
}

function Restore-EnvVar {
  param(
    [string]$Name,
    [AllowNull()]
    [string]$Value
  )
  if ($null -eq $Value) {
    Remove-Item -LiteralPath "Env:$Name" -ErrorAction SilentlyContinue
    return
  }
  Set-Item -LiteralPath "Env:$Name" -Value $Value
}

function Start-Maddog {
  param([string]$ArtifactPath)

  $oldHome = $env:MADDOG_HOME
  $oldState = $env:MADDOG_STATE_HOME
  $oldCache = $env:MADDOG_CACHE_HOME

  try {
    if (!$UseUserConfig) {
      $profileRoot = Resolve-LaunchProfileDir
      $homeDir = Join-Path $profileRoot "home"
      $stateDir = Join-Path $profileRoot "state"
      $cacheDir = Join-Path $profileRoot "cache"
      New-Item -ItemType Directory -Force -Path $homeDir, $stateDir, $cacheDir | Out-Null
      $env:MADDOG_HOME = (Resolve-Path -LiteralPath $homeDir).Path
      $env:MADDOG_STATE_HOME = (Resolve-Path -LiteralPath $stateDir).Path
      $env:MADDOG_CACHE_HOME = (Resolve-Path -LiteralPath $cacheDir).Path
      Write-Host "Launch profile: isolated ($profileRoot). Pass -UseUserConfig to launch against the real user config."
    } else {
      Write-Host "Launch profile: real user config (-UseUserConfig)."
    }

    if ($IsMacOSHost -and $ArtifactPath.EndsWith(".app", [System.StringComparison]::OrdinalIgnoreCase)) {
      Start-Process -FilePath "open" -ArgumentList @($ArtifactPath) | Out-Null
      Write-Host "Launched $ArtifactPath"
      return
    }

    $process = Start-Process -FilePath $ArtifactPath -WorkingDirectory $RepoRoot -PassThru
    Write-Host "Launched $ArtifactPath (PID $($process.Id))"
  } finally {
    Restore-EnvVar "MADDOG_HOME" $oldHome
    Restore-EnvVar "MADDOG_STATE_HOME" $oldState
    Restore-EnvVar "MADDOG_CACHE_HOME" $oldCache
  }
}

$GoExe = Resolve-GoExe $GoExe
$WailsExe = Resolve-WailsExe $WailsExe

if ((Resolve-Native "pnpm") -eq "") {
  throw "pnpm is required by desktop/wails.json. Install it with: npm install -g pnpm"
}

$buildArgs = @("build")
if (!$NoClean) {
  $buildArgs += "-clean"
}
if ($SkipBindings) {
  $buildArgs += "-skipbindings"
}
if ($ExtraWailsArgs.Count -gt 0) {
  $buildArgs += $ExtraWailsArgs
}

Write-Host "Repo: $RepoRoot"
Write-Host "Go: $GoExe"
Write-Host "Wails: $WailsExe"

$oldPath = $env:PATH
$goDir = Split-Path -Parent $GoExe
$env:PATH = "$goDir$([System.IO.Path]::PathSeparator)$oldPath"
Push-Location $DesktopRoot
try {
  Invoke-Native $WailsExe $buildArgs
} finally {
  Pop-Location
  $env:PATH = $oldPath
}

$artifact = Get-DesktopArtifact
Write-Host "Built artifact: $artifact"

if (!$NoPackage) {
  $package = New-PortablePackage $artifact
  Write-Host "Packaged artifact: $package"
}

if (!$NoLaunch) {
  Start-Maddog $artifact
} else {
  Write-Host "Launch skipped by -NoLaunch."
}
