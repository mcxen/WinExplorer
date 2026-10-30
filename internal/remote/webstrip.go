package remote

// webstrip.go is the image viewer's bottom filmstrip, shared by both pages
// (BandiView's thumbnail bar): one row of small thumbnails, the current one
// outlined and scrolled to the middle, tap or click to jump. Thumbnails load
// on demand — an IntersectionObserver gives each <img> its src only when it
// comes within a screen of the strip's visible range — and ask the server
// for the 256 px rendition (/d/…?w=256), never the original.

const webStripCSS = `
.svstrip{display:flex;gap:4px;overflow-x:auto;padding:6px 8px calc(6px + env(safe-area-inset-bottom));scrollbar-width:none;-webkit-overflow-scrolling:touch;touch-action:pan-x;flex-shrink:0;background:rgba(0,0,0,.4)}
.svstrip::-webkit-scrollbar{display:none}
.svstrip.hidden{display:none}
.svstrip-t{flex:0 0 auto;width:48px;height:48px;padding:0;border:2px solid transparent;border-radius:4px;overflow:hidden;background:#111;opacity:.55;cursor:pointer}
.svstrip-t.on{border-color:var(--accent);opacity:1}
.svstrip-t img{width:100%;height:100%;object-fit:cover;display:block}
.svpos{color:rgba(255,255,255,.7);font-size:12px;font-variant-numeric:tabular-nums;white-space:nowrap}
@media(max-width:600px){.svstrip-t{width:40px;height:40px}}`

const webStripJS = `
// svStrip(host,list,cur,url,pick)：底部胶片条，按需加载缩略图。
function svStrip(host,list,cur,url,pick){
 if(!host)return;
 if(!list||list.length<2){host.classList.add('hidden');host.textContent='';host._key='';return}
 host.classList.remove('hidden');
 const key=list.map(it=>url(it)||it.name).join('\n');
 if(host._key!==key){
  host._key=key;host.textContent='';
  if(host._io)host._io.disconnect();
  host._io='IntersectionObserver' in window?new IntersectionObserver(es=>{for(const e of es)if(e.isIntersecting){const im=e.target;im.src=im.dataset.src;host._io.unobserve(im)}},{root:host,rootMargin:'0px 100%'}):null;
  list.forEach((it,i)=>{
   const b=document.createElement('button');b.type='button';b.className='svstrip-t';b.title=it.name;
   const u=url(it);
   if(u){const im=document.createElement('img');im.alt='';im.decoding='async';im.dataset.src=u;b.append(im);if(host._io)host._io.observe(im);else im.src=u}
   b.onclick=()=>host._pick(list[i],i);
   host.append(b);
  });
 }
 host._pick=pick;
 const kids=host.children;
 for(let i=0;i<kids.length;i++)kids[i].classList.toggle('on',i===cur);
 const on=kids[cur];
 if(on)host.scrollTo({left:on.offsetLeft-host.clientWidth/2+on.offsetWidth/2,behavior:host._shown?'smooth':'auto'});
 host._shown=true;
}`
