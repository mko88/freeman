<#
.SYNOPSIS
    Stop the Freeman test server started by Start-TestServer.ps1.

.DESCRIPTION
    Reads the state file scripts/test_server.py wrote, stops that
    process, and waits until its ports are free again — so a
    Stop-then-Start pair can't race, which is the one thing that would
    make the next start fail for no visible reason.

    Stopping one that isn't running is not an error: it says so and
    exits 0, so this is safe in a teardown step.

.PARAMETER TimeoutSeconds
    How long to wait for the process to exit and the ports to close.

.EXAMPLE
    pwsh scripts/Stop-TestServer.ps1
#>

[CmdletBinding(SupportsShouldProcess)]
param(
    [int] $TimeoutSeconds = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'TestServer.Common.ps1')

$stateFile = Get-TestServerStateFile
$state = Get-TestServerState

if (-not $state) {
    Write-Host "No test server is running (nothing recorded at $stateFile)."
    return
}

$proc = Get-TestServerProcess -State $state
if (-not $proc) {
    # Either it's gone, or that pid now belongs to something else — and
    # Get-TestServerProcess checks the command line precisely so this
    # branch never kills the something else.
    Write-Host "No test server is running; clearing a stale state file for pid $($state.pid)."
    Remove-Item -LiteralPath $stateFile -Force -ErrorAction SilentlyContinue
    return
}

if (-not $PSCmdlet.ShouldProcess("pid $($state.pid) on ports $((Get-TestServerPorts -State $state) -join ', ')", 'Stop the test server')) {
    return
}

Write-Host "Stopping the test server (pid $($state.pid))..."
Stop-Process -Id $proc.Id -Force

if (-not $proc.WaitForExit($TimeoutSeconds * 1000)) {
    throw "pid $($state.pid) is still running after $TimeoutSeconds seconds."
}

# Stop-Process terminates rather than signalling, so test_server.py's own
# cleanup never ran: the state file is this script's to remove, and the
# listening sockets take a moment to actually close.
$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
$ports = Get-TestServerPorts -State $state
while ((Get-Date) -lt $deadline) {
    $stillOpen = @($ports | Where-Object { Test-TestServerPort -Port $_ -TargetHost $state.host -TimeoutMs 200 })
    if ($stillOpen.Count -eq 0) { break }
    Start-Sleep -Milliseconds 150
}

Remove-Item -LiteralPath $stateFile -Force -ErrorAction SilentlyContinue

$stillOpen = @($ports | Where-Object { Test-TestServerPort -Port $_ -TargetHost $state.host -TimeoutMs 200 })
if ($stillOpen.Count -gt 0) {
    Write-Warning "Stopped, but something is still listening on $($stillOpen -join ', ') — another server, or a socket not yet released."
    exit 1
}

Write-Host "Stopped. Ports $($ports -join ', ') are free." -ForegroundColor Green
