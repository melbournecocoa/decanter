#!/usr/bin/env python3
"""Transcribe a video to SRT using Groq's whisper-large-v3 API.

Usage: transcribe.py <input_video> <output_srt> [--model whisper-large-v3|whisper-large-v3-turbo]

Requires: GROQ_API_KEY env var, ffmpeg on PATH, urllib (stdlib only).

Long segments returned by the API (>8s) are post-split at natural punctuation
boundaries so the resulting SRT is comfortable to read as YouTube captions.
"""
import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

API_URL = "https://api.groq.com/openai/v1/audio/transcriptions"
MAX_CHUNK_DURATION = 8.0  # seconds — caption readability cap


def format_srt_timestamp(seconds: float) -> str:
    hours = int(seconds // 3600)
    minutes = int((seconds % 3600) // 60)
    secs = int(seconds % 60)
    millis = int((seconds % 1) * 1000)
    return f"{hours:02d}:{minutes:02d}:{secs:02d},{millis:03d}"


def extract_audio(video_path: str, audio_path: str) -> None:
    print(f"Extracting audio → {audio_path}...", file=sys.stderr)
    subprocess.run(
        [
            "ffmpeg", "-y", "-loglevel", "error",
            "-i", video_path,
            "-vn", "-ac", "1", "-ar", "16000",
            "-c:a", "libopus", "-b:a", "32k",
            audio_path,
        ],
        check=True,
    )
    size_mb = Path(audio_path).stat().st_size / (1024 * 1024)
    print(f"Audio size: {size_mb:.2f} MB", file=sys.stderr)


def build_multipart(audio_path: str, model: str) -> tuple[bytes, str]:
    boundary = f"----decanter{uuid.uuid4().hex}"
    fields = {
        "model": model,
        "response_format": "verbose_json",
        "language": "en",
        "temperature": "0",
    }
    parts: list[bytes] = []
    for name, value in fields.items():
        parts.append(f"--{boundary}\r\n".encode())
        parts.append(f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode())
        parts.append(value.encode())
        parts.append(b"\r\n")
    parts.append(f"--{boundary}\r\n".encode())
    parts.append(
        f'Content-Disposition: form-data; name="file"; filename="{Path(audio_path).name}"\r\n'.encode()
    )
    parts.append(b"Content-Type: audio/ogg\r\n\r\n")
    parts.append(Path(audio_path).read_bytes())
    parts.append(b"\r\n")
    parts.append(f"--{boundary}--\r\n".encode())
    return b"".join(parts), boundary


def transcribe_groq(audio_path: str, model: str, api_key: str) -> dict:
    body, boundary = build_multipart(audio_path, model)
    req = urllib.request.Request(
        API_URL,
        data=body,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
            "User-Agent": "decanter-transcribe/0.1",
        },
        method="POST",
    )
    print(f"POST {API_URL} (model={model})...", file=sys.stderr)
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=600) as resp:
            payload = json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        sys.exit(f"Groq API error {e.code}: {e.read().decode()}")
    elapsed = time.time() - t0
    print(f"API call took {elapsed:.1f}s", file=sys.stderr)
    return payload


def _split_at_sentences(text: str) -> list[str]:
    return [p for p in re.split(r"(?<=[.!?])\s+", text) if p.strip()]


def _split_at_clauses(text: str) -> list[str]:
    return [p for p in re.split(r"(?<=[,;:])\s+", text) if p.strip()]


def _split_at_words(text: str) -> list[str]:
    words = text.split()
    if len(words) < 2:
        return [text]
    mid = len(words) // 2
    return [" ".join(words[:mid]), " ".join(words[mid:])]


def _greedy_pack(pieces: list[str], max_chars: float) -> list[str]:
    chunks: list[str] = []
    current: list[str] = []
    current_chars = 0
    for p in pieces:
        added = len(p) + (1 if current else 0)
        if current and current_chars + added > max_chars:
            chunks.append(" ".join(current))
            current = [p]
            current_chars = len(p)
        else:
            current.append(p)
            current_chars += added
    if current:
        chunks.append(" ".join(current))
    return chunks


