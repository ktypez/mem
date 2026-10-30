package main

const graphHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>mem — Memory Graph グラフ</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
<script src="https://d3js.org/d3.v7.min.js"></script>
<style>
  :root {
    --bg: #0a0a0a; --bg2: #111113; --card: #141414;
    --border: rgba(255,255,255,0.06); --border2: rgba(255,255,255,0.12);
    --text: #fafafa; --text2: #a1a1aa; --muted: #71717a;
    --accent: #f472b6; --accent2: #a78bfa;
    --accent-grad: linear-gradient(135deg, #f472b6, #a78bfa);
    --green: #22c55e; --red: #ef4444; --yellow: #eab308; --blue: #3b82f6;
    --radius: 12px; --radius-sm: 8px;
    --glow: 0 0 24px rgba(244,114,182,0.35), 0 0 48px rgba(167,139,250,0.18);
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: 'Inter', system-ui, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; overflow: hidden; letter-spacing: -0.01em; }
  #toolbar {
    padding: 12px 20px; background: var(--bg2); border-bottom: 1px solid var(--border);
    display: flex; gap: 12px; align-items: center; flex-wrap: wrap; height: 56px;
  }
  .logo-mark {
    width: 32px; height: 32px; background: var(--accent-grad); border-radius: 8px;
    display: flex; align-items: center; justify-content: center; font-size: 1rem;
    box-shadow: var(--glow); position: relative; flex-shrink: 0;
  }
  .logo-mark::after { content:''; position:absolute; inset:-2px; border-radius:10px; background:var(--accent-grad); filter:blur(10px); opacity:0.3; z-index:-1; }
  .toolbar-title { font-weight: 700; font-size: 0.95rem; display: flex; align-items: baseline; gap: 8px; }
  .toolbar-kana { font-size: 0.7rem; color: var(--accent); font-family: 'JetBrains Mono', monospace; letter-spacing: 0.1em; }
  .btn {
    padding: 6px 14px; cursor: pointer; background: rgba(244,114,182,0.08);
    border: 1px solid rgba(244,114,182,0.25); color: var(--accent2);
    border-radius: var(--radius-sm); font-family: inherit; font-size: 0.8rem; font-weight: 500;
    transition: all 0.15s;
  }
  .btn:hover { background: rgba(244,114,182,0.15); border-color: rgba(244,114,182,0.4); }
  .btn.secondary {
    background: rgba(255,255,255,0.03); border-color: var(--border2); color: var(--text2);
  }
  .btn.secondary:hover { background: rgba(255,255,255,0.06); color: var(--text); }
  #status { color: var(--muted); font-size: 0.75rem; font-family: 'JetBrains Mono', monospace; }
  .legend { display: flex; gap: 10px; margin-left: auto; flex-wrap: wrap; align-items: center; }
  .legend span { display: inline-flex; align-items: center; gap: 5px; font-size: 0.7rem; color: var(--text2); }
  .legend i { width: 8px; height: 8px; display: inline-block; border-radius: 50%; }
  .nav-link {
    color: var(--accent); text-decoration: none; font-weight: 600; font-size: 0.85rem;
    display: flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: var(--radius-sm);
    transition: background 0.15s;
  }
  .nav-link:hover { background: rgba(244,114,182,0.08); }
  #graph { width: 100vw; height: calc(100vh - 56px); background: var(--bg); display: block; }
  .node { cursor: pointer; }
  .node circle { stroke: #fff; stroke-width: 1.5px; transition: r 0.2s; }
  .node:hover circle { stroke: var(--accent); stroke-width: 2.5px; }
  .node text { font-size: 10px; fill: var(--text2); pointer-events: none; font-family: 'Inter', sans-serif; }
  .link { stroke: var(--accent); stroke-opacity: 0.25; stroke-width: 1px; }
  .tooltip {
    position: absolute; background: var(--card); color: var(--text); padding: 10px 14px;
    border-radius: var(--radius-sm); font-size: 0.8rem; max-width: 320px; pointer-events: none;
    display: none; z-index: 100; border: 1px solid var(--border2);
    box-shadow: 0 8px 24px rgba(0,0,0,0.4); line-height: 1.5;
  }
  .tooltip .layer { font-size: 0.65rem; color: var(--accent); font-family: 'JetBrains Mono', monospace; text-transform: uppercase; letter-spacing: 0.08em; margin-bottom: 4px; }
  .tooltip .body { color: var(--text); }
  .controls {
    position: absolute; bottom: 20px; right: 20px; display: flex; gap: 8px; z-index: 10;
  }
  .controls button {
    width: 36px; height: 36px; border-radius: 8px; background: var(--card);
    border: 1px solid var(--border2); color: var(--text2); cursor: pointer; font-size: 1rem;
    display: flex; align-items: center; justify-content: center; transition: all 0.15s;
  }
  .controls button:hover { background: rgba(244,114,182,0.1); color: var(--accent); border-color: rgba(244,114,182,0.3); }
  .info-badge {
    position: absolute; top: 72px; left: 20px; background: var(--card); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 8px 12px; font-size: 0.75rem; color: var(--text2);
    font-family: 'JetBrains Mono', monospace; z-index: 10;
  }
  .info-badge .count { color: var(--accent); font-weight: 600; }
</style>
</head>
<body>
  
    <div class="logo-mark">🧠</div>
    <div class="toolbar-title">Memory Graph <span class="toolbar-kana">グラフ</span></div>
    <a href="/" class="nav-link">← ダッシュボード</a>
    <button class="btn" onclick="load()">Load Graph</button>
    <button class="btn secondary" onclick="resetView()">Reset View</button>
    <span id="status">click Load to fetch</span>
    <div class="filters">
      <div class="filter-group">
        <label>Layer:</label>
        <select id="filterLayer" onchange="filterGraph()">
          <option value="">All Layers</option>
          <option value="preference">preference</option>
          <option value="identity">identity</option>
          <option value="context">context</option>
          <option value="experience">experience</option>
          <option value="activity">activity</option>
          <option value="persona">persona</option>
          <option value="note">note</option>
          <option value="semantic">semantic</option>
          <option value="episodic">episodic</option>
          <option value="procedural">procedural</option>
          <option value="profile">profile</option>
        </select>
      </div>
      <div class="filter-group">
        <label>Topic:</label>
        <select id="filterTopic" onchange="filterGraph()"></select>
      </div>
      <div class="filter-group">
        <label>Edges:</label>
        <select id="filterEdges" onchange="filterEdges()">
          <option value="all">All Edges</option>
          <option value="same_topic">Same Topic</option>
          <option value="same_subject">Same Subject</option>
          <option value="same_day">Same Day</option>
          <option value="related">Related Only</option>
        </select>
      </div>
    </div>
    <div class="legend">
      <span><i style="background:#4FD1C0"></i>preference</span>
      <span><i style="background:#F6AD55"></i>identity</span>
      <span><i style="background:#90CDF4"></i>context</span>
      <span><i style="background:#FC8181"></i>experience</span>
      <span><i style="background:#68D391"></i>activity</span>
      <span><i style="background:#F687B3"></i>persona</span>
      <span><i style="background:#A0AEC0"></i>note</span>
      <span><i style="background:#B794F4"></i>semantic</span>
      <span><i style="background:#22c55e"></i>episodic</span>
      <span><i style="background:#eab308"></i>procedural</span>
      <span><i style="background:#ec4899"></i>profile</span>
    </div>
  </div>
  <div class="logo-mark">🧠</div>
  <div class="toolbar-title">Memory Graph <span class="toolbar-kana">グラフ</span></div>
  <a href="/" class="nav-link">← ダッシュボード</a>
  <button class="btn" onclick="load()">Load Graph</button>
  <button class="btn secondary" onclick="resetView()">Reset View</button>
  <span id="status">click Load to fetch</span>
  <div class="legend">
    <span><i style="background:#4FD1C0"></i>preference</span>
    <span><i style="background:#F6AD55"></i>identity</span>
    <span><i style="background:#90CDF4"></i>context</span>
    <span><i style="background:#FC8181"></i>experience</span>
    <span><i style="background:#68D391"></i>activity</span>
    <span><i style="background:#F687B3"></i>persona</span>
    <span><i style="background:#A0AEC0"></i>note</span>
    <span><i style="background:#B794F4"></i>other</span>
  </div>
</div>
<svg id="graph"></svg>
<div class="tooltip" id="tooltip"></div>
<div class="info-badge" id="infoBadge" style="display:none">
  Nodes: <span class="count" id="nodeCount">0</span> · Edges: <span class="count" id="edgeCount">0</span>
</div>
<div class="controls">
  <button onclick="zoomIn()" title="Zoom in">+</button>
  <button onclick="zoomOut()" title="Zoom out">−</button>
  <button onclick="resetView()" title="Reset">⊙</button>
</div>
<script>
const colors = {preference:'#4FD1C0',identity:'#F6AD55',context:'#90CDF4',experience:'#FC8181',activity:'#68D391',persona:'#F687B3',note:'#A0AEC0',semantic:'#8b5cf6',episodic:'#3b82f6',procedural:'#22c55e',profile:'#eab308',context:'#90CDF4'};
function color(l){return colors[l]||'#B794F4';}
const tooltip = document.getElementById('tooltip');
const svg = d3.select('#graph');
let simulation, zoom, g, node, link;
let currentTransform = d3.zoomIdentity;

async function load(){
  document.getElementById('status').textContent='loading...';
  try {
    const layerFilter = document.getElementById('filterLayer')?.value || '';
    const topicFilter = document.getElementById('filterTopic')?.value || '';
    const edgeFilter = document.getElementById('filterEdges')?.value || 'all';

    // Fetch nodes with optional filters
    let nodeUrl = '/api/nodes';
    const params = [];
    if (layerFilter) params.push('layer=' + encodeURIComponent(layerFilter));
    if (topicFilter) params.push('topic=' + encodeURIComponent(topicFilter));
    if (params.length > 0) nodeUrl += '?' + params.join('&');

    const [nr,er]=await Promise.all([
      fetch(nodeUrl),
      fetch('/api/edges'),
    ]);
    if(!nr.ok || !er.ok) throw new Error('HTTP '+nr.status+' / '+er.status);
    const nodes=await nr.json();
    const edgesResp=await er.json();
    const edges=Array.isArray(edgesResp)?edgesResp:(edgesResp.edges||[]);

    // Filter edges by type
    let filteredEdges = edges;
    if (edgeFilter === 'same_topic') {
      filteredEdges = edges.filter(e => {
        const src = nodes.find(n => n.id === e.src);
        const dst = nodes.find(n => n.id === e.dst);
        return src && dst && src.topic === dst.topic;
      });
    } else if (edgeFilter === 'same_subject') {
      filteredEdges = edges.filter(e => {
        const src = nodes.find(n => n.id === e.src);
        const dst = nodes.find(n => n.id === e.dst);
        return src && dst && src.subject === dst.subject;
      });
    } else if (edgeFilter === 'same_day') {
      filteredEdges = edges.filter(e => {
        const src = nodes.find(n => n.id === e.src);
        const dst = nodes.find(n => n.id === e.dst);
        return src && dst && src.created_at === dst.created_at;
      });
    } else if (edgeFilter === 'related') {
      filteredEdges = edges.filter(e => e.rel === 'related' || e.rel === 'edge');
    }

    // Map src/dst to source/target for D3 forceLink
    filteredEdges.forEach(e=>{ e.source=e.src; e.target=e.dst; });
    document.getElementById('status').textContent=nodes.length+' nodes, '+filteredEdges.length+' edges';
    document.getElementById('nodeCount').textContent=nodes.length;
    document.getElementById('edgeCount').textContent=filteredEdges.length;
    document.getElementById('infoBadge').style.display='block';
    render(nodes,filteredEdges);
  } catch(err) {
    document.getElementById('status').textContent='error: '+err.message;
    console.error(err);
  }
}

function render(nodes,edges){
  svg.selectAll('*').remove();
  const w=window.innerWidth, h=window.innerHeight-56;
  svg.attr('width',w).attr('height',h);
  g=svg.append('g');

  zoom=d3.zoom().scaleExtent([0.05,8]).on('zoom',e=>{
    g.attr('transform',e.transform);
    currentTransform=e.transform;
  });
  svg.call(zoom);

  // Limit nodes for performance
  const maxNodes=120;
  if(nodes.length>maxNodes){
    const connCount={};
    edges.forEach(e=>{connCount[e.src]=(connCount[e.src]||0)+1;connCount[e.dst]=(connCount[e.dst]||0)+1;});
    nodes.sort((a,b)=>(connCount[b.id]||0)-(connCount[a.id]||0));
    const keepIds=new Set(nodes.slice(0,maxNodes).map(n=>n.id));
    nodes=nodes.filter(n=>keepIds.has(n.id));
    edges=edges.filter(e=>keepIds.has(e.src)&&keepIds.has(e.dst));
  }

  // Limit edges to top N by weight to reduce clutter
  const maxEdges=300;
  if(edges.length>maxEdges){
    edges=edges.slice(0,maxEdges);
  }

  simulation=d3.forceSimulation(nodes)
    .force('link',d3.forceLink(edges).id(d=>d.id).distance(100).strength(0.2))
    .force('charge',d3.forceManyBody().strength(-400))
    .force('center',d3.forceCenter(w/2,h/2))
    .force('collide',d3.forceCollide(20))
    .alphaDecay(0.05)
    .on('tick',()=>{
      link.attr('x1',d=>d.source.x).attr('y1',d=>d.source.y).attr('x2',d=>d.target.x).attr('y2',d=>d.target.y);
      node.attr('transform',d=>'translate('+d.x+','+d.y+')');
    })
    .on('end',()=>{
      // Freeze positions so nodes stop floating
      nodes.forEach(n=>{n.fx=n.x;n.fy=n.y;});
      simulation.stop();
      document.getElementById('status').textContent='loaded '+nodes.length+' nodes, '+edges.length+' edges (frozen)';
    });

  link=g.append('g').attr('class','links').selectAll('line').data(edges).join('line').attr('class','link');

  node=g.append('g').attr('class','nodes').selectAll('g').data(nodes).join('g').attr('class','node')
    .call(d3.drag().on('start',(e,d)=>{if(!e.active)simulation.alphaTarget(0.3).restart();d.fx=d.x;d.fy=d.y;})
      .on('drag',(e,d)=>{d.fx=e.x;d.fy=e.y;})
      .on('end',(e,d)=>{if(!e.active)simulation.alphaTarget(0);d.fx=null;d.fy=null;}));

  node.append('circle').attr('r',8).attr('fill',d=>color(d.layer)).attr('stroke','rgba(255,255,255,0.3)').attr('stroke-width',1);
  node.append('text').text(d=>(d.body||d.id).slice(0,18)).attr('x',12).attr('y',4).attr('fill','#a1a1aa').attr('font-size','10px');

  node.on('mouseover',(e,d)=>{
    tooltip.style.display='block';
    tooltip.style.left=(e.pageX+12)+'px';
    tooltip.style.top=(e.pageY-12)+'px';
    tooltip.innerHTML='<div class="layer">'+(d.layer||'unknown')+'</div><div class="body">'+(d.body||d.id).slice(0,300)+'</div>';
  }).on('mousemove',(e)=>{
    tooltip.style.left=(e.pageX+12)+'px';
    tooltip.style.top=(e.pageY-12)+'px';
  }).on('mouseout',()=>{tooltip.style.display='none';});
}

function zoomIn(){ svg.transition().call(zoom.scaleBy,1.5); }
function zoomOut(){ svg.transition().call(zoom.scaleBy,0.67); }
function resetView(){ svg.transition().duration(500).call(zoom.transform, d3.zoomIdentity); }

// Filter functions for graph page
function populateFilters() {
  const layerSelect = document.getElementById('filterLayer');
  const topicSelect = document.getElementById('filterTopic');
  const edgeSelect = document.getElementById('filterEdges');
  if (!layerSelect || !topicSelect || !edgeSelect) return;

  // Populate layer filter
  const layers = ['preference','identity','context','experience','activity','persona','note','semantic','episodic','procedural','profile'];
  layerSelect.innerHTML = '<option value="">All Layers</option>' + layers.map(l => '<option value="' + l + '">' + l + '</option>').join('');

  // Populate topic filter
  fetch('/v1/memory/nodes?limit=1000').then(r => r.json()).then(nodes => {
    const topics = [...new Set(nodes.nodes.map(n => n.topic).filter(Boolean))];
    topicSelect.innerHTML = '<option value="">All Topics</option>' + topics.map(t => '<option value="' + t + '">' + t + '</option>').join('');
  });

  // Populate edge filter
  const edges = ['all','same_topic','same_subject','same_day','related'];
  edgeSelect.innerHTML = edges.map(e => '<option value="' + e + '">' + e + '</option>').join('');
}

populateFilters();

function filterGraph() {
  const layer = document.getElementById('filterLayer')?.value || '';
  const topic = document.getElementById('filterTopic')?.value || '';
  if (!layer && !topic) {
    load(); // Reset to all
    return;
  }
  // Reload with filters
  load();
}

function filterEdges() {
  const edgeType = document.getElementById('filterEdges')?.value || 'all';
  if (edgeType === 'all') {
    load(); // Reset
    return;
  }
  // Reload with edge filter
  load();
}

load();
window.addEventListener('resize',()=>{ if(simulation){ const w=window.innerWidth,h=window.innerHeight-56; svg.attr('width',w).attr('height',h); } });
</script>
</body>
</html>
`
