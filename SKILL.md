---
name: pikpak-idm
description: "Use PikPak as a cloud download accelerator (IDM-style): submit download links/magnets/URLs to PikPak cloud for offline download, monitor task progress, manage downloaded files, then pull to local machine at high speed. Supports native aria2 integration for accelerated downloads."
---

# PikPak IDM — Cloud Download Accelerator

Use PikPak cloud as a high-speed download proxy. Files download to PikPak's servers first (offline task), then you pull them locally at full bandwidth via aria2.

## Tool

- **Binary:** `/root/workspace/pikpak-IDM/pikpakidm/pikpakidm`
- **Alias used below:** `pikpakidm` (always use full path in shell commands)
- **Config:** `~/.config/pikpakidm/config.yml`
  ⚠️ **NOT** beside the binary. The binary reads `$HOME/.config/pikpakidm/config.yml` by default.
  Override with `--config /path/to/config.yml`.

## Prerequisites

- `pikpakidm` binary exists at `/root/workspace/pikpak-IDM/pikpakidm/pikpakidm`
- Config file at **`~/.config/pikpakidm/config.yml`** with valid PikPak credentials and aria2 settings
- If config is missing, run: `/root/workspace/pikpak-IDM/pikpakidm/pikpakidm setup`
  (or copy `/root/workspace/pikpak-IDM/pikpakidm/config_example.yml` there and fill it in)

### Config file (`config.yml`) key fields:

```yaml
username: xxx          # PikPak account / phone with country code
password: xxx          # PikPak password
proxy:                 # Optional proxy, e.g. http://127.0.0.1:7890
aria2:
  # ⚠️ MUST be reachable FROM WHERE pikpakidm RUNS.
  # - same host as aria2   -> http://127.0.0.1:6800/jsonrpc
  # - inside a container   -> the DOCKER GATEWAY IP, e.g. http://172.17.0.1:6800/jsonrpc
  #   (127.0.0.1 will NOT work across containers)
  rpc_url: http://127.0.0.1:6800/jsonrpc
  secret:                                   # aria2 RPC secret (empty if none)
  max_connections: 16                       # connections per file
  split: 16                                 # parts to split download
  min_split_size: 1M
  # ⚠️ Directory as seen BY THE ARIA2 PROCESS (inside its container), not the host path.
  # For the `p3terx/aria2-pro` container here: host /vol2/1000/Downloads -> container /downloads,
  # so set: download_dir: /downloads
  download_dir: /downloads
```

## Workflow

### 1. Submit Download Task

Add URL/magnet/torrent to PikPak cloud for offline download:

```bash
# Magnet link
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new url 'magnet:?xt=urn:btih:<hash>'

# Direct HTTP/HTTPS URL
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new url 'https://example.com/file.zip'

# Multiple URLs from file (one per line)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new url -i /tmp/urls.txt

# Specify target folder in PikPak cloud
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new url -p /Downloads 'magnet:?xt=urn:btih:<hash>'

# CLI mode (non-interactive)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new url -c 'https://example.com/file.zip'
```

**Completion check:** `/root/workspace/pikpak-IDM/pikpakidm/pikpakidm ls -lH -p /Downloads` shows the new file(s).

### 2. Monitor Offline Tasks

PikPak processes offline tasks in background. Check cloud folder for completion:

```bash
# List files in download folder
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm ls -lH -p /Downloads

# Check quota (storage usage)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm quota -H
```

**Completion check:** File appears in `ls` output with expected size.

### 3. Download to Local (with aria2)

Pull completed files from PikPak cloud to local machine using aria2 for maximum speed:

> ⚠️ **CRITICAL — how to name the target file:**
> The file/folder to download is given as a **POSITIONAL argument**, NOT via `-p`.
> `-p` only sets the cloud *base folder* (default `/`). Passing a file path to `-p`
> makes the command print its help and do nothing.

```bash
# ✅ CORRECT: positional argument (single file)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --aria2 "/Movies/movie.mkv"

# ✅ Download a whole cloud folder with aria2
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --aria2 "/Downloads"

# ✅ Override aria2 RPC / dir (dir is INSIDE the aria2 container)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --aria2 \
  --aria2-rpc-url http://172.17.0.1:6800/jsonrpc \
  --aria2-dir /downloads \
  "/Movies/movie.mkv"

# ✅ Export aria2 input file instead of downloading now
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --export-aria2 /tmp/aria2.txt "/Downloads"

# ❌ WRONG: -p with a file path -> prints help, does nothing
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --aria2 -p /Downloads/movie.mkv
```

