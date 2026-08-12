const app = document.getElementById('app');
const api = (p, opts) => fetch(p, opts).then(async r => {
  const t = await r.text();
  const body = t ? JSON.parse(t) : null;
  if (!r.ok) throw new Error((body && body.error) || r.statusText);
  return body;
});
const toast = (msg) => {
  const el = document.createElement('div'); el.className = 'toast'; el.textContent = msg;
  document.body.appendChild(el); setTimeout(() => el.remove(), 2500);
};
const esc = (s) => (s || '').replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));

// runDate parses the yyyymmdd-hhmmss stamp out of a workflow id into "D Mon YYYY".
function runDate(wf) {
  const m = /(\d{4})(\d{2})(\d{2})-\d{6}/.exec(wf || '');
  if (!m) return '';
  const mo = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
  return `${+m[3]} ${mo[+m[2] - 1]} ${m[1]}`;
}

// stateChip maps a run state to a status chip {cls, label}.
function stateChip(state) {
  const s = (state || '').replace(/_gate$/, '');
  if (state === 'review_gate') return { cls: 'chip--gate', label: 'ready for review' };
  if (state === 'upload_gate') return { cls: 'chip--gate', label: 'ready for upload' };
  if (s === 'running')   return { cls: 'chip--running', label: 'running' };
  if (s === 'completed') return { cls: 'chip--completed', label: 'completed' };
  if (s === 'failed' || s === 'terminated') return { cls: 'chip--failed', label: s };
  return { cls: 'chip--unknown', label: s || 'unknown' };
}

// emptyState renders a glassware line-art placeholder with a literal message.
function emptyState(msg) {
  return `<div class="empty">
    <svg viewBox="0 0 24 24" fill="none" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">
      <path d="M9 3h6M10 3v4.2L5.4 15.1A3 3 0 0 0 8 20h8a3 3 0 0 0 2.6-4.9L14 7.2V3"/><path d="M7.2 14h9.6"/>
    </svg>
    <p>${esc(msg)}</p></div>`;
}

// mdInline applies inline markdown (code, bold, italic, links) to an
// already-HTML-escaped string. Code spans are stashed first so their contents
// aren't touched by the bold/italic passes.
function mdInline(s) {
  const codes = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return `\x00${codes.length - 1}\x00`; });
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
       .replace(/\*([^*]+)\*/g, '<em>$1</em>')
       .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  return s.replace(/\x00(\d+)\x00/g, (_, n) => `<code>${codes[+n]}</code>`);
}

