param(
  [string]$GoExe = "",
  [string]$Model = "",
  [switch]$IncludeE2E,
  [string]$E2ETasks = "",
  [string]$E2ETags = "",
  [int]$E2EBudget = 400000,
  [switch]$IncludeFrontierSmoke,
  [string]$FrontierOpenAIModel = "gpt-5.5",
  [string]$FrontierAnthropicModel = "claude-sonnet-4-6",
  [switch]$IncludeOfficialAuthSmoke,
  [string]$OfficialOpenAIModel = "gpt-4.1-mini",
  [string]$OfficialAnthropicModel = "claude-sonnet-4-6",
  [switch]$IncludeExternal,
  [switch]$DryRunExternal,
  [string]$BenchmarkDir = "C:\Dev2\research\coding-agent-benchmark",
  [switch]$UseProxy,
  [switch]$SkipFrontend,
  [switch]$SkipFrontendBuild,
  [switch]$AuditOnly,
  [switch]$RequireComplete
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

if ($AuditOnly) {
  if (!(Test-Path $SummaryJson)) {
    Write-Error "No regression summary found at $SummaryJson. Run scripts/run-maddog-regression.ps1 first."
    exit 1
  }
  $latest = Get-Content -Raw $SummaryJson | ConvertFrom-Json
  $audit = $latest.completion_audit
  if ($null -eq $audit) {
    Write-Error "Regression summary does not contain completion_audit: $SummaryJson"
    exit 1
  }
  Write-Host "Completion audit: complete=$($audit.complete)"
  if ($audit.failed_required_steps.Count -gt 0) {
    foreach ($s in $audit.failed_required_steps) {
      Write-Host "Failed required step: $($s.name) ($($s.status)) $($s.reason)"
    }
  } else {
    Write-Host "Failed required steps: none"
  }
  if ($audit.pending.Count -gt 0) {
    foreach ($p in $audit.pending) {
      Write-Host "Pending: $($p.capability) - $($p.remaining -join '; ')"
    }
  } else {
    Write-Host "Pending: none"
  }
  if ($RequireComplete -and -not $audit.complete) {
    exit 1
  }
  exit 0
}

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

function Test-LiveCredential {
  param([string]$Name)
  return -not [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($Name))
}

function Test-StepPassed {
  param([string]$Name)
  return [bool](($Results | Where-Object { $_.name -eq $Name -and $_.status -eq "pass" } | Select-Object -First 1))
}

$LiveCredentialNames = @(
  "DEEPSEEK_API_KEY",
  "ICODEEASY_API_KEY",
  "OPENAI_API_KEY",
  "ANTHROPIC_API_KEY",
  "OPENAI_OFFICIAL_TOKEN",
  "ANTHROPIC_IDENTITY_TOKEN"
)
$LiveCredentials = @(
  foreach ($name in $LiveCredentialNames) {
    [pscustomobject]@{
      name = $name
      set = [bool](Test-LiveCredential $name)
    }
  }
)
$AnyProviderCredential = [bool](($LiveCredentials | Where-Object { $_.set -and $_.name -in @("DEEPSEEK_API_KEY", "ICODEEASY_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY") } | Select-Object -First 1))
$AnyFrontierCredential = [bool](($LiveCredentials | Where-Object { $_.set -and $_.name -in @("ICODEEASY_API_KEY", "OPENAI_API_KEY") } | Select-Object -First 1))
$AnthropicLiveReady = [bool](($LiveCredentials | Where-Object { $_.name -eq "ANTHROPIC_API_KEY" -and $_.set } | Select-Object -First 1))
$FrontierSmokeReady = [bool]($AnyFrontierCredential -and $AnthropicLiveReady)
$OpenAIOfficialReady = [bool](($LiveCredentials | Where-Object { $_.name -eq "OPENAI_OFFICIAL_TOKEN" -and $_.set } | Select-Object -First 1))
$AnthropicOfficialReady = [bool](($LiveCredentials | Where-Object { $_.name -eq "ANTHROPIC_IDENTITY_TOKEN" -and $_.set } | Select-Object -First 1))
$OfficialAuthReady = [bool]($OpenAIOfficialReady -and $AnthropicOfficialReady)
$LiveReadiness = [ordered]@{
  provider_e2e_ready = [bool]($AnyProviderCredential -and $OfficialAuthReady)
  provider_api_key_e2e_ready = $AnyProviderCredential
  official_auth_e2e_ready = $OfficialAuthReady
  frontier_smoke_ready = $FrontierSmokeReady
  credentials = @(
    foreach ($c in $LiveCredentials) {
      [ordered]@{
        name = [string]$c.name
        set = [bool]$c.set
      }
    }
  )
  commands = @(
    "powershell -ExecutionPolicy Bypass -File scripts/run-maddog-regression.ps1 -IncludeE2E",
    "powershell -ExecutionPolicy Bypass -File scripts/run-maddog-regression.ps1 -IncludeOfficialAuthSmoke",
    "powershell -ExecutionPolicy Bypass -File scripts/run-maddog-regression.ps1 -IncludeFrontierSmoke",
    "powershell -ExecutionPolicy Bypass -File scripts/run-maddog-regression.ps1 -IncludeE2E -IncludeOfficialAuthSmoke -IncludeFrontierSmoke -IncludeExternal",
    "Set OPENAI_OFFICIAL_TOKEN and ANTHROPIC_IDENTITY_TOKEN before claiming official auth live coverage"
  )
}

$CoverageMatrix = @(
  [pscustomobject]@{
    capability = "Provider API-key routing, official auth config, and OpenAI/Anthropic/iCodeEasy compatibility"
    evidence = @("core-go", "manifest", "local-provider-e2e", "provider-auth-frontier-profile")
    notes = "Covers API-key and official auth config shapes plus local OpenAI bearer and Anthropic workload-identity exchange paths; real official OAuth/browser flows still require manual/provider credential validation."
    status = "partial-live-pending"
    remaining = @(
      if (-not $LiveReadiness.provider_api_key_e2e_ready) { "Run real-provider Maddog e2e with at least one API-key provider credential." }
      if (-not $LiveReadiness.official_auth_e2e_ready) { "Set OPENAI_OFFICIAL_TOKEN and ANTHROPIC_IDENTITY_TOKEN, then run -IncludeOfficialAuthSmoke." }
      elseif (-not $IncludeOfficialAuthSmoke) { "Run official OpenAI bearer and Anthropic workload-identity live auth checks with -IncludeOfficialAuthSmoke." }
    )
    optional_remaining = @()
  },
  [pscustomobject]@{
    capability = "Frontier/small-model routing, budgets, advisor escalation, and cost wrappers"
    evidence = @("core-go", "local-provider-e2e", "e2e optional", "frontier smoke optional")
    notes = "The local fixture requires a small-model failure path to upgrade to frontier and record upgrade metrics; live frontier calls are skipped unless -IncludeFrontierSmoke is used and credentials are present."
    status = if ($LiveReadiness.frontier_smoke_ready) { "verified" } else { "partial-live-pending" }
    remaining = if ($LiveReadiness.frontier_smoke_ready) { @() } else { @("Run live frontier/advisor/scorer smoke with ICODEEASY_API_KEY or OPENAI_API_KEY plus ANTHROPIC_API_KEY.") }
    optional_remaining = @()
  },
  [pscustomobject]@{
    capability = "Anthropic native advisor tool and desktop advisor event presentation"
    evidence = @("core-go", "desktop-go", "frontend")
    notes = "Native advisor is unit-tested; provider-side beta behavior requires live Anthropic credentials."
    status = if ($AnthropicLiveReady) { "verified" } else { "partial-live-pending" }
    remaining = if ($AnthropicLiveReady) { @() } else { @("Run live Anthropic advisor/provider smoke with ANTHROPIC_API_KEY.") }
    optional_remaining = @()
  },
  [pscustomobject]@{
    capability = "Dynamic skills, project skill invocation, and subagent delegation"
    evidence = @("core-go", "manifest", "e2e optional")
    notes = "External coding benchmark does not inspect skill/advisor events."
    status = "verified-offline"
    remaining = @()
    optional_remaining = @()
  },
  [pscustomobject]@{
    capability = "C2 offline replay, scorer, guardrail, and skill promotion"
    evidence = @("core-go: internal/eval")
    notes = "Offline mechanics are local/unit verified; live frontier scoring requires optional provider runs."
    status = if ($LiveReadiness.frontier_smoke_ready) { "verified" } else { "partial-live-pending" }
    remaining = if ($LiveReadiness.frontier_smoke_ready) { @() } else { @("Run live frontier scoring path with frontier provider credentials.") }
    optional_remaining = @()
  },
  [pscustomobject]@{
    capability = "Readiness evidence gate, tool metrics, tinyctx/compaction, and run metrics"
    evidence = @("core-go", "manifest", "local-provider-e2e", "e2e optional")
    notes = "The local OpenAI-compatible and Anthropic-native fixtures are required and record provider/tool-loop metrics without live credentials; real provider e2e remains optional."
    status = "verified-offline"
    remaining = @()
    optional_remaining = @("Run real provider e2e to compare live provider metrics against local fixture metrics.")
  },
  [pscustomobject]@{
    capability = "Maddog naming, config/storage isolation, desktop GUI settings, and app build"
    evidence = @("core-go", "desktop-go", "frontend", "manifest")
    notes = "Desktop Go tests pin maddog-dev native binary naming, Maddog storage roots, release packaging names, settings wiring, signing, and updater helpers; packaged installer runtime smoke remains a native Windows release check."
    status = "verified-offline"
    remaining = @()
    optional_remaining = @("Run packaged Windows installer/runtime smoke on a signed release build.")
  },
  [pscustomobject]@{
    capability = "General coding-agent task performance"
    evidence = @("external coding-agent-benchmark optional", "local external benchmark smoke")
    notes = "-IncludeExternal defaults to the offline -LocalSmoke path so the external harness invokes Maddog without live credentials; run scripts/run-coding-agent-benchmark.ps1 without -LocalSmoke for full live performance comparisons."
    status = "verified-offline"
    remaining = @()
    optional_remaining = @("Run full live external benchmark for cross-agent performance comparisons when provider credentials and task toolchains are available.")
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

Invoke-Step `
  -Name "local-provider-e2e" `
  -Command "$GoExe run ./cmd/e2ebench -mode local-fixture -bin $MaddogBin -out .benchmark/regression/local-provider.md -json .benchmark/regression/local-provider.json" `
  -Coverage @("local-fixture", "openai-compatible-sse", "anthropic-native-sse", "official-auth", "frontier", "frontier-upgrade", "small-model", "tool-loop", "mechanism-metrics", "headless-cli") `
  -Required $true `
  -Action {
    Invoke-Native $GoExe @("run", "./cmd/e2ebench", "-mode", "local-fixture", "-bin", $MaddogBin, "-out", ".benchmark/regression/local-provider.md", "-json", ".benchmark/regression/local-provider.json")
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
  $e2eArgs = @("run", "./cmd/e2ebench", "-bin", $MaddogBin, "-budget", "$E2EBudget", "-exclude-tags", "local-fixture", "-out", ".benchmark/regression/e2e.md", "-json", ".benchmark/regression/e2e.json")
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
  if (!$AnyFrontierCredential -or !$AnthropicLiveReady) {
    Add-SkipStep -Name "frontier-smoke" -Reason "ICODEEASY_API_KEY or OPENAI_API_KEY plus ANTHROPIC_API_KEY are required for live frontier/advisor/scorer validation." -Coverage @("frontier-real-call", "anthropic-advisor-live", "frontier-scorer-live")
  } else {
    Invoke-Step `
      -Name "frontier-smoke" `
      -Command "$GoExe run ./cmd/e2ebench -mode frontier-smoke -openai-model $FrontierOpenAIModel -anthropic-model $FrontierAnthropicModel -out .benchmark/regression/frontier.md -json .benchmark/regression/frontier.json" `
      -Coverage @("frontier-real-call", "frontier-costwrap-live", "frontier-scorer-live", "anthropic-advisor-live") `
      -Required $true `
      -Action {
        Invoke-Native $GoExe @(
          "run", "./cmd/e2ebench",
          "-mode", "frontier-smoke",
          "-openai-model", $FrontierOpenAIModel,
          "-anthropic-model", $FrontierAnthropicModel,
          "-out", ".benchmark/regression/frontier.md",
          "-json", ".benchmark/regression/frontier.json"
        )
      }
  }
} else {
  Add-SkipStep -Name "frontier-smoke" -Reason "Skipped by default. Use -IncludeFrontierSmoke with provider credentials for live frontier/advisor/scorer validation." -Coverage @("frontier-real-call")
}

if ($IncludeOfficialAuthSmoke) {
  if (!$OfficialAuthReady) {
    Add-SkipStep -Name "official-auth-smoke" -Reason "OPENAI_OFFICIAL_TOKEN and ANTHROPIC_IDENTITY_TOKEN must both be set." -Coverage @("official-auth-live", "openai-bearer", "anthropic-workload-identity")
  } else {
    Invoke-Step `
      -Name "official-auth-smoke" `
      -Command "$GoExe run ./cmd/e2ebench -mode official-auth-smoke -openai-model $OfficialOpenAIModel -anthropic-model $OfficialAnthropicModel -out .benchmark/regression/official-auth.md -json .benchmark/regression/official-auth.json" `
      -Coverage @("official-auth-live", "openai-bearer", "anthropic-workload-identity") `
      -Required $true `
      -Action {
        Invoke-Native $GoExe @(
          "run", "./cmd/e2ebench",
          "-mode", "official-auth-smoke",
          "-openai-model", $OfficialOpenAIModel,
          "-anthropic-model", $OfficialAnthropicModel,
          "-out", ".benchmark/regression/official-auth.md",
          "-json", ".benchmark/regression/official-auth.json"
        )
      }
  }
} else {
  Add-SkipStep -Name "official-auth-smoke" -Reason "Skipped by default. Use -IncludeOfficialAuthSmoke with OPENAI_OFFICIAL_TOKEN and ANTHROPIC_IDENTITY_TOKEN for live official auth validation." -Coverage @("official-auth-live")
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
      -Command "powershell -ExecutionPolicy Bypass -File scripts/run-coding-agent-benchmark.ps1 -BenchmarkDir $BenchmarkDir -LocalSmoke" `
      -Coverage @("external-coding-benchmark", "agent-command-adapter", "local-provider-fixture") `
      -Required $true `
      -Action {
        $args = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", (Join-Path $RepoRoot "scripts\run-coding-agent-benchmark.ps1"), "-BenchmarkDir", $BenchmarkDir, "-GoExe", $GoExe)
        if ($Model -ne "") {
          $args += @("-Model", $Model)
        }
        if ($DryRunExternal) {
          $args += @("-DryRun", "-SmokeOnly")
        } else {
          $args += "-LocalSmoke"
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

$ProviderAPIKeyE2EVerified = [bool](Test-StepPassed "maddog-e2e")
$OfficialAuthLiveVerified = [bool](Test-StepPassed "official-auth-smoke")
$FrontierSmokeVerified = [bool](Test-StepPassed "frontier-smoke")
$AnthropicAdvisorVerified = [bool]($FrontierSmokeVerified -and $AnthropicLiveReady)
$CoverageMatrix[0].status = if ($ProviderAPIKeyE2EVerified -and $OfficialAuthLiveVerified) { "verified" } else { "partial-live-pending" }
$CoverageMatrix[0].remaining = @(
  if (-not $ProviderAPIKeyE2EVerified) {
    if ($LiveReadiness.provider_api_key_e2e_ready) {
      "Run real-provider Maddog e2e with -IncludeE2E and at least one API-key provider credential."
    } else {
      "Set DEEPSEEK_API_KEY, ICODEEASY_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY, then run -IncludeE2E."
    }
  }
  if (-not $OfficialAuthLiveVerified) {
    if ($LiveReadiness.official_auth_e2e_ready) {
      "Run official OpenAI bearer and Anthropic workload-identity live auth checks with -IncludeOfficialAuthSmoke."
    } else {
      "Set OPENAI_OFFICIAL_TOKEN and ANTHROPIC_IDENTITY_TOKEN, then run -IncludeOfficialAuthSmoke."
    }
  }
)
$CoverageMatrix[1].status = if ($FrontierSmokeVerified) { "verified" } else { "partial-live-pending" }
$CoverageMatrix[1].remaining = if ($FrontierSmokeVerified) { @() } else {
  if ($LiveReadiness.frontier_smoke_ready -and $AnthropicLiveReady) { @("Run live frontier/advisor/scorer smoke with -IncludeFrontierSmoke.") } else { @("Set ICODEEASY_API_KEY or OPENAI_API_KEY plus ANTHROPIC_API_KEY, then run -IncludeFrontierSmoke.") }
}
$CoverageMatrix[2].status = if ($AnthropicAdvisorVerified) { "verified" } else { "partial-live-pending" }
$CoverageMatrix[2].remaining = if ($AnthropicAdvisorVerified) { @() } else {
  if ($AnthropicLiveReady -and $LiveReadiness.frontier_smoke_ready) { @("Run live Anthropic advisor/provider smoke with -IncludeFrontierSmoke.") } else { @("Set ANTHROPIC_API_KEY plus ICODEEASY_API_KEY or OPENAI_API_KEY, then run -IncludeFrontierSmoke.") }
}
$CoverageMatrix[4].status = if ($FrontierSmokeVerified) { "verified" } else { "partial-live-pending" }
$CoverageMatrix[4].remaining = if ($FrontierSmokeVerified) { @() } else {
  if ($LiveReadiness.frontier_smoke_ready) { @("Run live frontier scoring path with -IncludeFrontierSmoke.") } else { @("Set ICODEEASY_API_KEY or OPENAI_API_KEY, then run live frontier scoring path.") }
}

$CoverageSummaries = @(
  foreach ($c in $CoverageMatrix) {
    [ordered]@{
      capability = [string]$c.capability
      evidence = [string[]]@($c.evidence)
      status = [string]$c.status
      remaining = [string[]]@($c.remaining)
      optional_remaining = [string[]]@($c.optional_remaining)
      notes = [string]$c.notes
    }
  }
)
$FailedRequiredSteps = @(
  foreach ($s in $StepSummaries) {
    if ($s.required -and $s.status -ne "pass") {
      [ordered]@{
        name = [string]$s.name
        status = [string]$s.status
        reason = [string]$s.reason
      }
    }
  }
)
$CompletionAudit = [ordered]@{
  complete = [bool](
    -not ($CoverageSummaries | Where-Object { $_.status -eq "partial-live-pending" } | Select-Object -First 1) -and
    -not ($FailedRequiredSteps | Select-Object -First 1)
  )
  failed_required_steps = $FailedRequiredSteps
  pending = @(
    foreach ($c in $CoverageSummaries) {
      if ($c.status -eq "partial-live-pending") {
        [ordered]@{
          capability = [string]$c.capability
          remaining = [string[]]@($c.remaining)
        }
      }
    }
  )
}
$Summary = [ordered]@{
  generated_at = [string]$GeneratedAt
  repo_root = [string]$RepoRoot
  git_branch = [string]$branch
  git_head = [string]$head
  git_dirty = [string[]]@($dirty)
  failed = [bool]$HadFailure
  go = [string]$GoExe
  live_readiness = $LiveReadiness
  completion_audit = $CompletionAudit
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
$md.Add("## Live Readiness") | Out-Null
$md.Add("") | Out-Null
$md.Add("- Provider e2e ready: $($LiveReadiness.provider_e2e_ready)") | Out-Null
$md.Add("- Provider API-key e2e ready: $($LiveReadiness.provider_api_key_e2e_ready)") | Out-Null
$md.Add("- Official auth e2e ready: $($LiveReadiness.official_auth_e2e_ready)") | Out-Null
$md.Add("- Frontier smoke ready: $($LiveReadiness.frontier_smoke_ready)") | Out-Null
$missingLiveCredentials = @($LiveReadiness.credentials | Where-Object { -not $_.set } | ForEach-Object { $_.name })
if ($missingLiveCredentials.Count -gt 0) {
  $md.Add("- Missing credentials: $($missingLiveCredentials -join ', ')") | Out-Null
} else {
  $md.Add("- Missing credentials: none") | Out-Null
}
$md.Add("- Live commands:") | Out-Null
foreach ($cmd in $LiveReadiness.commands) {
  $md.Add("  - ``$cmd``") | Out-Null
}
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
$md.Add("| Capability | Status | Evidence | Required Remaining | Optional Remaining | Notes |") | Out-Null
$md.Add("|---|---:|---|---|---|---|") | Out-Null
foreach ($c in $CoverageMatrix) {
  $remaining = if (@($c.remaining).Count -gt 0) { @($c.remaining) -join '; ' } else { "" }
  $optionalRemaining = if (@($c.optional_remaining).Count -gt 0) { @($c.optional_remaining) -join '; ' } else { "" }
  $md.Add("| $($c.capability) | $($c.status) | $($c.evidence -join ', ') | $remaining | $optionalRemaining | $($c.notes) |") | Out-Null
}
$md.Add("") | Out-Null
$md.Add("## Completion Audit") | Out-Null
$md.Add("") | Out-Null
$md.Add("- Complete: $($CompletionAudit.complete)") | Out-Null
if ($CompletionAudit.failed_required_steps.Count -gt 0) {
  foreach ($s in $CompletionAudit.failed_required_steps) {
    $md.Add("- Failed required step: $($s.name) ($($s.status)) $($s.reason)") | Out-Null
  }
} else {
  $md.Add("- Failed required steps: none") | Out-Null
}
if ($CompletionAudit.pending.Count -gt 0) {
  foreach ($p in $CompletionAudit.pending) {
    $md.Add("- Pending: $($p.capability) - $($p.remaining -join '; ')") | Out-Null
  }
} else {
  $md.Add("- Pending: none") | Out-Null
}
$md.Add("") | Out-Null
$md.Add("JSON summary: $SummaryJson") | Out-Null
$md | Set-Content -Encoding UTF8 $SummaryMd

Write-Host "Wrote $SummaryJson"
Write-Host "Wrote $SummaryMd"

if ($HadFailure) {
  exit 1
}
if ($RequireComplete -and -not $CompletionAudit.complete) {
  Write-Error "Completion audit is not complete. See $SummaryMd and $SummaryJson."
  exit 1
}
