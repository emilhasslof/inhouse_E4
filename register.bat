@echo off
powershell -ExecutionPolicy Bypass -NoProfile -Command "& { $f='%~f0'; $content = [System.IO.File]::ReadAllText($f); $ps = $content.Substring($content.LastIndexOf('##PSSTART##') + 11); $tmp = [System.IO.Path]::GetTempFileName() + '.ps1'; [System.IO.File]::WriteAllText($tmp, $ps); try { & $tmp } finally { Remove-Item $tmp -ErrorAction SilentlyContinue } }"
pause
exit /b
##PSSTART##
$ErrorActionPreference = 'Stop'

Write-Host ""
Write-Host "=== Inhouse League Registration ===" -ForegroundColor Cyan
Write-Host ""
Read-Host "Press Enter to start"
Write-Host ""

# Find Steam path from registry
try {
    $steamPath = (Get-ItemProperty 'HKCU:\Software\Valve\Steam' -Name 'SteamPath').SteamPath
} catch {
    Write-Host "ERROR: Steam not found. Is Steam installed?" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# Parse loginusers.vdf to find Steam accounts
$vdfPath = Join-Path $steamPath 'config\loginusers.vdf'
if (-not (Test-Path $vdfPath)) {
    Write-Host "ERROR: Could not find Steam login data. Have you logged in to Steam?" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

$vdf      = Get-Content $vdfPath -Raw
$accounts = @()
$ids      = [regex]::Matches($vdf, '"(\d{17})"')      | ForEach-Object { $_.Groups[1].Value }
$names    = [regex]::Matches($vdf, '"PersonaName"\s+"([^"]+)"') | ForEach-Object { $_.Groups[1].Value }

for ($i = 0; $i -lt $ids.Count; $i++) {
    $accounts += [PSCustomObject]@{
        SteamID = $ids[$i]
        Name    = if ($i -lt $names.Count) { $names[$i] } else { "Unknown" }
    }
}

if ($accounts.Count -eq 0) {
    Write-Host "ERROR: No Steam accounts found. Have you logged in to Steam?" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# Pick account
if ($accounts.Count -eq 1) {
    $chosen = $accounts[0]
    Write-Host "Found Steam account: $($chosen.Name) ($($chosen.SteamID))"
} else {
    Write-Host "Multiple Steam accounts found:"
    for ($i = 0; $i -lt $accounts.Count; $i++) {
        Write-Host "  [$($i+1)] $($accounts[$i].Name) ($($accounts[$i].SteamID))"
    }
    $pick = Read-Host "Enter the number of your account"
    $idx  = [int]$pick - 1
    if ($idx -lt 0 -or $idx -ge $accounts.Count) {
        Write-Host "Invalid selection." -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
    $chosen = $accounts[$idx]
}

Write-Host ""
$displayName = Read-Host "Enter your display name (shown on the leaderboard)"
if (-not $displayName) {
    Write-Host "Display name cannot be empty." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# Register with backend
$apiBase = "https://inhousee4-production.up.railway.app"
Write-Host ""
Write-Host "Registering..." -ForegroundColor Yellow
try {
    $body     = @{ steam_id = $chosen.SteamID; display_name = $displayName } | ConvertTo-Json
    $response = Invoke-RestMethod -Uri "$apiBase/api/register" -Method Post -Body $body -ContentType "application/json"
    $token    = $response.token
} catch {
    $status = $_.Exception.Response.StatusCode.value__
    if ($status -eq 409) {
        Write-Host "You are already registered! No need to run this again." -ForegroundColor Yellow
    } else {
        Write-Host "ERROR: Registration failed ($($_.Exception.Message))" -ForegroundColor Red
    }
    Read-Host "Press Enter to exit"
    exit 1
}

# Write GSI config
$dotaGsiDir = Join-Path $steamPath "steamapps\common\dota 2 beta\game\dota\cfg\gamestate_integration"
if (-not (Test-Path $dotaGsiDir)) {
    New-Item -ItemType Directory -Path $dotaGsiDir | Out-Null
}

$gsiConfig = @"
"inhouse"
{
    "uri"        "https://inhousee4-production.up.railway.app/gsi"
    "timeout"    "5.0"
    "buffer"     "0.1"
    "throttle"   "1.0"
    "heartbeat"  "30.0"
    "auth"
    {
        "token"  "$token"
    }
    "data"
    {
        "map"    "1"
        "player" "1"
        "hero"   "1"
    }
}
"@

$gsiPath = Join-Path $dotaGsiDir "gamestate_integration_inhouse.cfg"
Set-Content -Path $gsiPath -Value $gsiConfig -Encoding UTF8

Write-Host ""
Write-Host "All done! You are registered." -ForegroundColor Green
Write-Host "GSI config written to: $gsiPath"
Write-Host "Launch Dota 2 and your stats will be tracked automatically."
Write-Host ""
Write-Host "--------------------------------------------------------------" -ForegroundColor DarkGray
Write-Host "One last step: add the league bot as a Steam friend." -ForegroundColor Cyan
Write-Host "This lets the bot send you lobby invites when a match is ready." -ForegroundColor Cyan
Write-Host ""
Write-Host "Your browser will open the bot's Steam profile." -ForegroundColor Cyan
Write-Host "Click 'Add as Friend' and the bot will accept automatically." -ForegroundColor Cyan
Write-Host "--------------------------------------------------------------" -ForegroundColor DarkGray
Write-Host ""
Read-Host "Press Enter to open the Steam profile in your browser"
Start-Process "https://steamcommunity.com/profiles/76561198719296562"
Write-Host ""
Write-Host "Done! You're all set." -ForegroundColor Green
Write-Host ""
