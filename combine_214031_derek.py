#!/usr/bin/env python3
"""
One-off: stitch Derek Clarkson's two-part talk in run decanter-yt-20260610-214031
into a single video + captions + chapters, then upload via UploadOnlyWorkflow.

Why this exists: a catering break mid-talk left a ~5-min bumper between seg-01
(first half) and seg-02 (second half). Split already excised the break (it's the
bumper *between* the segments). The pipeline's Assemble can only cut ONE
contiguous source range per video, so it can't represent a two-range talk. This
script mirrors Assemble for two ranges (hard cut at the join, 0.3s xfades on the
intro/outro ends), merges the two cleaned SRTs and the two chapter lists into the
combined-content timeline, writes everything to processed/combined/, and drives
the upload through UploadOnlyWorkflow with a hand-built AssembledVideo so
BuildChapters gets the *combined* params (StartOffset=0, ContentDuration=sum).

USAGE
  python3 combine_214031_derek.py            # BUILD only: final.mp4 + final.srt + metadata.json
  python3 combine_214031_derek.py --upload   # ALSO fire UploadOnlyWorkflow (irreversible!)

PRECONDITIONS for --upload
  * The main decanter-yt-20260610-214031 PipelineWorkflow run is CLOSED/Completed
    (UploadOnly reuses the same workflow id; Temporal id-reuse needs the prior run
    finished). seg-03/seg-04 should have uploaded through the normal pipeline first.
  * seg-01 and seg-02 were skipped at the review gate so the pipeline did NOT
    upload half-talks.
"""

import argparse
import json
import os
import shutil
import subprocess
import sys

# ----------------------------------------------------------------------------
# Run-specific configuration
# ----------------------------------------------------------------------------
RUN_ID = "decanter-yt-20260610-214031"
WORKSPACE_BASE = os.environ.get("DECANTER_WORKSPACE_PATH", "workspace")
TEMPORAL_ADDRESS = os.environ.get("TEMPORAL_ADDRESS", "miyuki:7233")
TASK_QUEUE = "decanter-pipeline"

# Assemble constants (must match activity/assemble.go)
FPS = 30
XFADE = 0.3
THUMB_OFFSET = 3.0
LOUDNORM = "loudnorm=I=-14:LRA=11:TP=-1.5"

# Intro/outro: resolveIntroPath picks assets/intro-<year>.m4v by recordingDate
# year (2022-11-10 -> 2022). Base assets/intro.m4v, assets/outro.m4v.
INTRO = "assets/intro-2022.m4v"
OUTRO = "assets/outro.m4v"

# Per-half geometry, from the Split result payload + each half's curated trim.
# rough_start_in_source = Segment.Start - Segment.StartOffset ; src_start = rough + trim.start
HALVES = [
    {  # seg-01, first half
        "seg": 1,
        "seg_start": 1524.0933329999998,
        "start_offset": 4.093332999999802,
        "trim_start": 13.093332999999802,
        "trim_end": 1074.3654379999998,
    },
    {  # seg-02, second half
        "seg": 2,
        "seg_start": 2925.665438,
        "start_offset": 2.9987710000000334,
        "trim_start": 25.0,
        "trim_end": 1572.8356870000002,
    },
]

# ----------------------------------------------------------------------------
# Paths
# ----------------------------------------------------------------------------
WS = os.path.join(WORKSPACE_BASE, RUN_ID)
SOURCE = os.path.join(WS, "source.mp4")
OUT_DIR = os.path.join(WS, "processed", "combined")
OUT_VIDEO = os.path.join(OUT_DIR, "final.mp4")
OUT_SRT = os.path.join(OUT_DIR, "final.srt")
OUT_META = os.path.join(OUT_DIR, "metadata.json")
OUT_THUMB = os.path.join(OUT_DIR, "thumbnail.jpg")
# Relative paths for the AssembledVideo (Upload joins them onto wsDir)
REL_VIDEO = "processed/combined/final.mp4"
REL_SRT = "processed/combined/final.srt"


def ffprobe_duration(path):
    out = subprocess.check_output(
        ["ffprobe", "-v", "error", "-show_entries", "format=duration",
         "-of", "default=nk=1:nw=1", path])
    return float(out.strip())


# ----------------------------------------------------------------------------
# SRT helpers — replicate activity/srt.go AdjustSRT exactly, with an extra
# additive offset for the second half.
# ----------------------------------------------------------------------------
def parse_srt_time(s):
    hh, mm, rest = s.split(":")
    sec = float(rest.replace(",", "."))
    return int(hh) * 3600 + int(mm) * 60 + sec


def fmt_srt_time(seconds):
    if seconds < 0:
        seconds = 0
    total_ms = int(seconds * 1000 + 0.5)
    ms = total_ms % 1000
    total_sec = total_ms // 1000
    s = total_sec % 60
    total_min = total_sec // 60
    m = total_min % 60
    h = total_min // 60
    return f"{h:02d}:{m:02d}:{s:02d},{ms:03d}"


