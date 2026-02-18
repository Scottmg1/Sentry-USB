# SentryUSB Build & Release Guide

## Prerequisites

- **Node.js** (with npm) — for building the frontend
- **Go** (1.21+) — for compiling the server binaries
- Both must be available in your PATH

## Build Steps

### 1. Build the Frontend

```powershell
cd web
npm run build
```

This outputs the production frontend to `web/dist/`.

### 2. Copy Frontend to Server Static

The Go binary embeds frontend files from `server/static/` via `//go:embed`. You **must** copy the fresh build output before compiling.

```powershell
cd ..
Remove-Item -Recurse -Force "server\static\*"
Copy-Item -Recurse "web\dist\*" "server\static\"
```

### 3. Compile Binaries

**ARM64** (Raspberry Pi 4/5, Radxa Zero 3W, etc.):

```powershell
cd server
$env:GOOS="linux"; $env:GOARCH="arm64"; go build -o "bin/sentryusb-linux-arm64" .
```

**ARMv7** (Raspberry Pi Zero 2W, older Pi models):

```powershell
$env:GOOS="linux"; $env:GOARCH="arm"; $env:GOARM="7"; go build -o "bin/sentryusb-linux-armv7" .
```

Binaries are output to `server/bin/`.

### 4. Verify

```powershell
Get-ChildItem "bin" | Select-Object Name, Length, LastWriteTime
```

Confirm both binaries are present with updated timestamps (~10-11 MB each).

## Quick One-Liner (PowerShell)

```powershell
cd web; npm run build; cd ..; Remove-Item -Recurse -Force "server\static\*"; Copy-Item -Recurse "web\dist\*" "server\static\"; cd server; $env:GOOS="linux"; $env:GOARCH="arm64"; go build -o "bin/sentryusb-linux-arm64" .; $env:GOOS="linux"; $env:GOARCH="arm"; $env:GOARM="7"; go build -o "bin/sentryusb-linux-armv7" .; cd ..
```

## Release

1. Build both binaries using the steps above
2. Commit all changes (frontend source, server source, setup scripts)
3. Push to the repo — the SentryUSB update mechanism on devices will pull new binaries and scripts

## Common Issues

- **Frontend changes not appearing**: You forgot step 2. The Go binary embeds from `server/static/`, not `web/dist/`. Always copy before compiling.
- **Stale JS bundle**: Check the hash in the filename (e.g., `index-Dda2tfbK.js`). If it hasn't changed after a build, your source changes may not have been saved.
