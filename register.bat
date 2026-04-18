@echo off
powershell -ExecutionPolicy Bypass -NoProfile -Command "& { $f='%~f0'; $content = [System.IO.File]::ReadAllText($f); $ps = $content.Substring($content.LastIndexOf('##PSSTART##') + 11); $tmp = [System.IO.Path]::GetTempFileName() + '.ps1'; [System.IO.File]::WriteAllText($tmp, $ps); try { & $tmp } finally { Remove-Item $tmp -ErrorAction SilentlyContinue } }"
pause
exit /b
##PSSTART##
$ErrorActionPreference = 'Stop'

Write-Host ""
Write-Host "=== Inhouse League Registration ===" -ForegroundColor Cyan
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

# Read VDF with explicit encoding handling (Steam writes UTF-16 LE on Windows)
$rawBytes = [System.IO.File]::ReadAllBytes($vdfPath)
if ($rawBytes.Length -ge 2 -and $rawBytes[0] -eq 0xFF -and $rawBytes[1] -eq 0xFE) {
    $vdf = [System.Text.Encoding]::Unicode.GetString($rawBytes, 2, $rawBytes.Length - 2)
} elseif ($rawBytes.Length -ge 3 -and $rawBytes[0] -eq 0xEF -and $rawBytes[1] -eq 0xBB -and $rawBytes[2] -eq 0xBF) {
    $vdf = [System.Text.Encoding]::UTF8.GetString($rawBytes, 3, $rawBytes.Length - 3)
} else {
    $vdf = [System.Text.Encoding]::UTF8.GetString($rawBytes)
}
$accounts = @()
$ids      = @([regex]::Matches($vdf, '"(\d{17})"')      | ForEach-Object { $_.Groups[1].Value })
$names    = @([regex]::Matches($vdf, '"PersonaName"\s+"([^"]+)"') | ForEach-Object { $_.Groups[1].Value })

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

$displayName = $chosen.Name
Write-Host "Using Steam name: $displayName"

# Register with backend
$apiBase = "https://inhousee4-production.up.railway.app"
Write-Host ""
Write-Host "Registering..." -ForegroundColor Yellow
try {
    $body      = @{ steam_id = $chosen.SteamID; display_name = $displayName } | ConvertTo-Json
    $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes($body)
    $response  = Invoke-RestMethod -Uri "$apiBase/api/register" -Method Post -Body $bodyBytes -ContentType "application/json"
    $token    = $response.token
} catch {
    $status = $_.Exception.Response.StatusCode.value__
    if ($status -eq 409) {
        Write-Host "Already registered - updating GSI config..." -ForegroundColor Yellow
    } else {
        Write-Host "ERROR: Registration failed ($($_.Exception.Message))" -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
}

# Write GSI config (always - covers fresh registration and already-registered players).
# Dota may be installed in a secondary Steam library (different drive) - parse
# libraryfolders.vdf to find the library that actually owns app 570 (Dota 2).
$dotaLibraryPath = $null
$libVdfPath = Join-Path $steamPath 'config\libraryfolders.vdf'
if (Test-Path $libVdfPath) {
    $libVdfText = [System.IO.File]::ReadAllText($libVdfPath)
    # Match each numbered library block.
    $blocks = [regex]::Matches($libVdfText, '"\d+"\s*\{((?:[^{}]|\{[^{}]*\})*)\}')
    foreach ($m in $blocks) {
        $block = $m.Groups[1].Value
        if ($block -match '"570"') {
            if ($block -match '"path"\s*"([^"]+)"') {
                # Path is stored with escaped backslashes - unescape.
                $dotaLibraryPath = $matches[1] -replace '\\\\', '\'
                break
            }
        }
    }
}
if (-not $dotaLibraryPath) {
    Write-Host "WARNING: Could not locate Dota 2 in libraryfolders.vdf - falling back to default Steam path." -ForegroundColor Yellow
    $dotaLibraryPath = $steamPath
}
Write-Host "Dota 2 library: $dotaLibraryPath"
$dotaGsiDir = Join-Path $dotaLibraryPath "steamapps\common\dota 2 beta\game\dota\cfg\gamestate_integration"
if (-not (Test-Path $dotaGsiDir)) {
    New-Item -ItemType Directory -Path $dotaGsiDir -Force | Out-Null
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
        "token"  "$($chosen.SteamID)"
    }
    "data"
    {
        "map"    "1"
        "player" "1"
        "hero"   "1"
        "draft"  "1"
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
Write-Host "Add the league bot as a Steam friend so it can send you lobby invites." -ForegroundColor Cyan
Write-Host "Click 'Add as Friend' and the bot will accept automatically." -ForegroundColor Cyan
Write-Host "--------------------------------------------------------------" -ForegroundColor DarkGray
Write-Host ""
$open = Read-Host "Open bot Steam profile in browser? (y/n)"
if ($open -eq 'y' -or $open -eq 'Y') {
    Start-Process "https://steamcommunity.com/profiles/76561198719296562"
}
Write-Host ""
Write-Host "Done! You're all set." -ForegroundColor Green
Write-Host ""
