$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$bootstrapRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("verify-bootstrap-" + [guid]::NewGuid().ToString("N"))
$exitCode = 1

New-Item -ItemType Directory -Path $bootstrapRoot | Out-Null
try {
    $env:GOCACHE = Join-Path $bootstrapRoot "go-cache"
    Push-Location $repoRoot
    try {
        & go run ./tools/verify @args
        $exitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}
finally {
    if (Test-Path -LiteralPath $bootstrapRoot) {
        Get-ChildItem -LiteralPath $bootstrapRoot -Force -Recurse | ForEach-Object {
            $_.Attributes = "Normal"
        }
        Remove-Item -LiteralPath $bootstrapRoot -Recurse -Force
    }
}

exit $exitCode
