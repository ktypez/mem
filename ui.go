package main

const graphHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>mem — Memory Graph</title>
<script src="https://d3js.org/d3.v7.min.js"></script>
<style>
body { margin:0; font-family: sans-serif; background:#1a1a2e; color:#E6E6E6; }
#toolbar { padding:10px; background:#1a1a1a; display:flex; gap:10px; align-items:center; flex-wrap:wrap; }
#graph { width:100vw; height:calc(100vh - 50px); }
.node { cursor:pointer; }
.node circle { stroke:#fff; stroke-width:1.5px; }
.link { stroke:#555; stroke-opacity:0.6; }
label { font-size:11px; fill:#ccc; pointer-events:none; }
.legend { display:flex; gap:8px; margin-left:auto; flex-wrap:wrap; }
.legend span { display:inline-flex; align-items:center; gap:4px; font-size:12px; }
.legend i { width:12px; height:12px; display:inline-block; border-radius:50%; }
.tooltip { position:absolute; background:#222; color:#fff; padding:8px 12px; border-radius:6px; font-size:12px; max-width:300px; pointer-events:none; display:none; z-index:10; border:1px solid #555; }
#status { color:#aaa; font-size:12px; }
</style>
</head>
<body>
<div id="toolbar">
  <a href="/" style="color:#4FD1C0;text-decoration:none;font-weight:bold;margin-right:10px;">← Dashboard</a>
  <button onclick="load()" style="padding:4px 12px;cursor:pointer;">Load Graph</button>
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
<script>
const colors = {preference:'#4FD1C0',identity:'#F6AD55',context:'#90CDF4',experience:'#FC8181',activity:'#68D391',persona:'#F687B3',note:'#A0AEC0',semantic:'#8b5cf6',episodic:'#22c55e',procedural:'#eab308',profile:'#ec4899'};
function color(l){return colors[l]||'#B794F4';}
const tooltip = document.getElementById('tooltip');
async function load(){
  document.getElementById('status').textContent='loading...';
  const [nr,er]=await Promise.all([
    fetch('/api/nodes'),
    fetch('/api/edges'),
  ]);
  const nodes=await nr.json();
  const edgesResp=await er.json();
  const edges=Array.isArray(edgesResp)?edgesResp:(edgesResp.edges||[]);
  document.getElementById('status').textContent=nodes.length+' nodes, '+edges.length+' edges';
  render(nodes,edges);
}
function render(nodes,edges){
  const svg=d3.select('#graph');
  svg.selectAll('*').remove();
  const w=window.innerWidth||800, h=(window.innerHeight||600)-50;
  svg.attr('width',w).attr('height',h);
  const g=svg.append('g');
  svg.call(d3.zoom().scaleExtent([0.1,10]).on('zoom',e=>g.attr('transform',e.transform)));
  // Limit nodes for performance if too many
  const maxNodes=200;
  if(nodes.length>maxNodes){
    // Keep nodes with most connections
    const connCount={};
    edges.forEach(e=>{connCount[e.src]=(connCount[e.src]||0)+1;connCount[e.dst]=(connCount[e.dst]||0)+1;});
    nodes.sort((a,b)=>(connCount[b.id]||0)-(connCount[a.id]||0));
    const keepIds=new Set(nodes.slice(0,maxNodes).map(n=>n.id));
    nodes=nodes.filter(n=>keepIds.has(n.id));
    edges=edges.filter(e=>keepIds.has(e.src)&&keepIds.has(e.dst));
  }
  const sim=d3.forceSimulation(nodes)
    .force('link',d3.forceLink(edges).id(d=>d.id).distance(80).strength(0.3))
    .force('charge',d3.forceManyBody().strength(-300))
    .force('center',d3.forceCenter(w/2,h/2))
    .force('collide',d3.forceCollide(18));
  const link=g.append('g').attr('class','links').selectAll('line').data(edges).join('line')
    .attr('stroke','#4FD1C0').attr('stroke-opacity',0.4).attr('stroke-width',1.5);
  const node=g.append('g').attr('class','nodes').selectAll('g').data(nodes).join('g')
    .attr('class','node').style('cursor','pointer')
    .call(d3.drag().on('start',(e,d)=>{if(!e.active)sim.alphaTarget(0.3).restart();d.fx=d.x;d.fy=d.y;})
      .on('drag',(e,d)=>{d.fx=e.x;d.fy=e.y;})
      .on('end',(e,d)=>{if(!e.active)sim.alphaTarget(0);d.fx=null;d.fy=null;}));
  node.append('circle').attr('r',8).attr('fill',d=>color(d.layer)).attr('stroke','#fff').attr('stroke-width',1.5);
  node.append('text').text(d=>(d.body||d.id).slice(0,20)).attr('x',12).attr('y',4).attr('font-size','10px').attr('fill','#ccc');
  node.on('mouseover',(e,d)=>{
    tooltip.style.display='block';
    tooltip.style.left=(e.pageX+10)+'px';
    tooltip.style.top=(e.pageY-10)+'px';
    tooltip.innerHTML='<b>'+d.layer+'</b><br>'+(d.body||d.id).slice(0,200);
  }).on('mousemove',(e)=>{
    tooltip.style.left=(e.pageX+10)+'px';
    tooltip.style.top=(e.pageY-10)+'px';
  }).on('mouseout',()=>{tooltip.style.display='none';});
  sim.on('tick',()=>{
    link.attr('x1',d=>d.source.x).attr('y1',d=>d.source.y).attr('x2',d=>d.target.x).attr('y2',d=>d.target.y);
    node.attr('transform',d=>'translate('+d.x+','+d.y+')');
  });
  // Auto-fit after simulation settles
  sim.on('end',()=>{
    const bounds=g.node().getBBox();
    const scale=0.9*Math.min(w/bounds.width, h/bounds.height);
    const tx=w/2-scale*(bounds.x+bounds.width/2);
    const ty=h/2-scale*(bounds.y+bounds.height/2);
    svg.transition().duration(500).call(d3.zoom().transform, d3.zoomIdentity.translate(tx,ty).scale(scale));
  });
}
load();
</script>
</body>
</html>`
