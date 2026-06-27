param(
  [string]$GoExe = "",
  [string]$Model = "",
  [switch]$IncludeE2E,
  [string]$E2ETasks = "",
  [string]$E2ETags = "",
  [int]$E2EBudget = 400000,
  [switch]$IncludeFrontierSmoke,
  [switch]$IncludeExternal,
  [switch]$DryRunExternal,
  [string]$BenchmarkDir = "C:\Dev2\research\coding-agent-benchmark",
  [switch]$UseProxy,
  [switch]$SkipFrontend,
  [switch]$SkipFrontendBuild
)

$ErrorActionPreference = "Continue"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$ReportDir = Join-Path $RepoRoot ".benchmark\regression"
$LogDir = Join-Path $ReportDir "logs"
$SummaryJson = Join-Path $ReportDir "latest.json"
$SummaryMd = Join-Path $ReportDir "latest.md"
$MaddogBin = Join-Path $RepoRoot "bin\maddog.exe"

New-Item -ItemType Directory -Force -Path $ReportDir, $LogDir | Out-Null
Set-Location $RepoRoot

if ($UseProxy) {
  $env:HTTP_PROXY = "http://127.0.0.1:10809"
  $env:HTTPS_PROXY = "http://127.0.0.1:10809"
}

function Resolve-GoExe {
  param([string]$Requested)
  if ($Requested -ne "") {
    if (!(Test-Path $Requested)) {
      throw "Go executable not found: $Requested"
    }
    return (Resolve-Path $Requested).Path
  }
  $cmd = Get-Command go -ErrorAction SilentlyContinue
  if ($cmd) {
    return $cmd.Source
  }
  $bundled = "C:\Dev2\.tools\go1.26.4\bin\go.exe"
  if (Test-Path $bundled) {
    return $bundled
  }
  throw "Go executable not found. Pass -GoExe C:\path\to\go.exe."
}

function Resolve-Native {
  param([string]$Name)
  $cmd = Get-Command $Name -ErrorAction SilentlyContinue
  if (!$cmd) {
    return ""
  }
  return $cmd.Source
}

function Invoke-Native {
  param(
    [string]$FilePath,
    [string[]]$Arguments
  )
  $global:Error.Clear()
  & $FilePath @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "$FilePath $($Arguments -join ' ') exited with code $LASTEXITCODE"
  }
}

$GoExe = Resolve-GoExe $GoExe
$Results = New-Object System.Collections.Generic.List[object]
$HadFailure = $false

function Add-SkipStep {
  param(
    [string]$Name,
    [string]$Reason,
    [string[]]$Coverage
  )
  $Results.Add([pscustomobject]@{
    name = $Name
    status = "skip"
    required = $false
    duration_seconds = 0
    coverage = $Coverage
    command = ""
    log = ""
    reason = $Reason
  }) | Out-Null
  Write-Host "SKIP $Name - $Reason"
}

function Invoke-Step {
  param(
    [string]$Name,
    [string]$Command,
    [string[]]$Coverage,
    [bool]$Required,
    [scriptblock]$Action
  )
  $safeName = ($Name -replace '[^A-Za-z0-9_.-]+', '-').Trim('-')
  if ($safeName -eq "") {
    $safeName = "step"
  }
  $logPath = Join-Path $LogDir "$safeName.log"
  $sw = [System.Diagnostics.Stopwatch]::StartNew()
  $status = "pass"
  $reason = ""
  Write-Host "RUN  $Name"
  try {
    $global:Error.Clear()
    $global:LASTEXITCODE = 0
    & $Action *> $logPath
    if ($LASTEXITCODE -ne 0) {
      throw "last native command exited with code $LASTEXITCODE"
    }
  } catch {
    $status = "fail"
    $reason = $_.Exception.Message
    Add-Content -Path $logPath -Value ""
    Add-Content -Path $logPath -Value "ERROR: $reason"
    if ($Required) {
      $script:HadFailure = $true
    }
  } finally {
    $sw.Stop()
  }
  $Results.Add([pscustomobject]@{
    name = $Name
    status = $status
    required = $Required
    duration_seconds = [math]::Round($sw.Elapsed.TotalSeconds, 3)
    coverage = $Coverage
    command = $Command
    log = $logPath
    reason = $reason
  }) | Out-Null
  if ($status -eq "pass") {
    Write-Host "PASS $Name"
  } else {
    Write-Host "FAIL $Name - $reason"
  }
}

