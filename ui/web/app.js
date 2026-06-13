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
  const strip = d.segments.map(s => `<a class="seg-chip" href="#/run/${encodeURIComponent(wf)}/seg/${s.index}">
    <strong>${String(s.index).padStart(2,'0')}</strong>
    <span class="badge b-${s.skip ? 'skip' : s.type}">${s.skip ? 'skip' : s.type}</span>
    <div>${esc(s.title || '')}</div></a>`).join('');
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
function routeSegment(wf, idx) { app.innerHTML = '<p>(segment review — Task 17)</p>'; }
function routeUpload(wf, idx) { app.innerHTML = '<p>(upload review — Task 19)</p>'; }
function renderBumpersPanel(wf, d) { /* Task 20 */ }
function renderResetPanel(wf, d) { /* Task 20 */ }
