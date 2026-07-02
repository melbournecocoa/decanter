// initTimeline draws a trim bar in segment-file seconds over the video duration.
// Handles drag on the in/out handles, keyboard set-in (i) / set-out (o) at the
// playhead, shows chapter ticks, and writes trim back via onChange (debounced).
let _timelineAbort = null;
function initTimeline(opts) {
  if (_timelineAbort) _timelineAbort.abort();
  _timelineAbort = new AbortController();
  const _sig = _timelineAbort.signal;
  const { video, mount, readout, chapters } = opts;
  let trim = opts.trim ? { ...opts.trim } : null;
  window.currentTrim = trim;

  // Distiller's cut: dimmed/hatched discard zones flank a glowing amber kept
  // region. Styling lives in styles.css (.tl-*); JS only positions the pieces.
  mount.innerHTML = `<div class="tl">
    <div class="tl-cut tl-heads"></div>
    <div class="tl-cut tl-tails"></div>
    <div class="tl-kept"></div>
    <div class="tl-h tl-in"  data-h="in"></div>
    <div class="tl-h tl-out" data-h="out"></div>
    <div class="tl-play"></div>
  </div>`;
  const bar = mount.querySelector('.tl');
  const kept = mount.querySelector('.tl-kept');
  const heads = mount.querySelector('.tl-heads');
  const tails = mount.querySelector('.tl-tails');
  const inH = mount.querySelector('.tl-in');
  const outH = mount.querySelector('.tl-out');
  const play = mount.querySelector('.tl-play');

  const dur = () => video.duration || (trim ? trim.endSeconds : 0) || 1;
  const pct = (t) => Math.max(0, Math.min(1, t / dur())) * 100;
  const fmt = (t) => `${Math.floor(t/60)}:${String(Math.floor(t%60)).padStart(2,'0')}`;

  function ensureTrim() {
    if (!trim) trim = { startSeconds: 0, endSeconds: dur() };
    window.currentTrim = trim;
  }
  function renderChapters() {
    mount.querySelectorAll('.tl-chap').forEach(e => e.remove());
    (chapters || []).forEach(c => {
      const el = document.createElement('div');
      el.className = 'tl-chap'; el.title = c.title;
      el.style.left = pct(c.time) + '%';
      bar.appendChild(el);
    });
  }
  function draw() {
    ensureTrim();
    const inPct = pct(trim.startSeconds), outPct = pct(trim.endSeconds);
    inH.style.left = `calc(${inPct}% - 4px)`;
    outH.style.left = `calc(${outPct}% - 4px)`;
    kept.style.left = inPct + '%';
    kept.style.right = (100 - outPct) + '%';
    heads.style.left = '0'; heads.style.width = inPct + '%';
    tails.style.left = outPct + '%'; tails.style.right = '0';
    readout.textContent = `trim ${fmt(trim.startSeconds)} (${trim.startSeconds.toFixed(2)}s) → ${fmt(trim.endSeconds)} (${trim.endSeconds.toFixed(2)}s)  ·  press i / o to set in/out at playhead`;
  }

  let saveTimer;
  const commit = () => { clearTimeout(saveTimer); saveTimer = setTimeout(() => opts.onChange && opts.onChange(), 400); };

  let dragging = null;
  const xToTime = (clientX) => {
    const rect = bar.getBoundingClientRect();
    return Math.max(0, Math.min(1, (clientX - rect.left) / rect.width)) * dur();
  };
  [inH, outH].forEach(h => h.addEventListener('mousedown', e => { dragging = h.dataset.h; e.preventDefault(); }));
  window.addEventListener('mousemove', e => {
    if (!dragging) return;
    const t = xToTime(e.clientX);
    if (dragging === 'in') trim.startSeconds = Math.min(t, trim.endSeconds - 0.1);
    else trim.endSeconds = Math.max(t, trim.startSeconds + 0.1);
    draw();
  }, { signal: _sig });
  window.addEventListener('mouseup', () => { if (dragging) { dragging = null; commit(); } }, { signal: _sig });

  // click on the bar (not a handle) scrubs the video
  bar.addEventListener('click', e => { if (!e.target.classList.contains('tl-h')) video.currentTime = xToTime(e.clientX); });

  video.addEventListener('timeupdate', () => { play.style.left = pct(video.currentTime) + '%'; });
  video.addEventListener('loadedmetadata', () => { renderChapters(); draw(); });

  window.addEventListener('keydown', e => {
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
    if (e.key === 'i') { ensureTrim(); trim.startSeconds = +video.currentTime.toFixed(3); draw(); commit(); }
    if (e.key === 'o') { ensureTrim(); trim.endSeconds = +video.currentTime.toFixed(3); draw(); commit(); }
  }, { signal: _sig });

  renderChapters(); draw();
}
