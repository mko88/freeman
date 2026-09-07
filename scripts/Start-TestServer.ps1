<#
.SYNOPSIS
    Start the Freeman test server in the background.

.DESCRIPTION
    Runs scripts/test_server.py detached, waits until all three of its
    listeners answer, and prints where they are. Use it when you want
    the server up while you work in the same terminal; run the Python
    script directly (or pass -Foreground) when you'd rather watch its
    output.

    Starting it twice is a no-op: if the recorded process is still
    running it reports that and leaves it alone.

    Stop it with Stop-TestServer.ps1.

.PARAMETER Port
    Plain HTTP port. Defaults to test_server.py's own (8100).

.PARAMETER TlsPort
    HTTPS port, self-signed certificate. Defaults to 8101.

.PARAMETER MtlsPort
    HTTPS port demanding a client certificate. Defaults to 8102.

.PARAMETER BindAddress
    Interface to listen on. Loopback only unless you change it.

.PARAMETER Foreground
    Run in this terminal instead of detaching, so you see its output.
    Ctrl-C stops it; Stop-TestServer.ps1 works on it too.

.PARAMETER Restart
    Stop a server that's already running and start a fresh one.

.EXAMPLE
    pwsh scripts/Start-TestServer.ps1

.EXAMPLE
    pwsh scripts/Start-TestServer.ps1 -Port 9100 -TlsPort 9101 -MtlsPort 9102
#>

[CmdletBinding()]
param(
    [int] $Port,
    [int] $TlsPort,
    [int] $MtlsPort,
    [string] $BindAddress,
    [switch] $Foreground,
    [switch] $Restart,
    [int] $TimeoutSeconds = 20
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'TestServer.Common.ps1')

$serverScript = Join-Path $PSScriptRoot 'test_server.py'
if (-not (Test-Path -LiteralPath $serverScript)) {
    throw "Can't find $serverScript"
}

$state = Get-TestServerState
if ($state) {
    $running = Get-TestServerProcess -State $state
    if ($running -and -not $Restart) {
        Write-Host "The test server is already running." -ForegroundColor Yellow
        Write-TestServerSummary -State $state
        Write-Host "`nUse -Restart to replace it, or Stop-TestServer.ps1 to stop it."
        return
    }
    if ($running) {
        Write-Host "Restarting: stopping pid $($state.pid) first."
        & (Join-Path $PSScriptRoot 'Stop-TestServer.ps1')
    } else {
        # The process behind it is gone — a hard kill, a reboot, a crash.
        # test_server.py refuses a port that's taken, so there's nothing
        # to protect here beyond not confusing the next reader.
        Write-Verbose "Removing a stale state file left by pid $($state.pid)."
        Remove-Item -LiteralPath (Get-TestServerStateFile) -Force -ErrorAction SilentlyContinue
    }
}

$python = Resolve-PythonLauncher
$serverArgs = @($serverScript)
if ($PSBoundParameters.ContainsKey('Port')) { $serverArgs += @('--port', $Port) }
if ($PSBoundParameters.ContainsKey('TlsPort')) { $serverArgs += @('--tls-port', $TlsPort) }
if ($PSBoundParameters.ContainsKey('MtlsPort')) { $serverArgs += @('--mtls-port', $MtlsPort) }
if ($PSBoundParameters.ContainsKey('BindAddress')) { $serverArgs += @('--host', $BindAddress) }

if ($Foreground) {
    # No state-file polling here: the Python script writes it as it comes
    # up and prints its own summary, and this call doesn't return until
    # you stop it.
    & $python @serverArgs
    return
}

Write-Host "Starting the test server..."
# --quiet only when detaching: the endpoint list goes to a hidden window
# nobody will read, and this script prints the URLs itself. In the
# foreground it's the whole reason to watch.
$proc = Start-Process -FilePath $python -ArgumentList ($serverArgs + '--quiet') -WindowStyle Hidden -PassThru

# Wait for the state file *and* for every port to answer. The file
# appears once the sockets are bound, but a connect is what actually
# proves the server is usable, and it's what the caller is about to do.
$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
$state = $null
while ((Get-Date) -lt $deadline) {
    $state = Get-TestServerState
    if ($state) {
        $ports = Get-TestServerPorts -State $state
        if (@($ports | Where-Object { Test-TestServerPort -Port $_ -TargetHost $state.host }).Count -eq $ports.Count) {
            break
        }
    }
    if ($proc.HasExited) {
        # It refused to start — a port in use is the usual reason, and
        # test_server.py says so on stdout, which a hidden window ate.
        # Re-run it in the foreground so the reason is visible.
        Write-Host "The server exited immediately. Running it again to show why:`n" -ForegroundColor Yellow
        & $python @serverArgs
        exit 2
    }
    Start-Sleep -Milliseconds 150
    $state = $null
}

if (-not $state) {
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    throw "The test server didn't come up within $TimeoutSeconds seconds."
}

Write-Host "Test server running." -ForegroundColor Green
Write-TestServerSummary -State $state
Write-Host "`n  stop with    pwsh scripts/Stop-TestServer.ps1"
Write-Host "  requests     py scripts/seed_test_requests.py"