$CoverageMatrix = @(
  [pscustomobject]@{
    capability = "Provider API-key routing, official auth config, and OpenAI/Anthropic/iCodeEasy compatibility"
    evidence = @("core-go", "manifest", "provider-auth-frontier-profile")
    notes = "Covers API-key and official auth config shapes, including bearer/workload_identity token envs; real official OAuth/auth browser flows still require manual/provider credential validation."
  },
  [pscustomobject]@{
    capability = "Frontier/small-model routing, budgets, advisor escalation, and cost wrappers"
    evidence = @("core-go", "e2e optional", "frontier smoke optional")
    notes = "Frontier real calls are skipped unless -IncludeFrontierSmoke is used and credentials are present."
  },
  [pscustomobject]@{
    capability = "Anthropic native advisor tool and desktop advisor event presentation"
    evidence = @("core-go", "desktop-go", "frontend")
    notes = "Native advisor is unit-tested; provider-side beta behavior requires live Anthropic credentials."
  },
  [pscustomobject]@{
    capability = "Dynamic skills, project skill invocation, and subagent delegation"
    evidence = @("core-go", "manifest", "e2e optional")
    notes = "External coding benchmark does not inspect skill/advisor events."
  },
  [pscustomobject]@{
    capability = "C2 offline replay, scorer, guardrail, and skill promotion"
    evidence = @("core-go: internal/eval")
    notes = "Offline mechanics are local/unit verified; live frontier scoring requires optional provider runs."
  },
  [pscustomobject]@{
    capability = "Readiness evidence gate, tool metrics, tinyctx/compaction, and run metrics"
    evidence = @("core-go", "manifest", "e2e optional")
    notes = "The e2e suite records mechanism metrics when run against a real provider."
  },
  [pscustomobject]@{
    capability = "Maddog naming, config/storage isolation, desktop GUI settings, and app build"
    evidence = @("core-go", "desktop-go", "frontend", "manifest")
    notes = "Desktop installer/runtime smoke is separate from frontend and Wails package checks."
  },
  [pscustomobject]@{
    capability = "General coding-agent task performance"
    evidence = @("external coding-agent-benchmark optional")
    notes = "Use -IncludeExternal for usamadar/coding-agent-benchmark compatibility/performance runs."
  }
)

$CoreGoPackages = @(
  "./cmd/e2ebench",
  "./internal/agent",
  "./internal/boot",
  "./internal/cli",
  "./internal/config",
  "./internal/control",
  "./internal/eval",
  "./internal/evidence",
  "./internal/event",
  "./internal/provider",
  "./internal/provider/openai",
  "./internal/provider/anthropic",
  "./internal/provider/costwrap",
  "./internal/skill",
  "./internal/serve"
)

Invoke-Step `
  -Name "core-go" `
  -Command "$GoExe test $($CoreGoPackages -join ' ') -count=1" `
  -Coverage @("provider", "frontier", "auth", "official auth", "openai", "anthropic", "icodeeasy", "advisor", "skill", "subagent", "evidence", "tinyctx", "compaction", "eval", "desktop-wire", "desktop-parity") `
  -Required $true `
  -Action {
    Invoke-Native $GoExe (@("test") + $CoreGoPackages + @("-count=1"))
  }

Invoke-Step `
  -Name "coverage-audit" `
  -Command "$GoExe test ./cmd/e2ebench -run TestMaddogBenchmarkCoverageAudit -count=1" `
  -Coverage @("coverage-audit", "provider", "frontier", "auth", "official auth", "openai", "anthropic", "icodeeasy", "advisor", "upgrade", "skill", "subagent", "evidence", "tinyctx", "compaction", "eval", "desktop-parity", "external-harness") `
  -Required $true `
  -Action {
    Invoke-Native $GoExe @("test", "./cmd/e2ebench", "-run", "TestMaddogBenchmarkCoverageAudit", "-count=1")
  }

