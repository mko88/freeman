<#
.SYNOPSIS
    Shared helpers for Start-TestServer.ps1 and Stop-TestServer.ps1.

.DESCRIPTION
    Dot-sourced by both, so the two agree on where the state file lives
    and on what counts as "running" — neither repeats the port numbers,
    which belong to scripts/test_server.py and are written into the
    state file when it starts.

    Not meant to be run on its own.
#>

Set-StrictMode -Version Latest

# scripts/test_server.py writes this on startup with its pid and the
# ports it actually got, and deletes it on a clean exit. GetTempPath()
# is .NET's answer to Python's tempfile.gettempdir(); both read TMP/TEMP
# on Windows, so the two see the same file.
$script:TestServerStateFile = Join-Path ([System.IO.Path]::GetTempPath()) 'freeman-test-server.json'

function Get-TestServerStateFile {
    $script:TestServerStateFile
}

function Get-TestServerState {
    <#
        The recorded state, or $null when there is no state file or it
        can't be read. Says nothing about whether the process is still
        alive — see Get-TestServerProcess for that.
    #>
    $path = Get-TestServerStateFile
    if (-not (Test-Path -LiteralPath $path)) { return $null }
    try {
        Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    } catch {
        Write-Warning "Ignoring an unreadable state file at ${path}: $($_.Exception.Message)"
        $null
    }
}

function Get-TestServerProcess {
    <#
        The running process the state file describes, or $null.

        Checks the command line, not just that something holds the pid:
        a pid is reused once its process exits, and stopping whatever
        inherited it would be a genuinely bad way to find out that the
        state file was stale.
    #>
    param([Parameter(Mandatory)] $State)

    if (-not $State.PSObject.Properties['pid']) { return $null }
    $proc = Get-Process -Id $State.pid -ErrorAction SilentlyContinue
    if (-not $proc) { return $null }

    $cim = Get-CimInstance Win32_Process -Filter "ProcessId = $($State.pid)" -ErrorAction SilentlyContinue
    if ($cim -and $cim.CommandLine -notmatch 'test_server\.py') { return $null }
    $proc
}

function Test-TestServerPort {
    <#
        Whether something is listening, by connecting rather than by
        reading a table — the point is that a request would arrive, and
        for the TLS ports a handshake never happens, so this deliberately
        stops at the TCP connect.
    #>
    param(
        [Parameter(Mandatory)] [int] $Port,
        [string] $TargetHost = '127.0.0.1',
        [int] $TimeoutMs = 500
    )

    $client = [System.Net.Sockets.TcpClient]::new()
    try {
        $connect = $client.ConnectAsync($TargetHost, $Port)
        if (-not $connect.Wait($TimeoutMs)) { return $false }
        $client.Connected
    } catch {
        $false
    } finally {
        $client.Dispose()
    }
}

function Get-TestServerPorts {
    param([Parameter(Mandatory)] $State)
    @($State.plain, $State.tls, $State.mtls)
}

function Write-TestServerSummary {
    param([Parameter(Mandatory)] $State)

    Write-Host "  plain        $($State.baseUrl)"
    Write-Host "  TLS          $($State.tlsBaseUrl)   (self-signed)"
    Write-Host "  mutual TLS   $($State.mtlsBaseUrl)   (also wants a client certificate)"
    Write-Host "  pid          $($State.pid)"
}

function Resolve-PythonLauncher {
    <#
        `py` is the Windows launcher and what the rest of this repo's
        docs use; `python` is the fallback for a machine without it.
    #>
    foreach ($name in 'py', 'python') {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
    }
    throw "Neither 'py' nor 'python' is on PATH — install Python 3.8+ or add it."
}
