# Firmirror

A firmware mirroring tool that creates LVFS-compatible repositories by fetching and converting firmware from hardware vendors (Dell and HPE).

## Overview

Firmirror automates the process of:
- Fetching firmware catalogs from vendor sources
- Downloading firmware packages
- Converting vendor-specific formats to LVFS/fwupd AppStream metadata
- Building CAB packages compatible with fwupd
- Maintaining a LVFS-compatible metadata index

## Features

- **Multi-vendor Support**: Currently supports Dell DSU and HPE SDR repositories
- **Incremental Processing**: Tracks processed firmware to avoid re-downloading
- **Pluggable Storage**: Abstract storage interface supporting local filesystem, with ability to add cloud storage (S3, GCS, etc.)

## Installation

### Prerequisites

- Go 1.19 or higher
- `fwupdtool` command-line tool
- `jcat-tool` for signature

### Building

```bash
go build -o firmirror ./cmd/firmirror.go
```

### Docker

A multi-stage Dockerfile is available:

```bash
docker build -t firmirror .
docker run -v /output:/output firmirror refresh /output --dell.enable
```

## Configuration

### CLI Flags

```
Global Flags:
  --help                Show help

Refresh Command:
  <out-dir>             Output directory for firmware and metadata

Dell Flags:
  --dell.enable         Enable Dell firmware mirroring
  --dell.machines-id    Comma-separated list of System IDs (e.g., 0C60,0C61)

HPE Flags:
  --hpe.enable          Enable HPE firmware mirroring
  --hpe.gens            Comma-separated list of generations (e.g., gen10,gen11)
```

## Usage

### Basic Command

```bash
# Mirror Dell firmware for specific machine types
./firmirror refresh /output/dir \
  --dell.enable \
  --dell.machines-id=0C60,0C61

# Mirror HPE firmware for specific generations
./firmirror refresh /output/dir \
  --hpe.enable \
  --hpe.gens=gen10,gen11

# Mirror both vendors
./firmirror refresh /output/dir \
  --dell.enable \
  --dell.machines-id=0C60 \
  --hpe.enable \
  --hpe.gens=gen10
```

### Output Structure

```
/output/dir/
├── firmware1.bin.cab       # CAB packages
├── firmware2.bin.cab
├── ...
├── metadata.xml.zst        # Compressed LVFS metadata
└── metadata.xml            # Uncompressed metadata (temporary)
```
