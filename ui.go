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
    --bg: #101114; --bg2: #17191e; --card: #1d2026;
    --border: #2b2e36; --border2: #3a3d46;
    --text: #f1f2f4; --text2: #c5c8d0; --muted: #999eaa;
    --accent: #d7e4ff; --accent2: #a9c7ff;
    --green: #8ed8ad; --red: #ff9b9b; --yellow: #eab308; --blue: #90cdf4;
    --radius: 14px; --radius-sm: 9px;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: 'Inter', system-ui, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; overflow: hidden; letter-spacing: -0.01em; }
  #toolbar {
    padding: 12px 20px; background: var(--bg2); border-bottom: 1px solid var(--border);
    display: flex; gap: 12px; align-items: center; flex-wrap: wrap; height: 56px;
  }
  .logo-mark {
    width: 32px; height: 32px; background: var(--accent); border-radius: 8px;
    display: flex; align-items: center; justify-content: center; font-size: 1rem;
    color: var(--bg); font-weight: 700; flex-shrink: 0;
  }
  .toolbar-title { font-weight: 700; font-size: 0.95rem; display: flex; align-items: baseline; gap: 8px; }
  .toolbar-kana { font-size: 0.7rem; color: var(--muted); font-family: 'JetBrains Mono', monospace; letter-spacing: 0.1em; }
  .btn {
    padding: 9px 13px; cursor: pointer; background: var(--card);
    border: 1px solid var(--border); color: var(--text);
    border-radius: var(--radius-sm); font-family: inherit; font-size: 0.8rem; font-weight: 500;
    transition: all 0.15s;
  }
  .btn:hover { background: #252932; border-color: #555b67; }
  .btn.secondary {
    background: var(--card); border-color: var(--border); color: var(--text2);
  }
  .btn.secondary:hover { background: #252932; color: var(--text); }
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
  .link { stroke: var(--accent2); stroke-opacity: 0.3; stroke-width: 1px; }
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
  .controls button:hover { background: #252932; color: var(--accent); border-color: #555b67; }
  .info-badge {
    position: absolute; top: 72px; left: 20px; background: var(--card); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 8px 12px; font-size: 0.75rem; color: var(--text2);
    font-family: 'JetBrains Mono', monospace; z-index: 10;
  }
  .info-badge .count { color: var(--accent); font-weight: 600; }
</style>
</head>
<body>
  <div id="toolbar">
    <div class="logo-mark">m</div>
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
      <span><i style="background:#B794F4"></i>semantic</span>
      <span><i style="background:#22c55e"></i>episodic</span>
      <span><i style="background:#eab308"></i>procedural</span>
      <span><i style="background:#ec4899"></i>profile</span>
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

async function api(path){
  const response = await fetch(path, {headers: {'Content-Type': 'application/json'}});
  if(!response.ok) throw new Error('HTTP '+response.status);
  return response.json();
}

async function load(){
  document.getElementById('status').textContent='loading...';
  try {
    const [nodes,edgesResp]=await Promise.all([
      api('/api/nodes'),
      api('/api/edges'),
    ]);
    const rawEdges=Array.isArray(edgesResp)?edgesResp:(edgesResp.edges||[]);

    // Deduplicate edges: keep only one edge per src-dst pair
    const seenPairs = new Set();
    const edges = rawEdges.filter(e => {
      const key = e.src < e.dst ? e.src + '-' + e.dst : e.dst + '-' + e.src;
      if (seenPairs.has(key)) return false;
      seenPairs.add(key);
      return true;
    });

    // Map src/dst to source/target for D3 forceLink
    edges.forEach(e=>{ e.source=e.src; e.target=e.dst; });
    document.getElementById('status').textContent=nodes.length+' nodes, '+edges.length+' edges';
    document.getElementById('nodeCount').textContent=nodes.length;
    document.getElementById('edgeCount').textContent=edges.length;
    document.getElementById('infoBadge').style.display='block';
    render(nodes,edges);
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

  // Update legend to only show layers that exist
  const existingLayers = [...new Set(nodes.map(n => n.layer))];
  const legendEl = document.querySelector('.legend');
  if (legendEl) {
    legendEl.innerHTML = existingLayers.map(l =>
      '<span><i style="background:' + (colors[l] || '#B794F4') + '"></i>' + l + '</span>'
    ).join('');
  }

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

load();
window.addEventListener('resize',()=>{ if(simulation){ const w=window.innerWidth,h=window.innerHeight-56; svg.attr('width',w).attr('height',h); } });
</script>
</body>
</html>
`