// mdToHtml is a compact markdown renderer for the metadata_reasoning.md sidecar:
// ATX headings, unordered lists, horizontal rules, paragraphs, and inline marks.
// Source is HTML-escaped up front so the output is safe to inject.
function mdToHtml(src) {
  const lines = esc(src).split(/\r?\n/);
  const out = [];
  let listOpen = false;
  const closeList = () => { if (listOpen) { out.push('</ul>'); listOpen = false; } };
  const isBlank = (l) => /^\s*$/.test(l);
  const isSpecial = (l) => /^\s*---+\s*$/.test(l) || /^#{1,6}\s+/.test(l) || /^\s*[-*]\s+/.test(l);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^\s*---+\s*$/.test(line)) { closeList(); out.push('<hr>'); continue; }
    const h = line.match(/^(#{1,6})\s+(.*)$/);
    if (h) { closeList(); out.push(`<h${h[1].length}>${mdInline(h[2])}</h${h[1].length}>`); continue; }
    const li = line.match(/^\s*[-*]\s+(.*)$/);
    if (li) { if (!listOpen) { out.push('<ul>'); listOpen = true; } out.push(`<li>${mdInline(li[1])}</li>`); continue; }
    if (isBlank(line)) { closeList(); continue; }
    closeList();
    const para = [line];
    while (i + 1 < lines.length && !isBlank(lines[i + 1]) && !isSpecial(lines[i + 1])) para.push(lines[++i]);
    out.push(`<p>${mdInline(para.join(' '))}</p>`);
  }
  closeList();
  return out.join('\n');
}

let _pollTimer = null;
let _pollWf = null;
const POLL_MS = 2000;
const POLL_TERMINAL = new Set(['review_gate', 'upload_gate', 'completed', 'failed', 'terminated']);

function stopStatusPoll() {
  if (_pollTimer) { clearTimeout(_pollTimer); _pollTimer = null; }
  _pollWf = null;
  setConn('');
}
function setConn(text) {
  const c = document.getElementById('conn');
  if (c) c.textContent = text;
}

async function pollOnce(wf) {
  if (_pollWf !== wf) return; // route changed
  let rs;
  try { rs = await api(`/api/runs/${encodeURIComponent(wf)}/status`); }
  catch { setConn('· status unavailable'); schedulePoll(wf); return; }
  if (_pollWf !== wf) return;

  renderRunningBanner(wf, rs);
  (rs.segments || []).forEach(s => renderSegStatus(wf, s));

  if (POLL_TERMINAL.has(rs.state)) { setConn(''); _pollTimer = null; _pollWf = null; return; }
  setConn('● live');
  schedulePoll(wf);
}
function schedulePoll(wf) {
  if (document.hidden) { _pollTimer = null; return; } // resumed by visibilitychange
  _pollTimer = setTimeout(() => pollOnce(wf), POLL_MS);
}
function startStatusPoll(wf) {
  stopStatusPoll();
  _pollWf = wf;
  pollOnce(wf);
}

document.addEventListener('visibilitychange', () => {
  if (!document.hidden && _pollWf && !_pollTimer) pollOnce(_pollWf);
});

// renderRunningBanner only touches the banner while NOT at a gate (the gate
// banner is owned by renderGateBanner and must not be overwritten).
function renderRunningBanner(wf, rs) {
  if (POLL_TERMINAL.has(rs.state)) { renderGateBannerFromState(wf, rs); return; }
  const el = document.getElementById('run-banner');
  if (!el) return;
  const done = (rs.segments || []).filter(s => s.phase === 'done' || s.phase === 'uploaded').length;
  const total = (rs.segments || []).filter(s => s.phase !== 'skipped').length;
  el.className = 'banner';
  el.innerHTML = `<span class="banner-phase">⟳ ${esc((rs.phase || 'running').replace(/_/g, ' '))}</span>
    <span class="banner-count">${done}/${total} done</span>`;
}

// If the poll observes we've reached a gate, re-render the gate banner by
// re-fetching the run detail (cheap, and gives us the segment summary counts).
async function renderGateBannerFromState(wf, rs) {
  if (rs.state !== 'review_gate' && rs.state !== 'upload_gate') {
    const el = document.getElementById('run-banner');
    if (el) { el.className = 'banner'; el.innerHTML = `<span class="banner-phase">${esc(rs.state.replace(/_/g, ' '))}</span>`; }
    return;
  }
  try {
    const d = await api('/api/runs/' + encodeURIComponent(wf));
    if (_pollWf !== wf) return; // navigated away while fetching
    renderGateBanner(wf, d);
  } catch { /* leave as-is */ }
}

function dots(step) {
  let s = '';
  const labels = ['Classify', 'Transcribe', 'Clean', 'Meta'];
  for (let i = 1; i <= 4; i++) s += `<span class="dot ${i <= step.current ? 'on' : ''}">●</span>${i < 4 ? '<span class="dot-sep">─</span>' : ''}`;
  return `<span class="dots">${s}</span><span class="dots-label">${esc(labels[step.current - 1] || '')}</span>`;
}

function bar(percent, detail) {
  const p = Math.max(0, Math.min(100, percent));
  return `<span class="pbar"><span class="pbar-fill" style="width:${p}%"></span></span>
    <span class="pbar-pct">${p}%</span>${detail ? `<span class="pbar-detail">${esc(detail)}</span>` : ''}`;
}

function renderSegStatus(wf, s) {
  const el = document.getElementById(`seg-status-${s.index}`);
  if (!el) return;
  if (s.step) { el.innerHTML = dots(s.step); return; }
  if (s.percent != null) {
    const label = s.phase === 'uploading' ? 'Upload' : 'Assemble';
    el.innerHTML = `<span class="status-label">${label}</span> ${bar(s.percent, s.detail)}`;
    return;
  }
  const glyph = { done: '✓', uploaded: '✓', skipped: '', queued: '· queued' }[s.phase] || '';
  el.innerHTML = glyph ? `<span class="status-glyph">${glyph}</span>` : '';

  // reveal the upload-preview button the moment a final appears mid-run
  if (s.hasFinal) {
    const row = document.getElementById(`seg-row-${s.index}`);
    if (row && !row.querySelector('.up-link') && s.phase !== 'skipped') {
      const main = row.querySelector('.seg-main');
      if (!main) return;
      const a = document.createElement('a');
      a.className = 'btn btn-sm up-link';
      a.href = `#/run/${encodeURIComponent(wf)}/seg/${s.index}/upload`;
      a.textContent = '▸ upload preview';
      main.appendChild(a);
    }
  }
}

async function routeRuns() {
  const runs = await api('/api/runs');
  if (!runs.length) {
    app.innerHTML = `<h2>Runs</h2>${emptyState('No runs in the workspace yet.')}`;
    return;
  }
  app.innerHTML = `<h2>Runs</h2><div class="ledger">${runs.map(r => {
    const ch = stateChip(r.state);
    return `<a class="ledger-row${r.state === 'completed' ? ' is-done' : ''}" href="#/run/${encodeURIComponent(r.workflowId)}">
      <span class="ledger-edge"></span>
      <span class="ledger-body">
        <span class="ledger-no">${esc(r.workflowId)}</span>
        <span class="ledger-title">${esc(r.eventName || r.workflowId)}</span>
        <span class="ledger-meta" data-wf="${esc(r.workflowId)}"><span>${runDate(r.workflowId)}</span></span>
      </span>
      <span class="ledger-aside">
        <span class="chip ${ch.cls}">${esc(ch.label)}</span>
        <span class="ledger-step" data-wf="${esc(r.workflowId)}"></span>
      </span>
    </a>`;
  }).join('')}</div>`;

  // progressive enrichment: fill talk/skip counts, and a live step for running rows
  runs.forEach(async r => {
    try {
      const d = await api('/api/runs/' + encodeURIComponent(r.workflowId));
      const talks = d.segments.filter(s => s.type === 'talk' && !s.skip).length;
      const skipped = d.segments.filter(s => s.skip).length;
      const meta = document.querySelector(`.ledger-meta[data-wf="${CSS.escape(r.workflowId)}"]`);
      if (meta) meta.innerHTML = `<span>${runDate(r.workflowId)}</span>`
        + `<span class="sep">·</span><span><b>${talks}</b> talk${talks === 1 ? '' : 's'}</span>`
        + (skipped ? `<span class="sep">·</span><span><b>${skipped}</b> skipped</span>` : '');
    } catch { /* leave the date-only meta */ }
    if (r.status === 'Running') {
      try {
        const rs = await api(`/api/runs/${encodeURIComponent(r.workflowId)}/status`);
        const el = document.querySelector(`.ledger-step[data-wf="${CSS.escape(r.workflowId)}"]`);
        if (el) el.textContent = phaseLine(rs);
      } catch { /* ignore */ }
    }
  });
}
function phaseLine(rs) {
  const done = (rs.segments || []).filter(s => s.phase === 'done' || s.phase === 'uploaded').length;
  const total = (rs.segments || []).filter(s => s.phase !== 'skipped').length;
  if (rs.phase === 'assembling' || rs.phase === 'uploading') return `${rs.phase} ${done}/${total}`;
  return (rs.phase || '').replace(/_/g, ' ');
}

async function routeRun(wf) {
  const d = await api('/api/runs/' + encodeURIComponent(wf));
  const rows = d.segments.map(s => {
    const idx = String(s.index).padStart(2, '0');
    const badge = s.skip ? 'skip' : s.type;
    const isTalk = s.type === 'talk' && !s.skip;
    const upBtn = (isTalk && s.hasFinal)
      ? `<a class="btn btn-sm up-link" href="#/run/${encodeURIComponent(wf)}/seg/${s.index}/upload">▸ upload preview</a>`
      : '';
    return `<div class="seg-row" id="seg-row-${s.index}">
      <div class="seg-main">
        <span class="seg-idx">${idx}</span>
        <span class="badge b-${badge}">${badge}</span>
        <a class="seg-title" href="#/run/${encodeURIComponent(wf)}/seg/${s.index}">${esc(s.title || '(untitled)')}</a>
        ${upBtn}
      </div>
      <div class="seg-status" id="seg-status-${s.index}"></div>
    </div>`;
  }).join('');

  app.innerHTML = `
    <h2>${esc(d.event.eventName || wf)}</h2>
    <p class="run-sub">${esc(wf)}</p>
    <div id="run-banner" class="banner"></div>
    <div class="run-grid">
      <section class="run-main">
        <h3>Segments</h3>
        <div class="seg-rows">${rows}</div>
      </section>
      <aside class="run-aside">
        <div id="bumpers-panel"></div>
        <div id="reset-panel"></div>
      </aside>
    </div>`;

  renderGateBanner(wf, d);
  renderBumpersPanel(wf, d);
  renderResetPanel(wf, d);
  startStatusPoll(wf); // defined in Task 7; harmless no-op stub until then
}

// gateSummary returns "{N} talks → Assemble, {M} skipped" style counts.
function gateSummary(segments) {
  const talks = segments.filter(s => s.type === 'talk' && !s.skip).length;
  const skipped = segments.filter(s => s.skip).length;
  return { talks, skipped };
}

// renderGateBanner shows the dual-purpose banner. At a gate it hosts the
// workflow-level Approve/Reject; otherwise Task 7's poll fills it with status.
function renderGateBanner(wf, d) {
  const el = document.getElementById('run-banner');
  const gate = d.state;
  const { talks, skipped } = gateSummary(d.segments);
  if (gate !== 'review_gate' && gate !== 'upload_gate') {
    el.className = 'banner';
    el.innerHTML = `<span class="banner-phase">${esc((gate || 'running').replace(/_/g, ' '))}</span>`;
    return;
  }
  const isReview = gate === 'review_gate';
  const verb = isReview ? 'Approve review' : 'Approve upload';
  const next = isReview ? 'Assemble' : 'Upload';
  el.className = 'banner banner-gate';
  el.innerHTML = `
    <div class="banner-msg"><strong>Ready for ${isReview ? 'review' : 'upload'}</strong>
      · ${talks} talk${talks === 1 ? '' : 's'} → ${next}${skipped ? `, ${skipped} skipped` : ''}</div>
    <div class="row-actions">
      <button class="btn btn-go" id="gate-approve">✓ ${verb}</button>
      <button class="btn btn-no" id="gate-reject">Reject</button>
    </div>`;

  const gateKey = isReview ? 'review' : 'upload';
  document.getElementById('gate-approve').onclick = () => confirmDialog({
    title: `${verb}?`,
    bodyHTML: `<p>${talks} talk${talks === 1 ? '' : 's'} will proceed to ${next}${skipped ? `; ${skipped} segment${skipped === 1 ? '' : 's'} skipped` : ''}.</p>`,
    confirmLabel: `✓ ${verb}`,
    onConfirm: async () => {
      await api(`/api/runs/${encodeURIComponent(wf)}/approve`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gate: gateKey, approved: true }),
      });
      toast(`${verb} sent`); router();
    },
  });
  document.getElementById('gate-reject').onclick = () => confirmDialog({
    title: `Reject ${gateKey}?`,
    bodyHTML: '<p>Rejecting <strong>fails the entire workflow run</strong> — it does not re-open the gate. You would need to start a new run.</p>',
    confirmLabel: 'Reject & fail run',
    onConfirm: async () => {
      await api(`/api/runs/${encodeURIComponent(wf)}/approve`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gate: gateKey, approved: false }),
      });
      toast(`${gateKey} rejected — run failed`); router();
    },
  });
}

