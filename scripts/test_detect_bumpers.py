"""Unit tests for detect_bumpers boundary refinement helpers."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import detect_bumpers as db


def test_is_plateau_empty_returns_false():
    assert db.is_plateau([]) is False


def test_is_plateau_underfilled_window_returns_false():
    samples = [(0.1, 80), (0.2, 80), (0.3, 80)]
    assert len(samples) < db.PLATEAU_WINDOW
    assert db.is_plateau(samples) is False


def test_is_plateau_spread_above_delta_returns_false():
    spread = db.PLATEAU_DELTA + 1
    samples = [(0.1 * i, db.PLATEAU_HIGH + (i % 2) * spread) for i in range(db.PLATEAU_WINDOW)]
    assert db.is_plateau(samples) is False


def test_is_plateau_min_below_high_returns_false():
    samples = [(0.1 * i, db.PLATEAU_HIGH - 1) for i in range(db.PLATEAU_WINDOW)]
    assert db.is_plateau(samples) is False


def test_is_plateau_stable_high_returns_true():
    samples = [(0.1 * i, db.PLATEAU_HIGH + 10) for i in range(db.PLATEAU_WINDOW)]
    assert db.is_plateau(samples) is True


def test_is_plateau_at_exact_thresholds_returns_true():
    base = db.PLATEAU_HIGH
    samples = [(0.0, base), (0.1, base + db.PLATEAU_DELTA), (0.2, base), (0.3, base + db.PLATEAU_DELTA), (0.4, base)]
    assert db.is_plateau(samples) is True


def test_is_plateau_only_last_window_evaluated():
    # Early junk samples should not disqualify a later plateau.
    junk = [(0.1, 0), (0.2, 5), (0.3, 10)]
    plateau = [(0.4 + 0.1 * i, db.PLATEAU_HIGH + 5) for i in range(db.PLATEAU_WINDOW)]
    assert db.is_plateau(junk + plateau) is True


def _linear_fade_then_plateau(fade_end_t: float, plateau_distance: int):
    """Returns a probe callable simulating a linear fade from distance=0 at t=0
    to distance=plateau_distance at t=fade_end_t, then stable beyond."""
    def probe(t: float) -> int:
        if t <= 0:
            return 0
        if t >= fade_end_t:
            return plateau_distance
        return int(plateau_distance * (t / fade_end_t))
    return probe


def test_walk_forward_finds_plateau_after_fade():
    probe = _linear_fade_then_plateau(fade_end_t=1.0, plateau_distance=80)
    result = db.walk_to_plateau(probe, start_t=0.0, direction=+1)
    assert result is not None
    # Plateau begins at t=1.0 (first stable sample at plateau distance).
    assert result == pytest_approx(1.0)


def test_walk_backward_symmetric():
    # Fade from plateau at t <= 4.0 (pre-bumper speaker shot) up to 0 at t = 5.0
    # (deep into bumper). Walking backward from coarse boundary at t=5.0.
    def probe(t: float) -> int:
        if t >= 5.0:
            return 0
        if t <= 4.0:
            return 80
        return int(80 * (5.0 - t))
    result = db.walk_to_plateau(probe, start_t=5.0, direction=-1)
    assert result is not None
    assert result == pytest_approx(4.0)


def test_walk_returns_none_when_no_plateau_within_max_walk():
    # Monotonically climbing distance — never stabilises.
    def probe(t: float) -> int:
        return int(t * 20)
    result = db.walk_to_plateau(probe, start_t=0.0, direction=+1)
    assert result is None


def test_walk_returns_none_when_plateau_below_high():
    # Plateau exists, but at a distance below PLATEAU_HIGH (false-plateau guard).
    plateau_distance = db.PLATEAU_HIGH - 5
    probe = _linear_fade_then_plateau(fade_end_t=0.5, plateau_distance=plateau_distance)
    result = db.walk_to_plateau(probe, start_t=0.0, direction=+1)
    assert result is None


def test_walk_backward_clamps_at_zero():
    # start_t close to zero should not probe negative t.
    probe_calls: list[float] = []
    def probe(t: float) -> int:
        probe_calls.append(t)
        return 0
    result = db.walk_to_plateau(probe, start_t=0.2, direction=-1)
    assert result is None
    assert all(t >= 0 for t in probe_calls)


def test_walk_forward_with_fast_fade():
    # Faster fade should still plateau at the fade-end timestamp.
    probe = _linear_fade_then_plateau(fade_end_t=0.4, plateau_distance=60)
    result = db.walk_to_plateau(probe, start_t=0.0, direction=+1)
    assert result is not None
    assert result == pytest_approx(0.4)


def pytest_approx(value: float, tol: float = 1e-6):
    """Tiny approx helper to avoid importing pytest.approx (keeps test
    deterministic with FINE_STEP-aligned timestamps)."""
    class _Approx:
        def __eq__(self, other):
            return abs(other - value) <= max(tol, db.FINE_STEP / 2)
        def __repr__(self):
            return f"approx({value})"
    return _Approx()