Invoke-Step `
  -Name "all-go" `
  -Command "$GoExe test ./... -count=1" `
  -Coverage @("all-go-packages", "desktop-go-module", "bot", "builtin-mcp", "checkpoint", "codegraph", "diff", "hooks", "lsp", "memory", "permissions", "plugins", "tools") `
  -Required $true `
  -Action {
    Invoke-Native $GoExe @("test", "./...", "-count=1")
  }

Invoke-Step `
  -Name "build-maddog-cli" `
  -Command "$GoExe build -o $MaddogBin ./cmd/reasonix" `
  -Coverage @("cli", "external-harness", "e2e") `
  -Required $true `
  -Action {
    New-Item -ItemType Directory -Force -Path (Split-Path $MaddogBin) | Out-Null
    Invoke-Native $GoExe @("build", "-o", $MaddogBin, "./cmd/reasonix")
  }

Invoke-Step `
  -Name "e2e-manifest" `
  -Command "$GoExe run ./cmd/e2ebench -mode manifest -out benchmarks/e2e/manifest.md -json benchmarks/e2e/manifest.json" `
  -Coverage @("benchmark-metadata", "coverage-manifest", "e2e-task-contract") `
  -Required $true `
  -Action {
    Invoke-Native $GoExe @("run", "./cmd/e2ebench", "-mode", "manifest", "-out", "benchmarks/e2e/manifest.md", "-json", "benchmarks/e2e/manifest.json")
  }

if ($SkipFrontend) {
  Add-SkipStep -Name "frontend" -Reason "Skipped by -SkipFrontend." -Coverage @("desktop-gui", "frontend")
} else {
  $npm = Resolve-Native "npm"
  if ($npm -eq "") {
    Invoke-Step `
      -Name "frontend" `
      -Command "npm checks" `
      -Coverage @("desktop-gui", "frontend") `
      -Required $true `
      -Action { throw "npm is not available on PATH" }
  } else {
    Invoke-Step `
      -Name "frontend" `
      -Command "npm install/ci if needed; npm run typecheck; npm run check:css; npm run test:all; npm run build" `
      -Coverage @("desktop-gui", "provider-settings", "advisor-ui", "maddog-isolation") `
      -Required $true `
      -Action {
        Push-Location (Join-Path $RepoRoot "desktop\frontend")
        try {
          if (!(Test-Path "node_modules")) {
            if (Test-Path "package-lock.json") {
              Invoke-Native $npm @("ci")
            } else {
              Invoke-Native $npm @("install")
            }
          }
          Invoke-Native $npm @("run", "typecheck")
          Invoke-Native $npm @("run", "check:css")
          Invoke-Native $npm @("run", "test:all")
          if (!$SkipFrontendBuild) {
            Invoke-Native $npm @("run", "build")
          }
        } finally {
          Pop-Location
        }
      }
  }
}

Invoke-Step `
  -Name "desktop-go" `
  -Command "$GoExe test ./... -count=1 (in desktop)" `
  -Coverage @("desktop-app", "desktop-settings", "desktop-events", "desktop-signing", "desktop-updater", "maddog-isolation") `
  -Required $true `
  -Action {
    $distIndex = Join-Path $RepoRoot "desktop\frontend\dist\index.html"
    if (!(Test-Path $distIndex)) {
      throw "desktop/frontend/dist/index.html is missing; run frontend build or omit -SkipFrontend/-SkipFrontendBuild before desktop Go tests"
    }
    Push-Location (Join-Path $RepoRoot "desktop")
    try {
      Invoke-Native $GoExe @("test", "./...", "-count=1")
    } finally {
      Pop-Location
    }
  }

if ($IncludeE2E) {
  $e2eArgs = @("run", "./cmd/e2ebench", "-bin", $MaddogBin, "-budget", "$E2EBudget", "-out", ".benchmark/regression/e2e.md", "-json", ".benchmark/regression/e2e.json")
  if ($Model -ne "") {
    $e2eArgs += @("-model", $Model)
  }
  if ($E2ETasks -ne "") {
    $e2eArgs += @("-tasks", $E2ETasks)
  }
  if ($E2ETags -ne "") {
    $e2eArgs += @("-tags", $E2ETags)
  }
  Invoke-Step `
    -Name "maddog-e2e" `
    -Command "$GoExe $($e2eArgs -join ' ')" `
    -Coverage @("real-provider", "mechanism-metrics", "headless-cli") `
    -Required $true `
    -Action {
      Invoke-Native $GoExe $e2eArgs
    }
} else {
  Add-SkipStep -Name "maddog-e2e" -Reason "Skipped by default. Use -IncludeE2E to run the real-provider e2e suite." -Coverage @("real-provider", "mechanism-metrics", "headless-cli")
}

if ($IncludeFrontierSmoke) {
  if ($env:ICODEEASY_API_KEY -eq "" -and $env:OPENAI_API_KEY -eq "" -and $env:ANTHROPIC_API_KEY -eq "") {
    Add-SkipStep -Name "frontier-smoke" -Reason "No ICODEEASY_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY is set." -Coverage @("frontier-real-call")
  } else {
    Invoke-Step `
      -Name "frontier-smoke" `
      -Command "$GoExe run ./cmd/e2ebench -bin $MaddogBin -tags frontier -budget $E2EBudget -out .benchmark/regression/frontier.md -json .benchmark/regression/frontier.json" `
      -Coverage @("frontier-real-call", "provider-auth-frontier-profile", "project-config-isolation") `
      -Required $true `
      -Action {
        $frontierArgs = @("run", "./cmd/e2ebench", "-bin", $MaddogBin, "-tags", "frontier", "-budget", "$E2EBudget", "-out", ".benchmark/regression/frontier.md", "-json", ".benchmark/regression/frontier.json")
        if ($Model -ne "") {
          $frontierArgs += @("-model", $Model)
        }
        Invoke-Native $GoExe $frontierArgs
      }
  }
} else {
  Add-SkipStep -Name "frontier-smoke" -Reason "Skipped by default. Use -IncludeFrontierSmoke with provider credentials for live frontier validation." -Coverage @("frontier-real-call")
}