function router() {
  stopStatusPoll();
  const h = location.hash || '#/';
  const m = h.match(/^#\/run\/([^/]+)(?:\/seg\/(\d+)(\/upload)?)?$/);
  document.getElementById('conn').textContent = '';
  if (!m) return routeRuns().catch(e => app.innerHTML = `<p>${esc(e.message)}</p>`);
  const wf = decodeURIComponent(m[1]);
  if (m[2] == null) return routeRun(wf).catch(e => app.innerHTML = `<p>${esc(e.message)}</p>`);
  const idx = parseInt(m[2], 10);
  if (m[3]) return routeUpload(wf, idx).catch(e => app.innerHTML = `<p>${esc(e.message)}</p>`);
  return routeSegment(wf, idx).catch(e => app.innerHTML = `<p>${esc(e.message)}</p>`);
}
window.addEventListener('hashchange', router);
window.addEventListener('DOMContentLoaded', router);

// markBumperAtPlayhead converts the current segment-file playhead time to an
// absolute source time (seg.Start - seg.StartOffset + playhead, matching
// Assemble) and appends a zero-width bumper boundary there, so a missed bumper
// can be fixed without manual coordinate maths. Then the operator runs Redo
// Split from the run overview.
async function markBumperAtPlayhead(wf, idx, video) {
  const segs = await api(`/api/runs/${encodeURIComponent(wf)}/segment-timing`);
  const seg = (segs || []).find(s => s.Index === idx);
  if (!seg) { toast('No segment timing available (is the run still in Temporal?)'); return; }
  const sourceT = seg.Start - seg.StartOffset + video.currentTime;
  const bumpers = await api(`/api/runs/${encodeURIComponent(wf)}/bumpers`);
  bumpers.push({ VisualStart: sourceT, VisualEnd: sourceT });
  await api(`/api/runs/${encodeURIComponent(wf)}/bumpers`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(bumpers) });
  toast(`Bumper boundary added at source ${sourceT.toFixed(2)}s — open the run overview and Redo Split`);
}

