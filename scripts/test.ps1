[CmdletBinding()]
param(
    [switch]$Quick,
    [switch]$Integration
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$goCacheDirectory = Join-Path $repositoryRoot ".codex-go-cache"
$artifactDirectory = Join-Path $repositoryRoot ".artifacts\test"
$coverageProfile = Join-Path $artifactDirectory "coverage.out"
$coverageSummary = Join-Path $artifactDirectory "coverage.txt"
$previousGoCache = $env:GOCACHE
$previousOpenRouterKey = $env:OPENROUTER_API_KEY

function Invoke-GoCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Falha ao executar: go $($Arguments -join ' ')"
    }
}

Push-Location $repositoryRoot
try {
    if ($Quick -and $Integration) { throw "Use -Quick ou -Integration, nao ambos." }
    New-Item -ItemType Directory -Force -Path $goCacheDirectory | Out-Null
    $env:GOCACHE = $goCacheDirectory
    # Harness never inherits the user's production inference credential.
    # Provider calls in tests must be intercepted by the local test transport.
    $env:OPENROUTER_API_KEY = "harness-no-real-inference"

    if ($Quick) {
        Write-Host "[test] Executando suite rapida..."
        Invoke-GoCommand -Arguments @("test", "./...", "-count=1", "-timeout=2m")
        Write-Host "[test] Suite rapida concluida."
        return
    }

    Write-Host "[test] Validando formatacao..."
    $unformattedFiles = @(& gofmt -l .)
    if ($LASTEXITCODE -ne 0) {
        throw "Falha ao executar gofmt."
    }
    if ($unformattedFiles.Count -gt 0) {
        throw "Arquivos Go sem formatacao:`n$($unformattedFiles -join [Environment]::NewLine)"
    }

    Write-Host "[test] Executando go vet..."
    Invoke-GoCommand -Arguments @("vet", "./...")

    Write-Host "[test] Compilando todos os comandos..."
    Invoke-GoCommand -Arguments @("build", "./...")

    New-Item -ItemType Directory -Force -Path $artifactDirectory | Out-Null

    Write-Host "[test] Executando testes com cobertura..."
    Invoke-GoCommand -Arguments @(
        "test",
        "./...",
        "-count=1",
        "-shuffle=on",
        "-timeout=2m",
        "-covermode=atomic",
        "-coverprofile=$coverageProfile"
    )

    Write-Host "[test] Gerando resumo de cobertura..."
    & go tool cover "-func=$coverageProfile" | Set-Content -Encoding utf8 $coverageSummary
    if ($LASTEXITCODE -ne 0) {
        throw "Falha ao gerar o resumo de cobertura."
    }

    Get-Content $coverageSummary | Select-Object -Last 1
    if ($Integration) {
        Invoke-GoCommand -Arguments @("test", "-tags=integration", "./cmd/api", "-count=1", "-timeout=5m", "-coverpkg=./internal/...,./cmd/api", "-coverprofile=$artifactDirectory/integration.out")
    }
    Write-Host "[test] Harness completo concluido. Artefatos em $artifactDirectory"
}
finally {
    $env:GOCACHE = $previousGoCache
    $env:OPENROUTER_API_KEY = $previousOpenRouterKey
    Pop-Location
}