if ($IncludeExternal) {
  $ps = Resolve-Native "powershell"
  if ($ps -eq "") {
    Invoke-Step `
      -Name "coding-agent-benchmark" `
      -Command "powershell -ExecutionPolicy Bypass -File scripts/run-coding-agent-benchmark.ps1" `
      -Coverage @("external-coding-benchmark") `
      -Required $true `
      -Action { throw "powershell is not available on PATH" }
  } else {
    Invoke-Step `
      -Name "coding-agent-benchmark" `
      -Command "powershell -ExecutionPolicy Bypass -File scripts/run-coding-agent-benchmark.ps1 -BenchmarkDir $BenchmarkDir" `
      -Coverage @("external-coding-benchmark", "agent-command-adapter") `
      -Required $true `
      -Action {
        $args = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", (Join-Path $RepoRoot "scripts\run-coding-agent-benchmark.ps1"), "-BenchmarkDir", $BenchmarkDir, "-GoExe", $GoExe)
        if ($Model -ne "") {
          $args += @("-Model", $Model)
        }
        if ($DryRunExternal) {
          $args += @("-DryRun", "-SmokeOnly")
        }
        if ($UseProxy) {
          $args += "-UseProxy"
        }
        Invoke-Native $ps $args
      }
  }
} else {
  Add-SkipStep -Name "coding-agent-benchmark" -Reason "Skipped by default. Use -IncludeExternal to run usamadar/coding-agent-benchmark." -Coverage @("external-coding-benchmark")
}

