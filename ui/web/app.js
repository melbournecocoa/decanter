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

async function routeRuns() {
  const runs = await api('/api/runs');
  app.innerHTML = `<h2>Runs</h2><div class="runs">${runs.map(r => `
    <a class="run-row" href="#/run/${encodeURIComponent(r.workflowId)}">
      <span class="badge b-${r.state.replace('_gate','')}">${r.state.replace('_',' ')}</span>
      <strong>${esc(r.eventName || r.workflowId)}</strong>
      <span style="color:var(--muted)">${esc(r.workflowId)}</span>
    </a>`).join('')}</div>`;
}

async function routeRun(wf) {
  const d = await api('/api/runs/' + encodeURIComponent(wf));
  const strip = d.segments.map(s => {
    const up = s.type === 'talk'
      ? `<a href="#/run/${encodeURIComponent(wf)}/seg/${s.index}/upload" style="display:block;font-size:11px;margin-top:4px">▸ upload preview</a>`
      : '';
    return `<div class="seg-chip">
      <a href="#/run/${encodeURIComponent(wf)}/seg/${s.index}"><strong>${String(s.index).padStart(2,'0')}</strong>
      <span class="badge b-${s.skip ? 'skip' : s.type}">${s.skip ? 'skip' : s.type}</span>
      <div>${esc(s.title || '')}</div></a>${up}</div>`;
  }).join('');
  const gate = d.state;
  app.innerHTML = `<h2>${esc(d.event.eventName || wf)}</h2>
    <p style="color:var(--muted)">${esc(wf)} · <span class="badge b-${gate.replace('_gate','')}">${gate.replace('_',' ')}</span></p>
    <h3>Segments</h3><div class="seg-strip">${strip}</div>
    <div id="bumpers-panel"></div>
    <div id="reset-panel"></div>`;
  renderBumpersPanel(wf, d);
  renderResetPanel(wf, d);
}

