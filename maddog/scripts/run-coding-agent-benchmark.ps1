param(
  [string]$BenchmarkDir = "C:\Dev2\research\coding-agent-benchmark",
  [string]$GoExe = "",
  [string]$Model = "",
  [string]$TasksDir = "",
  [switch]$DryRun,
  [switch]$SmokeOnly,
  [switch]$LocalSmoke,
  [switch]$UseProxy
)

$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$MaddogBin = Join-Path $RepoRoot "bin\maddog.exe"
$FixtureBin = Join-Path $RepoRoot "bin\coding-agent-benchmark-fixture.exe"
$ConfigTemplate = Join-Path $RepoRoot "benchmarks\coding-agent-benchmark\maddog.config.yaml"
$ConfigOut = Join-Path $RepoRoot ".benchmark\coding-agent-benchmark\maddog.config.yaml"
$MaddogHome = Join-Path $RepoRoot ".benchmark\maddog-home"
$SmokeTasksDir = Join-Path $RepoRoot ".benchmark\coding-agent-benchmark\tasks-smoke"
$LocalTasksDir = Join-Path $RepoRoot ".benchmark\coding-agent-benchmark\tasks-local-smoke"
$FixtureReadyFile = Join-Path $RepoRoot ".benchmark\coding-agent-benchmark\fixture-url.txt"
$DefaultModel = "icodeeasy/gpt-4.1"
$LocalSmokeModel = "benchmark-local/local-smoke-model"
$LocalSmokeKeyEnv = "MADDOG_BENCHMARK_LOCAL_KEY"

function Get-BenchmarkResultDirs {
  param([string]$Root)
  $resultsRoot = Join-Path $Root "results"
  if (!(Test-Path $resultsRoot)) {
    return @()
  }
  return @(Get-ChildItem $resultsRoot -Directory | Sort-Object LastWriteTimeUtc)
}

function Find-NewBenchmarkResultDir {
  param(
    [string]$Root,
    [string[]]$Before
  )
  $beforeSet = @{}
  foreach ($path in $Before) {
    $beforeSet[$path] = $true
  }
  $newDirs = @(
    Get-BenchmarkResultDirs $Root |
      Where-Object { -not $beforeSet.ContainsKey($_.FullName) } |
      Sort-Object LastWriteTimeUtc -Descending
  )
  if ($newDirs.Count -gt 0) {
    return $newDirs[0].FullName
  }
  $allDirs = @(Get-BenchmarkResultDirs $Root | Sort-Object LastWriteTimeUtc -Descending)
  if ($allDirs.Count -gt 0) {
    return $allDirs[0].FullName
  }
  return ""
}

function Assert-LocalSmokeResult {
  param([string]$RunDir)
  if ($RunDir -eq "" -or !(Test-Path $RunDir)) {
    throw "coding-agent-benchmark did not write a results directory"
  }
  $summaryPath = Join-Path $RunDir "summary.json"
  $taskPath = Join-Path (Join-Path $RunDir "maddog") "00-smoke-test.json"
  if (!(Test-Path $summaryPath)) {
    throw "coding-agent-benchmark summary missing: $summaryPath"
  }
  if (!(Test-Path $taskPath)) {
    throw "coding-agent-benchmark Maddog smoke result missing: $taskPath"
  }
  $summary = Get-Content -Raw $summaryPath | ConvertFrom-Json
  if (!$summary.maddog) {
    throw "coding-agent-benchmark summary does not contain Maddog results"
  }
  if ([int]$summary.maddog.total_tasks -ne 1 -or [int]$summary.maddog.tasks_fully_passed -ne 1) {
    throw "coding-agent-benchmark Maddog local smoke summary did not fully pass: $summaryPath"
  }
  $score = @($summary.maddog.scores)[0]
  if (!$score -or $score.task -ne "00-smoke-test" -or $score.status -ne "pass" -or [int]$score.tests_passed -ne 2 -or [int]$score.tests_total -ne 2) {
    throw "coding-agent-benchmark Maddog smoke score is not a 2/2 pass: $summaryPath"
  }
  $task = Get-Content -Raw $taskPath | ConvertFrom-Json
  if (-not $task.passed -or $task.status -ne "pass" -or [int]$task.tests_passed -ne 2 -or [int]$task.tests_total -ne 2) {
    throw "coding-agent-benchmark Maddog smoke task did not pass 2/2 tests: $taskPath"
  }
  $raw = [string]$task.agent_raw_output
  foreach ($needle in @("Benchmark smoke fixture completed.", "write_file", "math_utils.py", "out 6")) {
    if (!$raw.Contains($needle)) {
      throw "coding-agent-benchmark Maddog smoke output missing '$needle': $taskPath"
    }
  }
  $testRaw = [string]$task.test_raw_output
  if (!$testRaw.Contains("2 passed")) {
    throw "coding-agent-benchmark Maddog smoke pytest output did not report 2 passed: $taskPath"
  }
  Write-Host "Verified coding-agent-benchmark local smoke result: $RunDir"
}

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
& $GoExe build -o $MaddogBin ./cmd/maddog
if ($LocalSmoke) {
  & $GoExe build -o $FixtureBin ./cmd/coding-agent-benchmark-fixture
}

