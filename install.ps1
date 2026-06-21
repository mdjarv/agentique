<#
.SYNOPSIS
    Install or upgrade agentique on Windows (amd64).

.DESCRIPTION
    Downloads the latest released windows-amd64 binary, verifies its checksum,
    installs it under %LOCALAPPDATA%\Programs\agentique (override with
    $env:INSTALL_DIR), ensures that directory is on the user PATH, re-installs
    the scheduled-task service if one already exists, and runs `agentique doctor`.

    Usage:
        irm https://raw.githubusercontent.com/mdjarv/agentique/master/install.ps1 | iex
#>

#Requires -Version 5
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Repo = 'mdjarv/agentique'

$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -ne 'AMD64') {
    throw "unsupported architecture: $arch (only amd64/x64 binaries are published)"
}
$asset = 'agentique-windows-amd64.exe'

$installDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\agentique' }
$target = Join-Path $installDir 'agentique.exe'

# Resolve the latest release tag.
$headers = @{ 'User-Agent' = 'agentique-installer' }
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers
$tag = $release.tag_name
if (-not $tag) { throw 'failed to fetch latest release tag' }

# Short-circuit if already current.
if (Test-Path $target) {
    $existing = ''
    try { $existing = ((& $target --version) -split '\s+')[1] } catch { }
    if ($existing -eq $tag) {
        Write-Host "agentique $tag already installed."
        return
    }
    Write-Host "Upgrading agentique ($(if ($existing) { $existing } else { 'unknown' }) -> $tag)"
} else {
    Write-Host "Installing agentique $tag..."
}

$baseUrl = "https://github.com/$Repo/releases/download/$tag"
$tmp = New-TemporaryFile
try {
    Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $tmp.FullName -UseBasicParsing

    # Verify checksum if the release publishes one.
    $checksums = ''
    try { $checksums = (Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -UseBasicParsing).Content } catch { }
    if ($checksums) {
        $expected = $checksums -split "`n" |
            Where-Object { $_ -match [regex]::Escape($asset) } |
            ForEach-Object { ($_ -split '\s+')[0] } |
            Select-Object -First 1
        $actual = (Get-FileHash -Path $tmp.FullName -Algorithm SHA256).Hash.ToLower()
        if ($expected -and ($expected.ToLower() -ne $actual)) {
            throw "checksum mismatch`n  expected: $expected`n  got:      $actual"
        }
        Write-Host 'Checksum verified.'
    } else {
        Write-Warning 'no checksums available, skipping verification.'
    }

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null

    # Replace even a running binary: a running .exe can be renamed aside (but not
    # overwritten or deleted) on Windows, so move the old one out of the way and
    # write the new one to the canonical path.
    if (Test-Path $target) {
        $stale = "$target.old"
        Remove-Item -Force $stale -ErrorAction SilentlyContinue
        Rename-Item -Path $target -NewName ([IO.Path]::GetFileName($stale)) -ErrorAction SilentlyContinue
    }
    Move-Item -Force -Path $tmp.FullName -Destination $target
    Remove-Item -Force "$target.old" -ErrorAction SilentlyContinue
} finally {
    if (Test-Path $tmp.FullName) { Remove-Item -Force $tmp.FullName -ErrorAction SilentlyContinue }
}

Write-Host "Installed agentique $tag to $target"

# Ensure the install dir is on the user PATH.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$onPath = $userPath -and (($userPath -split ';') | Where-Object { $_.TrimEnd('\') -ieq $installDir.TrimEnd('\') })
if (-not $onPath) {
    $newPath = if ($userPath) { "$userPath;$installDir" } else { $installDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to your user PATH (restart your shell to pick it up)."
}

# Re-install the scheduled-task service if one is already registered, so it
# points at the freshly installed binary.
schtasks /Query /TN agentique 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) {
    & $target service install
}

Write-Host ''
Write-Host 'Checking dependencies...'
Write-Host ''
& $target doctor