function router() {
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

// --- Temporary stubs, replaced in later tasks ---
async function routeSegment(wf, idx) {
  const [{ metadata, reasoning }, detail] = await Promise.all([
    api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/metadata`).catch(() => ({ metadata: {}, reasoning: '' })),
    api('/api/runs/' + encodeURIComponent(wf)),
  ]);
  const gate = detail.state;
  const m = Object.assign({ title:'', speaker:'', description:'', tags:[], chapters:[] }, metadata);
  const titleLen = [...(m.title || '')].length;

  app.innerHTML = `
  <p><a href="#/run/${encodeURIComponent(wf)}">← ${esc(detail.event.eventName || wf)}</a> · segment ${String(idx).padStart(2,'0')}</p>
  <div class="review">
    <div class="player-col">
      <video id="vid" controls preload="metadata" src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/video"></video>
      <div id="timeline"></div>
      <div id="trimreadout" style="color:var(--muted);font-size:12px;margin-top:6px"></div>
      ${reasoning ? `<details class="reason"><summary>metadata_reasoning.md</summary>${esc(reasoning)}</details>` : ''}
    </div>
    <div class="panel">
      <div class="field"><label>Title <span id="tc" class="counter ${titleLen>100?'over':''}">${titleLen}/100</span></label>
        <input id="f-title" value="${esc(m.title)}"></div>
      <div class="field"><label>Speaker</label><input id="f-speaker" value="${esc(m.speaker)}"></div>
      <div class="field"><label>Description</label><textarea id="f-desc">${esc(m.description)}</textarea></div>
      <div class="field"><label>Tags (comma separated)</label><input id="f-tags" value="${esc((m.tags||[]).join(', '))}"></div>
      <div class="field"><label><input type="checkbox" id="f-skip" ${m.skip?'checked':''}> Skip this segment</label></div>
      <div class="row-actions">
        <button class="btn" id="save">Save</button>
        <button class="btn btn-go" id="approve" ${gate==='review_gate'?'':'disabled'}>✓ Approve review</button>
        <button class="btn btn-no" id="reject" ${gate==='review_gate'?'':'disabled'}>Reject</button>
      </div>
      <p id="gatehint" style="color:var(--muted);font-size:12px"></p>
    </div>
  </div>`;

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
  document.getElementById('approve').onclick = async () => {
    await save();
    await api(`/api/runs/${encodeURIComponent(wf)}/approve`, { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ gate:'review', approved:true }) });
    toast('Review approved'); location.hash = `#/run/${encodeURIComponent(wf)}`;
  };
  document.getElementById('reject').onclick = async () => {
    await api(`/api/runs/${encodeURIComponent(wf)}/approve`, { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ gate:'review', approved:false }) });
    toast('Review rejected');
  };
  if (gate === 'upload_gate') document.getElementById('gatehint').textContent =
    'Parked at upload. Title/description/tags edits are picked up on upload — just re-approve there. A trim change needs a Redo Assemble (run it from the run overview).';

  initTimeline({ wf, idx, video: document.getElementById('vid'), mount: document.getElementById('timeline'),
    readout: document.getElementById('trimreadout'), trim: m.trim, chapters: m.chapters || [], onChange: () => save().catch(()=>{}) });
}
async function routeUpload(wf, idx) {
  const { metadata } = await api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/metadata`);
  const detail = await api('/api/runs/' + encodeURIComponent(wf));
  const gate = detail.state;
  const m = metadata || {};
  app.innerHTML = `
  <p><a href="#/run/${encodeURIComponent(wf)}">← ${esc(detail.event.eventName || wf)}</a> · upload preview · segment ${String(idx).padStart(2,'0')}</p>
  <div class="review">
    <div class="player-col">
      <video id="vid" controls preload="metadata" crossorigin="anonymous">
        <source src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/final" type="video/mp4">
        <track default kind="subtitles" srclang="en" label="English"
               src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/subtitles">
      </video>
      <div class="row-actions">
        <button class="btn" id="grabthumb">Use current frame as thumbnail</button>
        <img id="thumb" src="/api/runs/${encodeURIComponent(wf)}/segments/${idx}/thumbnail" alt="" style="height:54px;border-radius:4px;border:1px solid var(--border)">
      </div>
    </div>
    <div class="panel">
      <h3 style="margin-top:0">${esc(m.title || '')}</h3>
      <p style="color:var(--muted)">${esc(m.speaker || '')}</p>
      <p style="white-space:pre-wrap">${esc(m.description || '')}</p>
      <div class="chips">${(m.tags||[]).map(t => `<span>${esc(t)}</span>`).join('')}</div>
      ${(m.chapters||[]).length ? `<h4>Chapters</h4>${m.chapters.map(c => `<div>${esc(c.title)} — ${Math.floor(c.time/60)}:${String(Math.floor(c.time%60)).padStart(2,'0')}</div>`).join('')}` : ''}
      <div class="row-actions">
        <button class="btn btn-go" id="approve" ${gate==='upload_gate'?'':'disabled'}>✓ Approve upload</button>
        <button class="btn btn-no" id="reject" ${gate==='upload_gate'?'':'disabled'}>Reject</button>
      </div>
    </div>
  </div>`;

  document.getElementById('grabthumb').onclick = async () => {
    const t = document.getElementById('vid').currentTime;
    await api(`/api/runs/${encodeURIComponent(wf)}/segments/${idx}/thumbnail`,
      { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ seconds: t }) });
    document.getElementById('thumb').src = `/api/runs/${encodeURIComponent(wf)}/segments/${idx}/thumbnail?ts=${Date.now()}`;
    toast('Thumbnail updated');
  };
  document.getElementById('approve').onclick = async () => {
    await api(`/api/runs/${encodeURIComponent(wf)}/approve`, { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ gate:'upload', approved:true }) });
    toast('Upload approved'); location.hash = `#/run/${encodeURIComponent(wf)}`;
  };
  document.getElementById('reject').onclick = async () => {
    await api(`/api/runs/${encodeURIComponent(wf)}/approve`, { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ gate:'upload', approved:false }) });
    toast('Upload rejected');
  };
}
function renderBumpersPanel(wf, d) { /* Task 20 */ }
function renderResetPanel(wf, d) { /* Task 20 */ }