// --- Temporary stubs, replaced in later tasks ---
async function routeSegment(wf, idx) {
  const [{ metadata, reasoning }, detail] = await Promise.all([
    api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/metadata`).catch(() => ({ metadata: {}, reasoning: '' })),
    api('/api/runs/' + encodeURIComponent(wf)),
  ]);
  const m = Object.assign({ title:'', speaker:'', description:'', tags:[], chapters:[] }, metadata);
  const titleLen = [...(m.title || '')].length;

  app.innerHTML = `
  <p class="crumb"><a href="#/run/${encodeURIComponent(wf)}">← ${esc(detail.event.eventName || wf)}</a> · segment ${String(idx).padStart(2,'0')}</p>
  <div class="review">
    <div class="player-col">
      <video id="vid" controls preload="metadata" src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/video"></video>
      <div id="timeline"></div>
      <div id="trimreadout" class="readout"></div>
      <button class="btn" id="markbumper" style="margin-top:10px">⚑ Mark bumper boundary at playhead</button>
    </div>
    <div class="panel">
      <div class="field"><label>Title <span id="tc" class="counter ${titleLen>100?'over':''}">${titleLen}/100</span></label>
        <input id="f-title" value="${esc(m.title)}"></div>
      <div class="field"><label>Speaker</label><input id="f-speaker" value="${esc(m.speaker)}"></div>
      <div class="field"><label>Description</label><textarea id="f-desc">${esc(m.description)}</textarea></div>
      <div class="field"><label>Tags (comma separated)</label><input id="f-tags" value="${esc((m.tags||[]).join(', '))}"></div>
      <div class="skip-row"><input type="checkbox" id="f-skip" ${m.skip?'checked':''}><label for="f-skip">Skip this segment</label></div>
      <div class="row-actions">
        <button class="btn btn-save" id="save">Save</button>
      </div>
    </div>
  </div>
  ${reasoning ? `<section class="reason-section"><h3>Reasoning <span class="reason-file">metadata_reasoning.md</span></h3><div class="reason-body md">${mdToHtml(reasoning)}</div></section>` : ''}`;

  const titleEl = document.getElementById('f-title');
  titleEl.addEventListener('input', () => {
    const n = [...titleEl.value].length;
    const c = document.getElementById('tc'); c.textContent = n + '/100'; c.classList.toggle('over', n > 100);
  });

  const collect = () => ({
    title: titleEl.value, speaker: document.getElementById('f-speaker').value,
    description: document.getElementById('f-desc').value,
    tags: document.getElementById('f-tags').value.split(',').map(s => s.trim()).filter(Boolean),
    chapters: m.chapters || [],
    trim: window.currentTrim || m.trim || null,
    skip: document.getElementById('f-skip').checked,
  });
  const save = async () => {
    await api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/metadata`,
      { method:'PUT', headers:{'Content-Type':'application/json'}, body: JSON.stringify(collect()) });
    toast('Saved');
  };
  document.getElementById('save').onclick = () => save().catch(e => toast(e.message));

  document.getElementById('markbumper').onclick = () =>
    markBumperAtPlayhead(wf, idx, document.getElementById('vid')).catch(e => toast(e.message));

  // Park the playhead at the trim start so a single Play verifies the boundary
  // without scrubbing. Wait for metadata (seekable) if it hasn't loaded yet.
  const vid = document.getElementById('vid');
  const startAt = (m.trim && typeof m.trim.startSeconds === 'number') ? m.trim.startSeconds : 0;
  if (startAt > 0) {
    const seekToStart = () => { try { vid.currentTime = startAt; } catch {} };
    if (vid.readyState >= 1) seekToStart();
    else vid.addEventListener('loadedmetadata', seekToStart, { once: true });
  }

  initTimeline({ wf, idx, video: document.getElementById('vid'), mount: document.getElementById('timeline'),
    readout: document.getElementById('trimreadout'), trim: m.trim, chapters: m.chapters || [], onChange: () => save().catch(()=>{}) });
}
async function routeUpload(wf, idx) {
  const { metadata } = await api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/metadata`);
  const detail = await api('/api/runs/' + encodeURIComponent(wf));
  const m = metadata || {};
  app.innerHTML = `
  <p class="crumb"><a href="#/run/${encodeURIComponent(wf)}">← ${esc(detail.event.eventName || wf)}</a> · upload preview · segment ${String(idx).padStart(2,'0')}</p>
  <div class="review">
    <div class="player-col">
      <video id="vid" controls preload="metadata" crossorigin="anonymous">
        <source src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/final" type="video/mp4">
        <track default kind="subtitles" srclang="en" label="English"
               src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/subtitles">
      </video>
      <div class="thumb-block">
        <img id="thumb" class="thumb-preview" src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/thumbnail" alt="thumbnail">
        <button class="btn" id="grabthumb">Use current frame as thumbnail</button>
      </div>
    </div>
    <div class="panel">
      <h3 style="margin-top:0">${esc(m.title || '')}</h3>
      <p class="up-speaker">${esc(m.speaker || '')}</p>
      <p class="up-desc">${esc(m.description || '')}</p>
      <div class="chips">${(m.tags||[]).map(t => `<span>${esc(t)}</span>`).join('')}</div>
      ${(m.chapters||[]).length ? `<div class="up-chapters"><h4>Chapters</h4>${m.chapters.map(c => `<div class="row"><b>${esc(c.title)}</b> — ${Math.floor(c.time/60)}:${String(Math.floor(c.time%60)).padStart(2,'0')}</div>`).join('')}</div>` : ''}
    </div>
  </div>`;

  document.getElementById('grabthumb').onclick = async () => {
    const t = document.getElementById('vid').currentTime;
    await api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/thumbnail`,
      { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ seconds: t }) });
    document.getElementById('thumb').src = `/api/runs/${encodeURIComponent(wf)}/segments/${idx}/thumbnail?ts=${Date.now()}`;
    toast('Thumbnail updated');
  };
}
function confirmDialog({ title, bodyHTML, confirmLabel, onConfirm }) {
  const back = document.createElement('div'); back.className = 'dialog-backdrop';
  back.innerHTML = `<div class="dialog"><h3>${esc(title)}</h3>${bodyHTML}
    <div class="row-actions"><button class="btn btn-go" id="dlg-ok">${esc(confirmLabel)}</button>
    <button class="btn" id="dlg-cancel">Cancel</button></div></div>`;
  document.body.appendChild(back);
  back.querySelector('#dlg-cancel').onclick = () => back.remove();
  back.querySelector('#dlg-ok').onclick = async () => { back.remove(); try { await onConfirm(); } catch (e) { toast(e.message); } };
}