def parse_srt(text):
    text = text.replace("\r\n", "\n").strip("\n")
    entries = []
    for block in text.split("\n\n"):
        block = block.strip("\n")
        if not block:
            continue
        lines = block.split("\n")
        # lines[0] = index (discarded), lines[1] = timerange, rest = text
        a, b = lines[1].split("-->")
        start = parse_srt_time(a.strip())
        end = parse_srt_time(b.strip())
        entries.append((start, end, lines[2:]))
    return entries


def adjust_srt(entries, start_offset, intro_shift, content_dur, extra_offset):
    """Mirror of AdjustSRT: shift rough-segment time -> final time, clip to the
    content window, then add extra_offset (0 for half1, dur1 for half2)."""
    content_end = intro_shift + content_dur
    kept = []
    for (s, e, text) in entries:
        ws = s - start_offset
        we = e - start_offset
        if we < 0 or ws > content_dur:
            continue
        fs = ws + intro_shift
        fe = we + intro_shift
        if fs < intro_shift:
            fs = intro_shift
        if fe > content_end:
            fe = content_end
        kept.append((fs + extra_offset, fe + extra_offset, text))
    return kept


def format_srt(entries):
    buf = []
    for i, (s, e, text) in enumerate(entries, start=1):
        buf.append(str(i))
        buf.append(f"{fmt_srt_time(s)} --> {fmt_srt_time(e)}")
        buf.extend(text)
        buf.append("")
    return "\n".join(buf) + "\n"


