#!/bin/bash
# Fetch metadata + thumbnail for MP3 files using YouTube video IDs from filenames

MUSIC_DIR="${1:-$HOME/Music}"

for f in "$MUSIC_DIR"/*.mp3; do
  [ -f "$f" ] || continue
  bn=$(basename "$f")

  # Skip if already has title
  has_title=$(ffprobe -v quiet -print_format json -show_format "$f" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('format',{}).get('tags',{}).get('title',''))" 2>/dev/null)
  [ -n "$has_title" ] && echo "SKIP: $bn" && continue

  # Extract YouTube video ID
  vid=$(echo "$bn" | grep -oP '\[([a-zA-Z0-9_-]{11})\]' | head -1 | tr -d '[]')
  [ -z "$vid" ] && echo "SKIP (no ID): $bn" && continue

  echo "Processing: $bn"

  # Fetch metadata
  meta=$(yt-dlp --skip-download --print-json "https://www.youtube.com/watch?v=$vid" 2>/dev/null) || { echo "  FAILED fetch"; continue; }

  title=$(echo "$meta" | python3 -c "import sys,json; print(json.load(sys.stdin).get('title',''))" 2>/dev/null)
  artist=$(echo "$meta" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('artist','') or d.get('uploader',''))" 2>/dev/null)
  date=$(echo "$meta" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('upload_date','')[:4] if d.get('upload_date') else '')" 2>/dev/null)
  thumb_url=$(echo "$meta" | python3 -c "import sys,json; print(json.load(sys.stdin).get('thumbnail',''))" 2>/dev/null)

  # Step 1: Embed metadata
  tmp1="${f%.mp3}_a.mp3"
  ffmpeg -y -i "$f" \
    -metadata "title=$title" \
    -metadata "artist=$artist" \
    -metadata "date=$date" \
    -metadata "genre=Music" \
    -c:a copy "$tmp1" 2>/dev/null && mv "$tmp1" "$f"

  # Step 2: Download and embed thumbnail
  if [ -n "$thumb_url" ]; then
    thumb="/tmp/thumb_$vid.jpg"
    curl -sL -o "$thumb" "$thumb_url" 2>/dev/null
    if [ -s "$thumb" ]; then
      tmp2="${f%.mp3}_b.mp3"
      ffmpeg -y -i "$f" -i "$thumb" -map 0:a -map 1:0 -c:a copy -c:v mjpeg -disposition:v:0 attached_pic "$tmp2" 2>/dev/null && mv "$tmp2" "$f"
      rm -f "$thumb"
    fi
  fi

  echo "  OK: $title"
  sleep 1
done

echo "Done."