function renderBumpersPanel(wf, detail) {
  const el = document.getElementById('bumpers-panel');
  let list = (detail.bumpers || []).map(b => ({ VisualStart: b.VisualStart, VisualEnd: b.VisualEnd }));
  const rows = list.map((b, i) =>
    `<div>#${i} <input data-i="${i}" data-k="VisualStart" value="${b.VisualStart}" style="width:90px"> →
      <input data-i="${i}" data-k="VisualEnd" value="${b.VisualEnd}" style="width:90px">
      <button class="btn" data-del="${i}">✕</button></div>`).join('');
  el.innerHTML = `<details class="panel"><summary><strong>Bumpers</strong> (edit then Redo Split)</summary>
    <div id="bump-rows">${rows || '<em>none</em>'}</div>
    <div class="row-actions"><button class="btn" id="bump-add">+ Add boundary (source sec)</button>
    <button class="btn" id="bump-save">Save bumpers.json</button></div></details>`;

  const redraw = () => { detail.bumpers = list; renderBumpersPanel(wf, detail); };
  el.querySelectorAll('input').forEach(inp => inp.onchange = () => {
    list[+inp.dataset.i][inp.dataset.k] = parseFloat(inp.value);
  });
  el.querySelectorAll('[data-del]').forEach(b => b.onclick = () => { list.splice(+b.dataset.del, 1); redraw(); });
  document.getElementById('bump-add').onclick = () => { const t = parseFloat(prompt('Source time (seconds):') || '0'); list.push({ VisualStart: t, VisualEnd: t }); redraw(); };
  document.getElementById('bump-save').onclick = async () => {
    try {
      await api(`/api/runs/${encodeURIComponent(wf)}/bumpers`, { method:'PUT', headers:{'Content-Type':'application/json'}, body: JSON.stringify(list) });
      toast('bumpers.json saved');
    } catch (e) { toast(e.message); }
  };
}

