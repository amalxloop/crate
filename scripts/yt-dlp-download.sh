#!/bin/bash
# Download YouTube audio as MP3 with metadata and thumbnail embedded
# Usage: ./yt-dlp-download.sh <youtube-url> [output-dir]

set -e

URL="${1:?Usage: $0 <youtube-url> [output-dir]}"
OUTDIR="${2:-$HOME/Music}"

mkdir -p "$OUTDIR"

yt-dlp \
  -x --audio-format mp3 --audio-quality 0 \
  --embed-thumbnail \
  --add-metadata \
  --metadata-from-title "%(artist)s - %(title)s" \
  --parse-metadata "%(title)s - %(artist)s:%(meta_artist)s" \
  --parse-metadata "%(title)s - %(artist)s:%(meta_title)s" \
  --convert-thumbnails jpg \
  -o "$OUTDIR/%(title)s.%(ext)s" \
  --no-playlist \
  "$URL"