# ----------------------------------------------------------------------------
# Build
# ----------------------------------------------------------------------------
def build():
    # ---- preconditions ----
    for p in (SOURCE, INTRO, OUTRO):
        if not os.path.exists(p):
            sys.exit(f"missing required input: {p}")
    meta_paths, srt_paths = [], []
    for h in HALVES:
        mp = os.path.join(WS, "processed", f"segment-{h['seg']:02d}", "metadata.json")
        sp = os.path.join(WS, "processed", f"segment-{h['seg']:02d}", "transcript_clean.srt")
        for p in (mp, sp):
            if not os.path.exists(p):
                sys.exit(f"missing required input: {p}")
        meta_paths.append(mp)
        srt_paths.append(sp)

    intro_dur = ffprobe_duration(INTRO)
    intro_shift = intro_dur - XFADE

    # ---- geometry ----
    for h in HALVES:
        h["rough"] = h["seg_start"] - h["start_offset"]
        h["src_start"] = h["rough"] + h["trim_start"]
        h["content_dur"] = h["trim_end"] - h["trim_start"]
    dur1 = HALVES[0]["content_dur"]
    dur2 = HALVES[1]["content_dur"]
    combined_content = dur1 + dur2

    print(f"intro={intro_dur:.6f}s intro_shift={intro_shift:.6f}s")
    print(f"half1 src_start={HALVES[0]['src_start']:.6f} dur={dur1:.6f}")
    print(f"half2 src_start={HALVES[1]['src_start']:.6f} dur={dur2:.6f}")
    print(f"combined content={combined_content:.6f}s "
          f"(final video ~= {intro_dur + combined_content + ffprobe_duration(OUTRO) - 2*XFADE:.1f}s)")

    os.makedirs(OUT_DIR, exist_ok=True)

    # ---- video + audio (mirror of Assemble's filter_complex, two content ----
    #      inputs hard-concatenated, 0.3s xfades on the intro/outro ends) ----
    intro_xfade_offset = intro_dur - XFADE
    ic_xfade_offset = intro_dur + combined_content - 2 * XFADE
    tb = f"settb=1/{FPS}000"
    vnorm = f"format=yuv420p,setsar=1,fps={FPS},{tb}"
    fc = ";".join([
        f"[0:v]{vnorm}[v0]",
        f"[1:v]{vnorm}[v1]",
        f"[2:v]{vnorm}[v2]",
        f"[3:v]{vnorm}[v3]",
        # hard cut at the break; concat RESETS timebase to 1/1e6, so re-apply
        # settb=1/30000 or the following xfade rejects the mismatched pad.
        f"[v1][v2]concat=n=2:v=1:a=0,settb=1/{FPS}000[vc]",
        f"[v0][vc]xfade=transition=fade:duration={XFADE}:offset={intro_xfade_offset:.6f}[vic]",
        f"[vic][v3]xfade=transition=fade:duration={XFADE}:offset={ic_xfade_offset:.6f}[outv]",
        # aresample=48000 after loudnorm: loudnorm outputs 192kHz, concat needs a match
        f"[0:a]{LOUDNORM},aresample=48000[a0]",
        f"[1:a]{LOUDNORM},aresample=48000[a1]",
        f"[2:a]{LOUDNORM},aresample=48000[a2]",
        f"[3:a]{LOUDNORM},aresample=48000[a3]",
        "[a1][a2]concat=n=2:v=0:a=1[ac]",
        f"[a0][ac]acrossfade=d={XFADE}:c1=tri:c2=tri[aic]",
        f"[aic][a3]acrossfade=d={XFADE}:c1=tri:c2=tri[outa]",
    ])
    cmd = [
        "ffmpeg", "-y",
        "-i", INTRO,
        "-ss", f"{HALVES[0]['src_start']:.6f}", "-t", f"{dur1:.6f}", "-i", SOURCE,
        "-ss", f"{HALVES[1]['src_start']:.6f}", "-t", f"{dur2:.6f}", "-i", SOURCE,
        "-i", OUTRO,
        "-filter_complex", fc,
        "-map", "[outv]", "-map", "[outa]",
        "-c:v", "libx264", "-preset", "medium", "-crf", "20",
        "-pix_fmt", "yuv420p", "-profile:v", "high", "-level", "4.0",
        "-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2",
        "-movflags", "+faststart",
        OUT_VIDEO,
    ]
    print("\n=== ffmpeg assemble ===")
    subprocess.run(cmd, check=True)

    # ---- thumbnail (best effort, like Assemble) ----
    try:
        subprocess.run([
            "ffmpeg", "-y",
            "-ss", f"{HALVES[0]['src_start'] + THUMB_OFFSET:.6f}", "-i", SOURCE,
            "-frames:v", "1", "-update", "1",
            "-vf", "scale=1280:720:flags=lanczos", "-q:v", "2", OUT_THUMB,
        ], check=True)
    except subprocess.CalledProcessError:
        print("thumbnail extraction failed — continuing (YouTube will auto-pick)")

    # ---- merged SRT ----
    h1 = adjust_srt(parse_srt(open(srt_paths[0]).read()),
                    HALVES[0]["trim_start"], intro_shift, dur1, 0.0)
    h2 = adjust_srt(parse_srt(open(srt_paths[1]).read()),
                    HALVES[1]["trim_start"], intro_shift, dur2, dur1)
    open(OUT_SRT, "w").write(format_srt(h1 + h2))
    print(f"\nmerged SRT: {len(h1)} + {len(h2)} = {len(h1)+len(h2)} cues -> {OUT_SRT}")

    # ---- merged metadata (seg-01 canonical; chapters in content-local time) ----
    meta = json.load(open(meta_paths[0]))                 # seg-01 = source of truth
    seg2 = json.load(open(meta_paths[1]))
    chapters = []
    for c in meta.get("chapters", []):                    # half1: content-local = t - trim1.start
        chapters.append({"time": c["time"] - HALVES[0]["trim_start"], "title": c["title"]})
    for c in seg2.get("chapters", []):                    # half2: + dur1
        chapters.append({"time": (c["time"] - HALVES[1]["trim_start"]) + dur1, "title": c["title"]})
    meta["chapters"] = chapters
    meta["skip"] = False
    meta.pop("trim", None)                                # Upload ignores trim; content is pre-cut
    json.dump(meta, open(OUT_META, "w"), indent=2, ensure_ascii=False)
    print(f"merged metadata -> {OUT_META}")
    print("chapters (content-local; BuildChapters adds intro_shift + Intro@0/Outro@end):")
    for c in chapters:
        print(f"   {c['time']:8.1f}s  {c['title']!r}")

    # ---- the upload command (printed; run with --upload or paste manually) ----
    assembled = {
        "SegmentIndex": 1,
        "VideoPath": REL_VIDEO,
        "SubtitlePath": REL_SRT,
        "IntroDuration": round(intro_dur, 6),
        "ContentDuration": round(combined_content, 6),
        "StartOffset": 0,
        "XfadeDuration": XFADE,
    }
    payload = json.dumps({"Videos": [assembled]})
    return payload


def upload(payload):
    cmd = [
        "temporal", "--address", TEMPORAL_ADDRESS, "workflow", "start",
        "--task-queue", TASK_QUEUE,
        "--workflow-id", RUN_ID,
        "--type", "UploadOnlyWorkflow",
        "--input", payload,
    ]
    print("\n=== UploadOnlyWorkflow ===")
    print(" ".join(f"'{c}'" if " " in c or "{" in c else c for c in cmd))
    subprocess.run(cmd, check=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--upload", action="store_true",
                    help="fire UploadOnlyWorkflow after building (main run must be CLOSED)")
    args = ap.parse_args()

    payload = build()

    print("\n--- UploadOnly payload (run when the main 214031 run has closed) ---")
    print(payload)
    print("\nTo upload:  python3 combine_214031_derek.py --upload")

    if args.upload:
        upload(payload)
    else:
        print("\n[build-only] skipped upload. Re-run with --upload to push to YouTube.")


if __name__ == "__main__":
    main()
