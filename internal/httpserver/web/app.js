// Cartograph web UI — vanilla JS, no build step, no framework (see
// ADR-0013: chosen so the whole project stays `go build`-only). Fetches
// JSON from internal/httpserver's /api/* endpoints, which are thin
// wrappers over internal/service — every decision (search matching, graph
// traversal) is made server-side; this file only renders what the API
// returns and does simple CLIENT-SIDE substring filtering over the
// already-fetched entity list for the search box's live-narrowing feel
// (the actual jump-to-entity still goes through the server's exact-match
// /api/find, matching internal/service.Find's documented contract).

let allEntities = [];

async function getJSON(url) {
  const res = await fetch(url);
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

async function loadOverview() {
  const stats = await getJSON('/api/stats');
  document.getElementById('stats').textContent =
    `${stats.repo} · ${stats.entities} entities · ${stats.edges} edges`;

  const breakdown = document.getElementById('kind-breakdown');
  breakdown.innerHTML = '';
  const kinds = Object.entries(stats.byKind).sort((a, b) => b[1] - a[1]);
  for (const [kind, count] of kinds) {
    const card = el('div', 'card');
    card.appendChild(el('div', 'n', String(count)));
    card.appendChild(el('div', 'l', kind));
    breakdown.appendChild(card);
  }

  const graph = await getJSON('/api/graph');
  allEntities = graph.Entities || [];
}

function renderResults(items) {
  const box = document.getElementById('results');
  box.innerHTML = '';
  for (const e of items.slice(0, 60)) {
    const row = el('div', 'result-item');
    const kindSpan = el('span', 'kind', e.Kind);
    row.appendChild(kindSpan);
    row.appendChild(document.createTextNode(e.Name));
    row.appendChild(el('span', 'file', e.Anchor ? e.Anchor.File : ''));
    row.onclick = () => showEntity(e.Name, e.Anchor ? e.Anchor.File : '');
    box.appendChild(row);
  }
}

function wireSearch() {
  const input = document.getElementById('search');
  input.addEventListener('input', () => {
    const q = input.value.trim().toLowerCase();
    if (!q) { renderResults([]); return; }
    const matches = allEntities.filter(e => e.Name.toLowerCase().includes(q));
    renderResults(matches);
  });
  input.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter') {
      const q = input.value.trim();
      if (q) showEntity(q, '');
    }
  });
}

function showOverview() {
  document.getElementById('overview').hidden = false;
  document.getElementById('inspector').hidden = true;
}

document.getElementById('back-to-overview').onclick = showOverview;

async function showEntity(name, fileHint) {
  let insp;
  try {
    insp = await getJSON(`/api/inspect?name=${encodeURIComponent(name)}&file=${encodeURIComponent(fileHint || '')}`);
  } catch (err) {
    alert(err.message);
    return;
  }
  document.getElementById('overview').hidden = true;
  const inspector = document.getElementById('inspector');
  inspector.hidden = false;

  document.getElementById('insp-name').textContent = insp.Entity.Name;
  document.getElementById('insp-meta').textContent =
    `${insp.Entity.Kind} · ${insp.Entity.Qualified} · ${insp.Entity.Anchor.File}:${insp.Entity.Anchor.StartLine}-${insp.Entity.Anchor.EndLine}`;

  document.getElementById('insp-source').hidden = true;
  document.getElementById('insp-source').textContent = '';
  document.getElementById('graph-wrap').hidden = true;
  document.getElementById('impact-wrap').hidden = true;

  const fillEdges = (listId, edges, endpointKey) => {
    const ul = document.getElementById(listId);
    ul.innerHTML = '';
    if (!edges || edges.length === 0) {
      ul.appendChild(el('li', 'muted', '(none)'));
      return;
    }
    for (const edge of edges) {
      const id = edge[endpointKey];
      const target = allEntities.find(e => e.ID === id);
      const li = el('li');
      li.appendChild(el('span', 'kind-tag', edge.Kind));
      li.appendChild(document.createTextNode(target ? target.Name : id));
      if (target) li.onclick = () => showEntity(target.Name, target.Anchor.File);
      ul.appendChild(li);
    }
  };
  fillEdges('fan-in', insp.FanIn, 'Src');
  fillEdges('fan-out', insp.FanOut, 'Dst');

  document.getElementById('btn-source').onclick = async () => {
    const data = await getJSON(`/api/source?name=${encodeURIComponent(insp.Entity.Name)}&file=${encodeURIComponent(insp.Entity.Anchor.File)}`);
    const pre = document.getElementById('insp-source');
    pre.textContent = data.Source;
    pre.hidden = false;
  };

  document.getElementById('btn-graph').onclick = async () => {
    const related = await getJSON(`/api/related?name=${encodeURIComponent(insp.Entity.Name)}&file=${encodeURIComponent(insp.Entity.Anchor.File)}&depth=2`);
    document.getElementById('graph-wrap').hidden = false;
    drawGraph(insp.Entity, related);
  };

  document.getElementById('btn-impact').onclick = async () => {
    const impact = await getJSON(`/api/impact?name=${encodeURIComponent(insp.Entity.Name)}&file=${encodeURIComponent(insp.Entity.Anchor.File)}`);
    document.getElementById('impact-wrap').hidden = false;
    document.getElementById('impact-summary').textContent =
      `${impact.DirectCallers ? impact.DirectCallers.length : 0} direct caller(s) · ${impact.Transitive ? impact.Transitive.length : 0} total in the blast radius`;

    const list = document.getElementById('impact-list');
    list.innerHTML = '';
    if (!impact.Transitive || impact.Transitive.length === 0) {
      list.appendChild(el('li', 'muted', '(nothing depends on this — safe to change in isolation)'));
    } else {
      for (const r of impact.Transitive) {
        const li = el('li');
        li.appendChild(el('span', 'kind-tag', `depth ${r.Depth}`));
        li.appendChild(document.createTextNode(`${r.Entity.Kind} ${r.Entity.Name}`));
        li.onclick = () => showEntity(r.Entity.Name, r.Entity.Anchor.File);
        list.appendChild(li);
      }
    }

    const testsUl = document.getElementById('impact-tests');
    testsUl.innerHTML = '';
    const tests = impact.CoveringTests || [];
    document.getElementById('impact-tests-h').textContent = `Tests covering it (${tests.length})`;
    if (tests.length === 0) {
      testsUl.appendChild(el('li', 'muted', '(none found)'));
    } else {
      for (const test of tests) {
        const li = el('li', null, test.Name);
        li.onclick = () => showEntity(test.Name, test.Anchor.File);
        testsUl.appendChild(li);
      }
    }
  };
}

