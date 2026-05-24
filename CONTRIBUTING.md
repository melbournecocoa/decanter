# Contributing to Decanter

Thanks for your interest. Decanter is a community pipeline built primarily to
run Melbourne CocoaHeads' own talk archive — it's shared in case other
communities find it useful or want to help improve it, not as a polished
turnkey product.

## Scope and Support

Decanter is maintained on a best-effort basis by Melbourne CocoaHeads
volunteers. Expect:

- **No SLA on issues or PRs.** Responses will be sporadic.
- **No promise of backwards compatibility.** The pipeline evolves to suit our
  meetup format; breaking changes happen.
- **Limited support for forks.** Happy to chat about how it works, but we
  can't run your pipeline for you.

If you need a production-grade video pipeline, this isn't it. If you want a
working reference for splitting/transcribing/uploading long-form recordings
on a Temporal backbone, you're in the right place.

## What's In Scope

Bug reports and PRs are welcome for:

- Fixing actual bugs in the pipeline.
- Improving reliability, idempotency, error handling.
- Better defaults that work across community setups (not just ours).
- Documentation improvements.
- Test coverage.

## What's Out of Scope

- New pipeline steps that only make sense for one community's format.
- Heavy abstractions to support arbitrary video layouts.
- Wrapping the worker in a UI / hosted service.

If you want any of the above, fork freely — that's what the MIT licence is
for.

## Brand Assets

The `assets/` directory contains the Melbourne CocoaHeads visual identity:

- `assets/bumper_reference.png` — the bumper still used by `DetectBumpers`
  for visual verification.
- `assets/intro.m4v`, `assets/intro-2021.m4v`, `assets/outro.m4v` — the
  rendered intro/outro clips concatenated into final talk videos.

**These assets are the intellectual property of Melbourne CocoaHeads Inc.**
and are included so the pipeline runs out-of-the-box for our use. They are
NOT covered by the MIT licence on the code.

If you fork Decanter for your own community:

- **Replace the assets** with your own bumper/intro/outro. Don't ship videos
  with the Melbourne CocoaHeads brand on them.
- The `DECANTER_BUMPER_REF_IMAGE`, `DECANTER_INTRO_VIDEO`, and
  `DECANTER_OUTRO_VIDEO` env vars let you point at your own files without
  touching code.

## Development

```bash
go build ./...
go test ./...
```

Python scripts in `scripts/` have their own dependencies (see
`requirements.txt`).

For end-to-end testing you'll need a running Temporal server — see the
[Temporal docs](https://docs.temporal.io/self-hosted-guide) for a self-hosted
setup.

## Submitting Changes

1. Open an issue first for anything non-trivial — saves you the round-trip
   if the change isn't a fit.
2. Keep PRs focused. One concern per PR.
3. Tests for new behaviour; update existing tests when changing behaviour.
4. `go fmt` and `go vet` clean.
5. Match the existing code style — no large refactors bundled into feature
   PRs.

## Code of Conduct

Be decent. We're a community. If you'd be uncomfortable saying it at a
CocoaHeads meetup, don't say it here.