def _split_one(text: str, start: float, end: float, max_duration: float) -> list[dict]:
    duration = end - start
    if duration <= max_duration:
        return [{"start": start, "end": end, "text": text}]
    pieces = _split_at_sentences(text)
    if len(pieces) < 2:
        pieces = _split_at_clauses(text)
    if len(pieces) < 2:
        pieces = _split_at_words(text)
    if len(pieces) < 2:
        return [{"start": start, "end": end, "text": text}]
    chars_total = sum(len(p) for p in pieces)
    max_chars = (chars_total / duration) * max_duration
    chunks = _greedy_pack(pieces, max_chars)
    if len(chunks) < 2:
        # Greedy pack collapsed back to one — last resort word-split.
        chunks = _split_at_words(text)
        if len(chunks) < 2:
            return [{"start": start, "end": end, "text": text}]
    total = sum(len(c) for c in chunks)
    result: list[dict] = []
    cursor = start
    for i, c in enumerate(chunks):
        share = len(c) / total
        c_end = end if i == len(chunks) - 1 else cursor + share * duration
        result.extend(_split_one(c, cursor, c_end, max_duration))
        cursor = c_end
    return result


def split_long_segments(segments: list[dict], max_duration: float = MAX_CHUNK_DURATION) -> list[dict]:
    out: list[dict] = []
    for seg in segments:
        start = float(seg["start"])
        end = float(seg["end"])
        text = (seg.get("text") or "").strip()
        if not text:
            continue
        out.extend(_split_one(text, start, end, max_duration))
    return out


def write_srt(payload: dict, output_path: str) -> int:
    raw_segments = payload.get("segments") or []
    if not raw_segments:
        text = payload.get("text", "").strip()
        with open(output_path, "w", encoding="utf-8") as f:
            f.write(f"1\n00:00:00,000 --> 00:00:00,000\n{text}\n\n")
        return 1
    segments = split_long_segments(raw_segments)
    print(
        f"Split {len(raw_segments)} API segments → {len(segments)} SRT entries "
        f"(max {MAX_CHUNK_DURATION:.0f}s each)",
        file=sys.stderr,
    )
    with open(output_path, "w", encoding="utf-8") as f:
        for i, seg in enumerate(segments, 1):
            start = format_srt_timestamp(float(seg["start"]))
            end = format_srt_timestamp(float(seg["end"]))
            text = seg["text"].strip()
            f.write(f"{i}\n{start} --> {end}\n{text}\n\n")
    return len(segments)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("input_video")
    parser.add_argument("output_srt")
    parser.add_argument("--model", default="whisper-large-v3",
                        choices=["whisper-large-v3", "whisper-large-v3-turbo"])
    args = parser.parse_args()

    api_key = os.environ.get("GROQ_API_KEY")
    if not api_key:
        sys.exit("GROQ_API_KEY env var is required")
    if not Path(args.input_video).exists():
        sys.exit(f"input file not found: {args.input_video}")

    overall = time.time()
    with tempfile.TemporaryDirectory() as tmp:
        audio_path = str(Path(tmp) / "audio.ogg")
        t0 = time.time()
        extract_audio(args.input_video, audio_path)
        print(f"ffmpeg took {time.time() - t0:.1f}s", file=sys.stderr)

        payload = transcribe_groq(audio_path, args.model, api_key)

    n = write_srt(payload, args.output_srt)
    total = time.time() - overall
    duration = float(payload.get("duration") or 0)
    rt = (duration / total) if total > 0 else 0
    print(
        f"Wrote {n} segments to {args.output_srt} ({total:.1f}s total, "
        f"audio={duration:.0f}s, {rt:.0f}x real-time)",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
