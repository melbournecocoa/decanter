#!/usr/bin/env python3
"""Detect bumper regions in a video using silence detection + dHash.

Usage: detect_bumpers.py <video_path> <reference_image>

Output: JSON array of bumper regions to stdout.
    [{"visual_start": 666.0, "visual_end": 704.0}, ...]

Algorithm:
1. ffmpeg silencedetect finds candidate silent regions (noise=-40dB, duration=10s)
2. Coarse pass: probe frames at PROBE_INTERVAL seconds inside each silence
   region. Frames with dHash distance < DHASH_THRESHOLD = bumper.
3. Refinement: walk outward in FINE_STEP increments from the coarse boundary
   until dHash distance plateaus high — that's the clean speaker boundary,
   past the MimoLive cross-fade.
4. Report refined visual boundaries for each silence region.
"""
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Callable

SILENCE_NOISE_DB = -40
SILENCE_MIN_DURATION = 10
DHASH_SIZE = 16
DHASH_THRESHOLD = 30
PROBE_INTERVAL = 2  # seconds between coarse frame probes

# MimoLive "dissolve" transition duration; bounds the cross-fade contribution
# to the boundary error.
FADE_DURATION = 1.0

# Fine-pass refinement parameters. Worst-case walk distance from coarse
# boundary = PROBE_INTERVAL (probe quantisation) + FADE_DURATION (cross-fade)
# + PLATEAU_WINDOW * FINE_STEP (confirmation window) = 3.5 s. MAX_WALK gives
# a half-second cushion.
FINE_STEP = 0.1
MAX_WALK = 4.0
PLATEAU_WINDOW = 5
PLATEAU_DELTA = 3
PLATEAU_HIGH = 45  # 1.5x DHASH_THRESHOLD — guards against false plateau on a static bumper frame


def detect_silence(video_path: str) -> list[tuple[float, float]]:
    """Run ffmpeg silencedetect and parse silent regions from stderr."""
    cmd = [
        "ffmpeg", "-i", video_path,
        "-af", f"silencedetect=noise={SILENCE_NOISE_DB}dB:d={SILENCE_MIN_DURATION}",
        "-f", "null", "-",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)

    regions = []
    starts = re.findall(r"silence_start: ([\d.]+)", result.stderr)
    ends = re.findall(r"silence_end: ([\d.]+)", result.stderr)

    for start, end in zip(starts, ends):
        regions.append((float(start), float(end)))

    return regions


def extract_frame(video_path: str, timestamp: float, output_path: str) -> bool:
    """Extract a single frame from the video at the given timestamp."""
    cmd = [
        "ffmpeg", "-ss", str(timestamp),
        "-i", video_path,
        "-frames:v", "1",
        "-y", output_path,
    ]
    result = subprocess.run(cmd, capture_output=True)
    return result.returncode == 0


def compute_dhash_distance(image_path: str, reference_path: str) -> int:
    """Compute dHash distance between two images."""
    from imagehash import dhash
    from PIL import Image

    img_hash = dhash(Image.open(image_path), hash_size=DHASH_SIZE)
    ref_hash = dhash(Image.open(reference_path), hash_size=DHASH_SIZE)
    return img_hash - ref_hash


def is_plateau(samples: list[tuple[float, int]]) -> bool:
    """True when the most recent PLATEAU_WINDOW samples are stable and high.

    Stable: spread (max - min) across the window <= PLATEAU_DELTA.
    High: min distance in the window >= PLATEAU_HIGH — guards against a false
    plateau on a static frame inside the bumper itself.
    """
    if len(samples) < PLATEAU_WINDOW:
        return False
    window = [d for _, d in samples[-PLATEAU_WINDOW:]]
    return min(window) >= PLATEAU_HIGH and (max(window) - min(window)) <= PLATEAU_DELTA


def walk_to_plateau(
    probe: Callable[[float], int], start_t: float, direction: int
) -> float | None:
    """Walk from start_t in `direction` (+1 forward, -1 backward) in FINE_STEP
    increments, calling `probe(t)` for the dHash distance at each step. Returns
    the timestamp of the first sample in the confirmed plateau window, or None
    if no plateau is found within MAX_WALK.

    `probe` is injected for testability; production callers wrap extract_frame
    + compute_dhash_distance.
    """
    samples: list[tuple[float, int]] = []
    steps = int(MAX_WALK / FINE_STEP)
    for i in range(1, steps + 1):
        t = start_t + direction * i * FINE_STEP
        if t < 0:
            break
        d = probe(t)
        samples.append((t, d))
        if is_plateau(samples):
            return samples[-PLATEAU_WINDOW][0]
    return None