$config = Get-Content -Raw $ConfigTemplate
$config = $config.Replace("__MADDOG_BIN__", ($MaddogBin -replace "\\", "/"))
$config = $config.Replace("__MADDOG_HOME__", ($MaddogHome -replace "\\", "/"))
if ($LocalSmoke) {
  $config = $config.Replace("__MADDOG_BENCHMARK_LOCAL_KEY__", "local-fixture-key")
} else {
  $config = $config.Replace("__MADDOG_BENCHMARK_LOCAL_KEY__", "")
}
if ($LocalSmoke) {
  $config = $config.Replace("__MADDOG_MODEL__", $LocalSmokeModel)
} elseif ($Model -ne "") {
  $config = $config.Replace("__MADDOG_MODEL__", $Model)
} else {
  $config = $config.Replace("__MADDOG_MODEL__", $DefaultModel)
}
[System.IO.File]::WriteAllText($ConfigOut, $config, [System.Text.UTF8Encoding]::new($false))

$fixtureProcess = $null
$pushedBenchmarkDir = $false
$beforeResultDirs = @(Get-BenchmarkResultDirs $BenchmarkDir | ForEach-Object { $_.FullName })
try {
  if ($LocalSmoke) {
    if (Test-Path $FixtureReadyFile) {
      Remove-Item -Force $FixtureReadyFile
    }
    $fixtureProcess = Start-Process -FilePath $FixtureBin -ArgumentList @("-ready-file", $FixtureReadyFile) -NoNewWindow -PassThru
    $deadline = (Get-Date).AddSeconds(10)
    while (!(Test-Path $FixtureReadyFile) -and (Get-Date) -lt $deadline) {
      Start-Sleep -Milliseconds 100
    }
    if (!(Test-Path $FixtureReadyFile)) {
      throw "Local coding-agent benchmark fixture did not start."
    }
    $fixtureUrl = (Get-Content -Raw $FixtureReadyFile).Trim()
    if ($fixtureUrl -eq "") {
      throw "Local coding-agent benchmark fixture wrote an empty URL."
    }

    if (Test-Path $LocalTasksDir) {
      Remove-Item -Recurse -Force $LocalTasksDir
    }
    New-Item -ItemType Directory -Force -Path $LocalTasksDir | Out-Null
    $taskName = "00-smoke-test"
    $src = Join-Path (Join-Path $BenchmarkDir "tasks") $taskName
    if (!(Test-Path $src)) {
      throw "Local smoke benchmark task not found: $src"
    }
    Copy-Item -Recurse -Force $src (Join-Path $LocalTasksDir $taskName)
    $taskRepo = Join-Path (Join-Path $LocalTasksDir $taskName) "repo"
    $taskConfig = @"
default_model = "$LocalSmokeModel"

[[providers]]
name = "benchmark-local"
kind = "openai"
base_url = "$fixtureUrl"
model = "local-smoke-model"
api_key_env = "$LocalSmokeKeyEnv"
no_proxy = true
"@
    [System.IO.File]::WriteAllText((Join-Path $taskRepo "maddog.toml"), $taskConfig, [System.Text.UTF8Encoding]::new($false))
    $TasksDir = $LocalTasksDir
  } elseif ($SmokeOnly -and $TasksDir -eq "") {
    if (Test-Path $SmokeTasksDir) {
      Remove-Item -Recurse -Force $SmokeTasksDir
    }
    New-Item -ItemType Directory -Force -Path $SmokeTasksDir | Out-Null
    foreach ($taskName in @("00-smoke-test", "03-typescript-feature-table-filter")) {
      $src = Join-Path (Join-Path $BenchmarkDir "tasks") $taskName
      if (!(Test-Path $src)) {
        throw "Smoke benchmark task not found: $src"
      }
      Copy-Item -Recurse -Force $src (Join-Path $SmokeTasksDir $taskName)
    }
    $TasksDir = $SmokeTasksDir
  }

  $args = @("-m", "harness.run", "--config", $ConfigOut)
  if ($TasksDir -ne "") {
    $args += @("--tasks-dir", $TasksDir)
  }
  if ($DryRun) {
    $args += "--dry-run"
  }

  Push-Location $BenchmarkDir
  $pushedBenchmarkDir = $true
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
  if ($LocalSmoke) {
    $runDir = Find-NewBenchmarkResultDir -Root $BenchmarkDir -Before $beforeResultDirs
    Assert-LocalSmokeResult $runDir
  }
} finally {
  if ($pushedBenchmarkDir) {
    Pop-Location
  }
  if ($fixtureProcess -and !$fixtureProcess.HasExited) {
    Stop-Process -Id $fixtureProcess.Id -Force
  }
}
