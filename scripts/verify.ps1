param([string]$GodotPath = 'godot')
$ErrorActionPreference = 'Stop'
function CheckExit([string]$Name) { if ($LASTEXITCODE -ne 0) { throw "$Name failed with exit $LASTEXITCODE" } }
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
  docker run --rm -v "${PWD}/backend:/src" -w /src golang:1.27.3 sh -c 'go test -race ./... && go vet ./... && go build ./...'
  CheckExit 'Go checks'
  docker compose -f deploy/compose.yaml --profile rules --profile gm up -d --build
  CheckExit 'Compose'
  node tests/e2e.mjs
  CheckExit 'HTTP milestones'
  node tests/migrations.mjs
  CheckExit 'Migrations'
  node tests/production-boundary.mjs
  CheckExit 'Production boundary'
  node tests/recovery.mjs
  CheckExit 'Recovery'
  node tests/load.mjs
  CheckExit 'Local load check'
  & $GodotPath --headless --path sdk/godot --editor --import --quit
  CheckExit 'Godot import'
  & $GodotPath --headless --path sdk/godot --script test_sdk.gd
  CheckExit 'Godot SDK'
  & $GodotPath --headless --path sdk/godot --script test_saves.gd
  CheckExit 'Godot save conflict'
  Write-Output 'All requested verification commands passed.'
} finally { Pop-Location }
