param([string]$GodotPath = 'godot', [switch]$LoadTest)
$ErrorActionPreference = 'Stop'
function CheckExit([string]$Name) { if ($LASTEXITCODE -ne 0) { throw "$Name failed with exit $LASTEXITCODE" } }
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
  docker run --rm -v "${PWD}/backend:/src" -w /src golang:1.27.3 sh -c 'go test -race ./... && go vet ./... && go build ./...'
  CheckExit 'Go checks'
  foreach ($Frontend in @('admin/web', 'player/web')) {
    Push-Location $Frontend
    try {
      npm.cmd ci
      CheckExit "$Frontend dependencies"
      npm.cmd audit --audit-level=high
      CheckExit "$Frontend dependency audit"
      if ($Frontend -eq 'player/web') {
        npm.cmd test
        CheckExit 'Player tests'
        node --test ../../scripts/package-images.test.mjs
        CheckExit 'Image delivery tests'
      }
      npm.cmd run build
      CheckExit "$Frontend build"
    } finally { Pop-Location }
  }
  docker compose -f deploy/compose.yaml --profile rules --profile gm --profile player config --quiet
  CheckExit 'Compose configuration'
  docker compose -f deploy/compose.yaml --profile rules --profile gm --profile player up -d --build --wait --wait-timeout 180
  CheckExit 'Compose'
  node scripts/integration.mjs
  CheckExit 'Sandbox integration'
  if ($LoadTest) {
    node tests/load.mjs
    CheckExit 'Local load check'
  }
  & $GodotPath --headless --path sdk/godot --editor --import --quit
  CheckExit 'Godot import'
  & $GodotPath --headless --path sdk/godot --script test_sdk.gd
  CheckExit 'Godot SDK'
  & $GodotPath --headless --path sdk/godot --script test_saves.gd
  CheckExit 'Godot save conflict'
  foreach ($Script in @('test_local_bridge', 'test_mail_inventory', 'test_platform')) {
    & $GodotPath --headless --path sdk/godot --script "$Script.gd"
    CheckExit $Script
  }
  Write-Output 'All requested verification commands passed.'
} finally { Pop-Location }
