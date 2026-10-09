param([string]$Version = '', [string]$Destination = '')
$ErrorActionPreference = 'Stop'
$repo = 'ZevaierZhan/guarded-assist'
if (!$Version) { $Version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name }
if ($Version -notmatch '^v\d+\.\d+\.\d+$') { throw 'Invalid stable Release version' }
$machineArch = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) { $machineArch = $env:PROCESSOR_ARCHITEW6432 }
$arch = switch ($machineArch) { 'AMD64' { 'amd64' } 'ARM64' { 'arm64' } default { throw "Unsupported architecture: $machineArch" } }
if (!$Destination) { $Destination = Join-Path $env:LOCALAPPDATA "GuardedAssist/releases/$Version/windows-$arch" }
$Destination = [IO.Path]::GetFullPath($Destination)
$asset = "guarded-assist-server-windows-$arch.zip"
$base = "https://github.com/$repo/releases/download/$Version"
$temp = Join-Path ([IO.Path]::GetTempPath()) ('guarded-assist-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temp | Out-Null
try {
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile (Join-Path $temp 'SHA256SUMS')
    Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $temp $asset)
    $line = Get-Content (Join-Path $temp 'SHA256SUMS') | Where-Object { $_ -match ('^[a-f0-9]{64}  ' + [regex]::Escape($asset) + '$') }
    if (@($line).Count -ne 1) { throw 'Checksum entry missing or ambiguous' }
    $expected = $line.Substring(0, 64)
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $temp $asset)).Hash.ToLowerInvariant() -ne $expected) { throw 'Release checksum mismatch' }
    if (Test-Path -LiteralPath $Destination) {
        if (!(Test-Path -LiteralPath (Join-Path $Destination 'assistctl.exe'))) { throw 'Destination exists but is incomplete; choose another Destination' }
    } else { Expand-Archive -LiteralPath (Join-Path $temp $asset) -DestinationPath $Destination }
    @{ version = $Version; cli = (Join-Path $Destination 'assistctl.exe'); directory = $Destination } | ConvertTo-Json -Compress
} finally {
    # Only the explicitly created temporary directory is removed.
    Remove-Item -LiteralPath $temp -Recurse -Force
}