**Without aria2 (built-in HTTP downloader):**

```bash
# Built-in downloader with concurrency
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download -c 5 "/Downloads"

# With progress display
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download -c 5 -g "/Movies/movie.mkv"

# Force disable aria2
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --no-aria2 -c 5 "/Downloads"
```

**Video clip download (requires ffmpeg):**

```bash
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --time-range 01:02-01:30 "/Videos/movie.mp4"
```

**Completion check:** Local file exists and size matches cloud listing.

### 4. Cleanup (Optional)

Remove files from PikPak cloud after local download to free quota:

```bash
# Delete specific file
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm delete movie.mkv -p /Downloads

# Delete folder contents recursively
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm delete -p /Downloads

# Clean empty folders
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm empty -p /Downloads -d

# Remove junk files (matches rules)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm rubbish -p /Downloads -d

# Download default rubbish rules from GitHub first
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm rubbish --download-rules
```

## Common Patterns

### Batch Download Queue

```bash
# 1. Create a URLs file
cat > /tmp/urls.txt << EOF
magnet:?xt=urn:btih:abc123
https://example.com/file1.zip
https://example.com/file2.zip
EOF

# 2. Submit all to cloud
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new url -i /tmp/urls.txt -p /Queue

# 3. Wait, then pull all with aria2
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --aria2 -p /Queue -o ~/BatchDownload

# 4. Clean up cloud
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm empty -p /Queue -d
```

### Folder Sync (Cloud → Local)

```bash
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm download --aria2 -p /Movies -o ~/Movies
```

### Upload Local Files to PikPak

```bash
# Upload files/folders
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm upload -p /Backup /path/to/local/files

# Upload with concurrency and sync mode
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm upload -p /Backup -c 16 -s /path/to/local/files

# Exclude patterns
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm upload -p /Backup -e '*.tmp' -e '.git' /path/to/local/files
```

### Interactive Shell

```bash
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm shell
```

## File Management

### Browse Cloud

```bash
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm ls -lH -p /           # Root
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm ls -lH -p /Downloads  # Downloads folder
```

### Organize

```bash
# Create folder
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm new folder -p /Movies Action

# Move file (supports wildcards, use --literal for exact names)
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm mv /Downloads/movie.mkv /Movies/Action

# Rename
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm rename /Movies/Action/movie.mkv Better_Name.mkv
```

### Share

```bash
# Get share link
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm share -p /Movies/Action/movie.mkv

# Save someone's shared link to your drive
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm save https://mypikpak.com/s/<share_id>

# Save with passcode
/root/workspace/pikpak-IDM/pikpakidm/pikpakidm save https://mypikpak.com/s/<share_id> dd3e
```

## Tips

- **Prefer `--aria2`** for downloads — much faster than built-in HTTP, especially for large files
- **Offline tasks are async** — after `new url`, wait for PikPak to fetch the file before downloading locally
- **Use `-c` flag** (built-in mode) to control concurrent downloads
- **Monitor quota** with `quota -H` to avoid hitting storage limits
- **Use folders** to organize batch downloads (`/Queue`, `/Movies`, etc.)
- **Clean up regularly** — delete local-downloaded files from cloud to free space
- **Debug mode:** add `--debug` to any command; use `--debug-topic api,session,transfer` for targeted debug output
- **Config path:** pass `--config /path/to/config.yml` to use a non-default config

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Binary not found | Check `/root/workspace/pikpak-IDM/pikpakidm/pikpakidm` exists and is executable |
| `Init config failed ... no such file or directory` | Create `~/.config/pikpakidm/config.yml` (see Prerequisites) |
| `download` prints help instead of downloading | Do NOT pass the file via `-p`; pass it as a positional argument (see step 3) |
| Auth failed | Run `/root/workspace/pikpak-IDM/pikpakidm/pikpakidm setup` to reconfigure |
| File not appearing after `new url` | Wait — offline tasks take time; check again with `ls` |
| aria2 connection failed | Verify aria2 RPC is reachable from where pikpakidm runs (use `172.17.0.1`, not `127.0.0.1`, from another container); check `secret` matches |
| Download lands in wrong place | `aria2-dir`/`download_dir` is the path INSIDE the aria2 container (`/downloads`), not the host path |
| Download slow without aria2 | Use `--aria2` flag, or increase `-c` for built-in downloader |
| Quota full | Delete old files or upgrade plan |
