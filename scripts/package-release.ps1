[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]*$')]
    [string]$Version,
    [Parameter(Mandatory = $true)]
    [ValidateSet('windows', 'darwin', 'linux')]
    [string]$TargetOS,
    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string]$TargetArch
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Assert-NativeSuccess([string]$Operation) {
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed with exit code $LASTEXITCODE"
    }
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $hostOS = (& go env GOHOSTOS).Trim()
    Assert-NativeSuccess 'Detect Go host OS'
    $hostArch = (& go env GOHOSTARCH).Trim()
    Assert-NativeSuccess 'Detect Go host architecture'
    if ($hostOS -ne $TargetOS -or $hostArch -ne $TargetArch) {
        throw 'Release builds must run on a native runner for the target OS and architecture (CGO is required).'
    }

    $env:CGO_ENABLED = '1'
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch
    $name = "cyberstrike-ai_${Version}_${TargetOS}_${TargetArch}"
    $stage = Join-Path $root "release-build/$name"
    $dist = Join-Path $root 'dist'
    if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
    New-Item -ItemType Directory -Force $stage, $dist | Out-Null

    # Export only tracked, explicitly selected runtime resources: never local
    # config.yaml, API keys, databases, logs, virtualenvs, or generated payloads.
    $resources = @(
        'web', 'tools', 'roles', 'skills', 'agents', 'knowledge_base',
        'mcp-servers', 'plugins', 'docs', 'config.example.yaml',
        'requirements.txt', 'LICENSE', 'README.md', 'README_CN.md', 'SECURITY.md'
    )
    $resourceTar = Join-Path $root "release-build/resources-${TargetOS}-${TargetArch}.tar"
    & git archive --format=tar "--output=$resourceTar" HEAD -- @resources
    Assert-NativeSuccess 'Export runtime resources'
    & tar -xf $resourceTar -C $stage
    Assert-NativeSuccess 'Extract runtime resources'
    Remove-Item $resourceTar

    # The UI reads its version from the configuration, not linker variables.
    $template = Join-Path $stage 'config.example.yaml'
    $content = [System.IO.File]::ReadAllText($template)
    if ($content -notmatch '(?m)^version:.*$') { throw 'Configuration version field is missing.' }
    $content = [regex]::Replace($content, '(?m)^version:.*$', "version: `"$Version`"")
    [System.IO.File]::WriteAllText($template, $content, [System.Text.UTF8Encoding]::new($false))

    $suffix = ''
    if ($TargetOS -eq 'windows') { $suffix = '.exe' }
    $binaries = @{
        'cyberstrike-ai' = './cmd/server'
        'cyberstrike-ai-mcp-stdio' = './cmd/mcp-stdio'
    }
    foreach ($binary in $binaries.Keys) {
        $output = Join-Path $stage "$binary$suffix"
        & go build -mod=readonly -trimpath -ldflags '-s -w' -o $output $binaries[$binary]
        Assert-NativeSuccess "Build $binary"
        # -h exits before loading config, starting services, or executing tools.
        & $output -h
        Assert-NativeSuccess "Smoke test $binary"
    }

    $commit = (& git rev-parse HEAD).Trim()
    Assert-NativeSuccess 'Read source commit'
    $metadata = [ordered]@{
        version = $Version
        commit = $commit
        os = $TargetOS
        arch = $TargetArch
        go = ((& go version) -join ' ')
    } | ConvertTo-Json
    Assert-NativeSuccess 'Read Go version'
    [System.IO.File]::WriteAllText((Join-Path $stage 'build-info.json'), $metadata, [System.Text.UTF8Encoding]::new($false))

    if ($TargetOS -eq 'windows') {
        $archive = Join-Path $dist "$name.zip"
        if (Test-Path $archive) { Remove-Item $archive }
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        # Unlike Compress-Archive, ZipFile preserves hidden resource files.
        [System.IO.Compression.ZipFile]::CreateFromDirectory(
            $stage, $archive, [System.IO.Compression.CompressionLevel]::Optimal, $true
        )
    } else {
        $archive = Join-Path $dist "$name.tar.gz"
        & tar -czf $archive -C (Split-Path -Parent $stage) $name
        Assert-NativeSuccess 'Create release archive'
    }
    Write-Host "Release archive: $archive"
} finally {
    Pop-Location
}