$branch = (git branch --show-current)
$head = (git rev-parse --short HEAD)
$dirty = (git status --short)

$GeneratedAt = (Get-Date).ToUniversalTime().ToString("o")
$global:Error.Clear()
$StepSummaries = @(
  foreach ($r in $Results) {
    [ordered]@{
      name = [string]$r.name
      status = [string]$r.status
      required = [bool]$r.required
      duration_seconds = [double]$r.duration_seconds
      coverage = [string[]]@($r.coverage)
      command = [string]$r.command
      log = [string]$r.log
      reason = [string]$r.reason
    }
  }
)
$CoverageSummaries = @(
  foreach ($c in $CoverageMatrix) {
    [ordered]@{
      capability = [string]$c.capability
      evidence = [string[]]@($c.evidence)
      notes = [string]$c.notes
    }
  }
)
$Summary = [ordered]@{
  generated_at = [string]$GeneratedAt
  repo_root = [string]$RepoRoot
  git_branch = [string]$branch
  git_head = [string]$head
  git_dirty = [string[]]@($dirty)
  failed = [bool]$HadFailure
  go = [string]$GoExe
  report_markdown = [string]$SummaryMd
  steps = $StepSummaries
  coverage_matrix = $CoverageSummaries
}

$jsonSummary = $Summary | ConvertTo-Json -Depth 8
if ([string]::IsNullOrWhiteSpace($jsonSummary)) {
  throw "failed to render JSON regression summary"
}
[System.IO.File]::WriteAllText($SummaryJson, $jsonSummary + [Environment]::NewLine, [System.Text.UTF8Encoding]::new($false))

$md = New-Object System.Collections.Generic.List[string]
$md.Add("# Maddog Regression Report") | Out-Null
$md.Add("") | Out-Null
$md.Add("- Generated: $GeneratedAt") | Out-Null
$md.Add("- Branch: $branch") | Out-Null
$md.Add("- Head: $head") | Out-Null
$md.Add("- Failed: $HadFailure") | Out-Null
$md.Add("") | Out-Null
$md.Add("## Steps") | Out-Null
$md.Add("") | Out-Null
$md.Add("| Step | Status | Required | Duration | Coverage | Log |") | Out-Null
$md.Add("|---|---:|---:|---:|---|---|") | Out-Null
foreach ($r in $Results) {
  $relLog = ""
  if ($r.log -ne "") {
    try {
      $relLog = Resolve-Path -Relative $r.log -ErrorAction Stop
    } catch {
      $relLog = "$($r.log)"
    }
  }
  $md.Add("| $($r.name) | $($r.status) | $($r.required) | $($r.duration_seconds)s | $($r.coverage -join ', ') | $relLog |") | Out-Null
}
$md.Add("") | Out-Null
$md.Add("## Coverage Matrix") | Out-Null
$md.Add("") | Out-Null
$md.Add("| Capability | Evidence | Notes |") | Out-Null
$md.Add("|---|---|---|") | Out-Null
foreach ($c in $CoverageMatrix) {
  $md.Add("| $($c.capability) | $($c.evidence -join ', ') | $($c.notes) |") | Out-Null
}
$md.Add("") | Out-Null
$md.Add("JSON summary: $SummaryJson") | Out-Null
$md | Set-Content -Encoding UTF8 $SummaryMd

Write-Host "Wrote $SummaryJson"
Write-Host "Wrote $SummaryMd"

if ($HadFailure) {
  exit 1
}