// Summarise what the anchor activity will actually read off disk. The bumpers
// panel holds edits in memory until "Save bumpers.json" is pressed, so a
// boundary that was added but not saved is invisible to the worker and the
// reset fails identically. Returns '' for recipes that don't use the sidecar.
function bumperSummaryHTML(prev) {
  if (prev.bumperCount === undefined || prev.bumperCount === null) return '';
  if (!prev.bumperCount) {
    return `<p class="reset-note bad"><strong>No bumpers.json on disk.</strong> Detection will
      run again and fail the same way. Add your boundaries in the Bumpers panel and press
      <em>Save bumpers.json</em> before resetting.</p>`;
  }
  // Source-absolute times, so roll over to h:mm:ss — these get compared against
  // a player's scrubber on a 1-2 hour stream, where "105:30" reads as a typo.
  const clock = (t) => {
    const s = Math.floor(t % 60), m = Math.floor(t / 60) % 60, h = Math.floor(t / 3600);
    const mm = String(m).padStart(2, '0'), ss = String(s).padStart(2, '0');
    return h ? `${h}:${mm}:${ss}` : `${m}:${ss}`;
  };
  const rows = (prev.bumpers || []).map(b => b.visual_start === b.visual_end
    ? `${clock(b.visual_start)} <span class="faint">— ${b.visual_start}s, manual boundary</span>`
    : `${clock(b.visual_start)} → ${clock(b.visual_end)} <span class="faint">— ${b.visual_start}–${b.visual_end}s</span>`
  ).join('<br>');
  // Classify is positional: index 0 is welcome, the last index is wrap-up, and
  // both are dropped. Fewer than 2 boundaries therefore yields no talks at all.
  const segments = prev.bumperCount + 1, talks = segments - 2;
  const verdict = talks < 1
    ? `<p class="reset-note bad"><strong>This produces no talks.</strong> ${segments} segments —
       the first is classified <em>welcome</em> and the last <em>wrap-up</em>, both dropped.
       Add another boundary (a zero-width one near the end of the stream works).</p>`
    : `<p class="reset-note ok">${segments} segments → <strong>${talks} talk${talks === 1 ? '' : 's'}</strong>
       (first is welcome, last is wrap-up, both dropped).</p>`;
  return `<p class="reset-note">Using <strong>${prev.bumperCount}</strong> boundar${prev.bumperCount === 1 ? 'y' : 'ies'} from bumpers.json:</p>
    <div class="cmd">${rows}</div>${verdict}`;
}

