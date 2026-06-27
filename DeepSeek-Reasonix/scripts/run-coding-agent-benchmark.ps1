param(
  [string]$BenchmarkDir = "C:\Dev2\research\coding-agent-benchmark",
  [string]$GoExe = "",
  [string]$Model = "",
  [string]$TasksDir = "",
  [switch]$DryRun,
  [switch]$UseProxy
)

$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$MaddogBin = Join-Path $RepoRoot "bin\maddog.exe"
$ConfigTemplate = Join-Path $RepoRoot "benchmarks\coding-agent-benchmark\maddog.config.yaml"
$ConfigOut = Join-Path $RepoRoot ".benchmark\coding-agent-benchmark\maddog.config.yaml"
$MaddogHome = Join-Path $RepoRoot ".benchmark\maddog-home"
$DefaultModel = "icodeeasy/gpt-4.1"

if ($UseProxy) {
  $env:HTTP_PROXY = "http://127.0.0.1:10809"
  $env:HTTPS_PROXY = "http://127.0.0.1:10809"
}
$env:PYTHONUTF8 = "1"

if (!(Test-Path $BenchmarkDir)) {
  New-Item -ItemType Directory -Force -Path (Split-Path $BenchmarkDir) | Out-Null
  git clone https://github.com/usamadar/coding-agent-benchmark $BenchmarkDir
}

if ($GoExe -eq "") {
  $goCmd = Get-Command go -ErrorAction SilentlyContinue
  if ($goCmd) {
    $GoExe = $goCmd.Source
  } elseif (Test-Path "C:\Dev2\.tools\go1.26.4\bin\go.exe") {
    $GoExe = "C:\Dev2\.tools\go1.26.4\bin\go.exe"
  } else {
    throw "Go executable not found. Pass -GoExe C:\path\to\go.exe."
  }
}

New-Item -ItemType Directory -Force -Path (Split-Path $MaddogBin), (Split-Path $ConfigOut), $MaddogHome | Out-Null
& $GoExe build -o $MaddogBin ./cmd/reasonix

$config = Get-Content -Raw $ConfigTemplate
$config = $config.Replace("__MADDOG_BIN__", ($MaddogBin -replace "\\", "/"))
$config = $config.Replace("__MADDOG_HOME__", ($MaddogHome -replace "\\", "/"))
if ($Model -ne "") {
  $config = $config.Replace("__MADDOG_MODEL__", $Model)
} else {
  $config = $config.Replace("__MADDOG_MODEL__", $DefaultModel)
}
[System.IO.File]::WriteAllText($ConfigOut, $config, [System.Text.UTF8Encoding]::new($false))

$args = @("-m", "harness.run", "--config", $ConfigOut)
if ($TasksDir -ne "") {
  $args += @("--tasks-dir", $TasksDir)
}
if ($DryRun) {
  $args += "--dry-run"
}

Push-Location $BenchmarkDir
try {
  python -m pip install -r requirements.txt | Out-Host
  if ($LASTEXITCODE -ne 0) {
    throw "pip install exited with code $LASTEXITCODE"
  }
  python -m pip install "pytest>=8.0" | Out-Host
  if ($LASTEXITCODE -ne 0) {
    throw "pytest install exited with code $LASTEXITCODE"
  }
  python @args
  if ($LASTEXITCODE -ne 0) {
    throw "coding-agent-benchmark exited with code $LASTEXITCODE"
  }
} finally {
  Pop-Location
}