def _make_video_probe(video_path: str, reference_path: str, tmpdir: str) -> Callable[[float], int]:
    """Build a probe closure that extracts a frame and returns its dHash
    distance to reference_path. Used during real bumper detection."""
    def probe(t: float) -> int:
        frame_path = str(Path(tmpdir) / f"frame_{t:.3f}.png")
        if not extract_frame(video_path, t, frame_path):
            # Treat extraction failures as "still bumper-y" so the walk keeps
            # trying outward; the plateau check will simply not fire here.
            return 0
        return compute_dhash_distance(frame_path, reference_path)
    return probe


def refine_visual_boundaries(
    video_path: str, silence_start: float, silence_end: float, reference_path: str
) -> tuple[float, float] | None:
    """Find the clean visual bumper boundaries within a silence region.

    Two passes:
    1. Coarse — probe at PROBE_INTERVAL inside [silence_start, silence_end]
       to find the rough bumper extent (first/last frame below DHASH_THRESHOLD).
    2. Fine — walk outward from each coarse boundary in FINE_STEP increments
       until dHash distance plateaus high. The fine walk is allowed to cross
       outside the silence region — cross-fades extend beyond audio silence,
       especially the fade in/out where MimoLive's audio mute happens at
       scene-flip but the visual fade has already started/finished.
    """
    with tempfile.TemporaryDirectory() as tmpdir:
        probe = _make_video_probe(video_path, reference_path, tmpdir)

        coarse_start: float | None = None
        coarse_end: float | None = None

        t = silence_start
        while t <= silence_end:
            distance = probe(t)
            if distance < DHASH_THRESHOLD:
                if coarse_start is None:
                    coarse_start = t
                coarse_end = t
            t += PROBE_INTERVAL

        if coarse_start is None or coarse_end is None:
            return None

        refined_start = walk_to_plateau(probe, start_t=coarse_start, direction=-1)
        if refined_start is None:
            print(
                f"  warning: no pre-bumper plateau within {MAX_WALK}s of "
                f"coarse start {coarse_start:.2f}s; keeping coarse value",
                file=sys.stderr,
            )
            refined_start = coarse_start

        refined_end = walk_to_plateau(probe, start_t=coarse_end, direction=+1)
        if refined_end is None:
            print(
                f"  warning: no post-bumper plateau within {MAX_WALK}s of "
                f"coarse end {coarse_end:.2f}s; keeping coarse value",
                file=sys.stderr,
            )
            refined_end = coarse_end

        return (refined_start, refined_end)


def main():
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <video_path> <reference_image>", file=sys.stderr)
        sys.exit(1)

    video_path = sys.argv[1]
    reference_path = sys.argv[2]

    for p in [video_path, reference_path]:
        if not Path(p).exists():
            print(f"Error: file not found: {p}", file=sys.stderr)
            sys.exit(1)

    # Step 1: Find silent regions
    print(f"Detecting silence in {video_path}...", file=sys.stderr)
    silence_regions = detect_silence(video_path)
    print(f"Found {len(silence_regions)} silent regions", file=sys.stderr)

    # Step 2: Refine each silence region with visual boundary detection
    bumpers = []
    for i, (start, end) in enumerate(silence_regions):
        print(f"Refining silence region {i+1}: {start:.1f}s - {end:.1f}s", file=sys.stderr)
        result = refine_visual_boundaries(video_path, start, end, reference_path)
        if result is not None:
            visual_start, visual_end = result
            bumpers.append({"visual_start": visual_start, "visual_end": visual_end})
            print(f"  Bumper found: {visual_start:.1f}s - {visual_end:.1f}s", file=sys.stderr)
        else:
            print(f"  No bumper detected in this region", file=sys.stderr)

    # Output JSON to stdout
    json.dump(bumpers, sys.stdout, indent=2)
    print()  # trailing newline


if __name__ == "__main__":
    main()