function renderResetPanel(wf, detail) {
  const el = document.getElementById('reset-panel');
  el.innerHTML = `<div class="panel"><strong>Recovery resets</strong>
    <div class="row-actions">
      <button class="btn" id="r-split">Redo Split (missed bumper)</button>
      <button class="btn" id="r-asm">Redo Assemble (trim fix)</button>
    </div></div>`;
  const doReset = async (recipe) => {
    const prev = await api(`/api/runs/${encodeURIComponent(wf)}/reset/${recipe}`);
    confirmDialog({
      title: prev.label,
      bodyHTML: `<p>${esc(prev.explanation)}</p>${bumperSummaryHTML(prev)}<p>Resets to event <strong>${prev.targetEventId}</strong>, excluding old signals so the gates re-block.</p>${prev.command ? `<div class="cmd">${esc(prev.command)}</div>` : ''}`,
      confirmLabel: 'Run reset',
      onConfirm: async () => {
        await api(`/api/runs/${encodeURIComponent(wf)}/reset/${recipe}`, { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ targetEventId: prev.targetEventId }) });
        toast('Reset issued'); setTimeout(() => router(), 800);
      },
    });
  };
  document.getElementById('r-split').onclick = () => doReset('redo-split').catch(e => toast(e.message));
  document.getElementById('r-asm').onclick = () => doReset('redo-assemble').catch(e => toast(e.message));
}