// --- Minimal force-directed layout, no external library (ADR-0013) ---
// A textbook spring/repulsion simulation run for a fixed number of
// iterations, then rendered once. Good enough for the bounded
// neighborhoods this view actually shows (never the whole repo at once —
// see internal/httpserver's package doc).
function drawGraph(centerEntity, related) {
  const canvas = document.getElementById('graph-canvas');
  const ctx = canvas.getContext('2d');
  const W = canvas.width, H = canvas.height;

  const nodesById = new Map();
  nodesById.set(centerEntity.ID, { id: centerEntity.ID, name: centerEntity.Name, kind: centerEntity.Kind, x: W / 2, y: H / 2, vx: 0, vy: 0, center: true });
  const edges = [];
  for (const r of related) {
    const e = r.Entity;
    if (!nodesById.has(e.ID)) {
      nodesById.set(e.ID, {
        id: e.ID, name: e.Name, kind: e.Kind,
        x: W / 2 + (Math.random() - 0.5) * 400,
        y: H / 2 + (Math.random() - 0.5) * 300,
        vx: 0, vy: 0, center: false,
      });
    }
    const via = r.Via;
    if (via && via.Src && via.Dst) edges.push([via.Src, via.Dst]);
  }
  const nodes = [...nodesById.values()];

  const iterations = 200;
  const k = 70; // ideal spring/repulsion distance
  for (let iter = 0; iter < iterations; iter++) {
    for (const a of nodes) {
      let fx = 0, fy = 0;
      for (const b of nodes) {
        if (a === b) continue;
        const dx = a.x - b.x, dy = a.y - b.y;
        const dist = Math.max(Math.sqrt(dx * dx + dy * dy), 1);
        const repel = (k * k) / dist;
        fx += (dx / dist) * repel;
        fy += (dy / dist) * repel;
      }
      a.vx = (a.vx + fx) * 0.85;
      a.vy = (a.vy + fy) * 0.85;
    }
    for (const [srcId, dstId] of edges) {
      const a = nodesById.get(srcId), b = nodesById.get(dstId);
      if (!a || !b) continue;
      const dx = b.x - a.x, dy = b.y - a.y;
      const dist = Math.max(Math.sqrt(dx * dx + dy * dy), 1);
      const force = (dist - k) * 0.02;
      const fx = (dx / dist) * force, fy = (dy / dist) * force;
      a.vx += fx; a.vy += fy;
      b.vx -= fx; b.vy -= fy;
    }
    for (const n of nodes) {
      if (n.center) continue; // keep the focal entity anchored at center
      n.x += n.vx * 0.05;
      n.y += n.vy * 0.05;
      n.x = Math.min(Math.max(n.x, 30), W - 30);
      n.y = Math.min(Math.max(n.y, 30), H - 30);
    }
  }

  ctx.clearRect(0, 0, W, H);
  const style = getComputedStyle(document.documentElement);
  const border = style.getPropertyValue('--border') || '#ccc';
  const accent = style.getPropertyValue('--accent') || '#2563eb';
  const fg = style.getPropertyValue('--fg') || '#111';

  ctx.strokeStyle = border;
  ctx.lineWidth = 1;
  for (const [srcId, dstId] of edges) {
    const a = nodesById.get(srcId), b = nodesById.get(dstId);
    if (!a || !b) continue;
    ctx.beginPath();
    ctx.moveTo(a.x, a.y);
    ctx.lineTo(b.x, b.y);
    ctx.stroke();
  }

  for (const n of nodes) {
    ctx.beginPath();
    ctx.arc(n.x, n.y, n.center ? 9 : 6, 0, Math.PI * 2);
    ctx.fillStyle = n.center ? accent : border;
    ctx.fill();
    ctx.strokeStyle = accent;
    ctx.stroke();

    ctx.fillStyle = fg;
    ctx.font = n.center ? 'bold 12px sans-serif' : '11px sans-serif';
    ctx.fillText(n.name, n.x + 10, n.y + 4);
  }

  canvas.onclick = (ev) => {
    const rect = canvas.getBoundingClientRect();
    const mx = (ev.clientX - rect.left) * (canvas.width / rect.width);
    const my = (ev.clientY - rect.top) * (canvas.height / rect.height);
    for (const n of nodes) {
      const d = Math.hypot(n.x - mx, n.y - my);
      if (d < 12) { showEntity(n.name, ''); return; }
    }
  };
}

wireSearch();
loadOverview().catch(err => {
  document.getElementById('overview').innerHTML =
    `<h1>No index found</h1><p class="muted">${err.message}</p>`;
});
