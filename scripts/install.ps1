# handoff installer for Windows.
# Detects the architecture, downloads the latest release, verifies SHA256,
# installs to the user profile and adds it to PATH.
#
#   irm https://raw.githubusercontent.com/yujinping/handoff/main/scripts/install.ps1 | iex
#
# When running the downloaded file you can pass parameters:
#   .\install.ps1 -Version v0.2.0 -Dir C:\tools -NoPath
#
# Piping into iex cannot pass parameters; use environment variables instead:
#   $env:HANDOFF_VERSION='v0.2.0'; irm ... | iex
#   $env:HANDOFF_INSTALL_DIR='C:\tools'; irm ... | iex

param(
    [string]$Version = $env:HANDOFF_VERSION,
    [string]$Dir = $env:HANDOFF_INSTALL_DIR,
    [string]$BaseUrl = $env:HANDOFF_BASE_URL,
    [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'yujinping/handoff'
$Bin = 'handoff'

if (-not $Dir) {
    $Dir = Join-Path $env:LOCALAPPDATA 'Programs\handoff'
}
if (-not $BaseUrl) {
    $BaseUrl = "https://github.com/$Repo/releases/download"
}
$BaseUrl = $BaseUrl.TrimEnd('/')

# Windows PowerShell 5.1 may still default to TLS 1.0, which GitHub rejects.
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
}

function Get-LatestTag {
    $api = "https://api.github.com/repos/$Repo/releases/latest"
    try {
        $release = Invoke-RestMethod -Uri $api -UseBasicParsing -TimeoutSec 20 `
            -Headers @{ 'User-Agent' = 'handoff-installer' }
        if ($release.tag_name) {
            return [string]$release.tag_name
        }
    } catch {
        Write-Verbose "GitHub API lookup failed: $($_.Exception.Message)"
    }

    # Fall back to the redirect target of /releases/latest.
    try {
        $resp = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" `
            -UseBasicParsing -MaximumRedirection 5 -TimeoutSec 20
        $uri = $null
        try { $uri = $resp.BaseResponse.ResponseUri.AbsoluteUri } catch { }
        if (-not $uri) {
            try { $uri = $resp.BaseResponse.RequestMessage.RequestUri.AbsoluteUri } catch { }
        }
        if ($uri -and $uri -match '/tag/([^/?#]+)') {
            return $Matches[1]
        }
    } catch {
        Write-Verbose "Redirect lookup failed: $($_.Exception.Message)"
    }

    throw 'Could not determine the latest version. Pass -Version vX.Y.Z.'
}

if (-not $Version -or $Version -eq 'latest') {
    $Version = Get-LatestTag
}
$Version = $Version.Trim()
if ($Version -notmatch '^v') {
    $Version = "v$Version"
}
$ver = $Version.Substring(1)

$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) {
    $arch = $env:PROCESSOR_ARCHITECTURE
}
if ($arch -ne 'AMD64') {
    throw "Unsupported architecture: $arch (only windows/amd64 is published; Windows on ARM is not supported)."
}
$assetArch = 'amd64'

$asset = "${Bin}_${ver}_windows_${assetArch}.exe"
$releaseBase = "$BaseUrl/$Version"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("handoff-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    $exePath = Join-Path $tmp $asset
    $sumPath = Join-Path $tmp 'checksums.txt'

    Write-Host "Downloading $asset ..."
    Invoke-WebRequest -Uri "$releaseBase/$asset" -OutFile $exePath -UseBasicParsing -TimeoutSec 300
    Invoke-WebRequest -Uri "$releaseBase/checksums.txt" -OutFile $sumPath -UseBasicParsing -TimeoutSec 60

    $want = $null
    foreach ($line in Get-Content -LiteralPath $sumPath) {
        $parts = $line -split '\s+'
        if ($parts.Count -ge 2 -and $parts[1] -eq $asset) {
            $want = $parts[0]
            break
        }
    }
    if (-not $want) {
        throw "checksums.txt does not contain $asset"
    }

    $got = (Get-FileHash -LiteralPath $exePath -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want.ToLower()) {
        throw "SHA256 mismatch (expected $want, got $got)"
    }

    Unblock-File -LiteralPath $exePath -ErrorAction SilentlyContinue

    if (-not (Test-Path -LiteralPath $Dir)) {
        New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    }
    $dest = Join-Path $Dir "$Bin.exe"
    try {
        Copy-Item -LiteralPath $exePath -Destination $dest -Force
    } catch {
        throw "Failed to write $dest. Close anything running handoff (for example an MCP client) and retry. $($_.Exception.Message)"
    }
    Unblock-File -LiteralPath $dest -ErrorAction SilentlyContinue

    try {
        $output = & $dest version 2>&1
    } catch {
        throw "The installed binary failed to start: $($_.Exception.Message)"
    }
    if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
        throw "The installed binary failed to run (exit code $LASTEXITCODE): $output"
    }

    Write-Host "Installed handoff $ver to $dest"
    Write-Host $output
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

if (-not $NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) {
        $userPath = ''
    }
    $found = $false
    foreach ($entry in @($userPath -split ';')) {
        if ($entry -and $entry.TrimEnd('\') -ieq $Dir.TrimEnd('\')) {
            $found = $true
            break
        }
    }
    if (-not $found) {
        $newPath = if ($userPath) { $userPath.TrimEnd(';') + ';' + $Dir } else { $Dir }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        $env:Path = $env:Path + ';' + $Dir
        Write-Host "Added $Dir to the user PATH. Open a new terminal to use it."
    }
}

Write-Host ''
Write-Host 'Next steps:'
Write-Host '  handoff init'
Write-Host '  handoff mcp     # use it in your AI client MCP config'