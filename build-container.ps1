# Builds freeman from the Windows host by delegating to the Go dev
# container: there's no Go toolchain on the host, so `bash build.sh` has to
# run inside the devcontainer for this repo. Wraps two steps that otherwise
# have to be done by hand every time:
#   1. Stop any running freeman.exe — a locked binary makes the Windows
#      cross-compile step in build.sh fail with "permission denied" when it
#      tries to overwrite bin/freeman.exe.
#   2. Find that devcontainer (its name is auto-generated and changes across
#      recreations, so it's matched by the "devcontainer.local_folder" label
#      instead of a hardcoded name) and run build.sh inside it.
#
# Builds both platforms by default, matching build.sh — pass -Windows or
# -Linux to build only that platform (-Windows for routine use on this
# host, which never runs the Linux binary; -Linux when only a Linux binary
# is needed, e.g. Xvfb-based visual verification inside the container).
# -Vet/-Test/-Check/-Full pass straight through to build.sh's own opt-in
# flags of the same name (--vet/--test/--check/--full).
param(
    [switch]$Windows,
    [switch]$Linux,
    [switch]$Vet,
    [switch]$Test,
    [switch]$Check,
    [switch]$Full
)

$ErrorActionPreference = "Stop"

$repoRoot = $PSScriptRoot.TrimEnd('\')

Get-Process freeman* -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

$container = $null
foreach ($name in (docker ps --format "{{.Names}}")) {
    $folder = docker inspect $name --format '{{ index .Config.Labels "devcontainer.local_folder" }}' 2>$null
    if ($folder -and ($folder.TrimEnd('\') -ieq $repoRoot)) {
        $container = $name
        break
    }
}
if (-not $container) {
    Write-Error "No running devcontainer found for $repoRoot. Open this repo's devcontainer in VS Code first."
    exit 1
}
Write-Host "Using container: $container"

$buildArgs = @()
if ($Windows) { $buildArgs += "--windows" }
if ($Linux) { $buildArgs += "--linux" }
if ($Vet) { $buildArgs += "--vet" }
if ($Test) { $buildArgs += "--test" }
if ($Check) { $buildArgs += "--check" }
if ($Full) { $buildArgs += "--full" }

docker exec $container bash -c "cd /workspaces/freeman && bash build.sh $($buildArgs -join ' ')"
exit $LASTEXITCODE
