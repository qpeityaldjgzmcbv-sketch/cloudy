param([string]$Go = "go", [string]$OriginalExe = "D:\Program Files\Cloudy\Cloudy.exe", [string]$Python = "python")
$ErrorActionPreference = "Stop"
$resourceCreated = $false
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    $env:CGO_ENABLED = "0"
    & $Go test ./internal/controlbridge ./internal/player
    if ($LASTEXITCODE -ne 0) { throw "Cloudy tests failed" }
    & $Go vet ./internal/controlbridge ./internal/app
    if ($LASTEXITCODE -ne 0) { throw "Cloudy vet failed" }
    $flags = "-H=windowsgui -X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageName=Cloudy -X github.com/egoist/mygo.packageVersion=1.2.12-shelf -X github.com/egoist/mygo.packageIdentifier=io.github.nya-owo.cloud-nya"
    # Use the original executable's Windows resources; plain go build omits
    # its manifest, application metadata and icon. Keep a safe backup of this
    # generated resource after building, without touching an existing .syso.
    $resource = Join-Path (Get-Location) 'shelf_resources_windows_amd64.syso'
    & $Python scripts/windows_resources.py $OriginalExe $resource
    if ($LASTEXITCODE -ne 0) { throw "Cloudy Windows resources failed" }
    $resourceCreated = $true
    & $Go build -trimpath -tags mygo_noinspector -ldflags $flags -o build/Cloudy-Shelf.exe .
    if ($LASTEXITCODE -ne 0) { throw "Cloudy build failed" }
    $launcher = '@echo off' + "`r`n" + 'setlocal' + "`r`n" + 'set "CLOUDY_BRIDGE_FILE=%~dp0cloudy-bridge.json"' + "`r`n" + 'start "" "%~dp0Cloudy-Shelf.exe"' + "`r`n"
    [System.IO.File]::WriteAllText((Join-Path (Get-Location) 'build/Start-Shelf.cmd'), $launcher, [System.Text.Encoding]::ASCII)
} finally {
    if ($resourceCreated -and (Test-Path -LiteralPath $resource)) {
        $archive = Join-Path (Get-Location) ('build/shelf-resources-' + [Guid]::NewGuid().ToString('N') + '.coff')
        Move-Item -LiteralPath $resource -Destination $archive
    }
    Pop-Location
}
