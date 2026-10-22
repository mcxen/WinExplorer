package remote

// webtokens.go holds the shared design tokens for Sunvale's two built-in web
// pages (the management console and the guest page). Both pages keep
// /*__TOKENS__*/ and /*__SHARED_JS__*/ placeholders in their embedded HTML;
// remote_manage_web.go and remote_web.go inline these strings at init time,
// so the pages stay single self-contained documents with no build step and
// no external assets. The token values mirror the desktop shell's Win11
// styling: light chrome #F3F3F3 / dark #202020, accent #0067C0 / #4CC2FF.

// WebUITokensCSS is the shared CSS variable block. The light theme lives on
// :root; the dark theme is applied by [data-theme="dark"] (set by the shared
// theme toggle) and falls back to the prefers-color-scheme media query only
// while the user has not chosen a theme manually (:root:not([data-theme=
// "light"]) keeps an explicit light choice from being overridden).
const WebUITokensCSS = `:root{--chrome:#F3F3F3;--side:#E9E9E9;--card:#FFFFFF;--border:#E5E5E5;--text:#1A1A1A;--muted:#6B6B6B;--accent:#0067C0;--weak:rgba(0,103,192,.10);--sel:#CCE4F7;--danger:#C42B1C;--ok:#0F7B0F;--hover:rgba(0,0,0,.05);--onaccent:#FFFFFF;--dot:#9A9A9A;--dotok:#13A10E;--shadow:rgba(0,0,0,.10)}
@media(prefers-color-scheme:dark){:root:not([data-theme="light"]){--chrome:#202020;--side:#1B1B1B;--card:#272727;--border:#383838;--text:#F3F3F3;--muted:#A8A8A8;--accent:#4CC2FF;--weak:rgba(76,194,255,.13);--sel:#004C87;--danger:#FF8A80;--ok:#6CCB5F;--hover:rgba(255,255,255,.07);--onaccent:#161616;--dot:#9A9A9A;--dotok:#6CCB5F;--shadow:rgba(0,0,0,.5)}}
[data-theme="dark"]{--chrome:#202020;--side:#1B1B1B;--card:#272727;--border:#383838;--text:#F3F3F3;--muted:#A8A8A8;--accent:#4CC2FF;--weak:rgba(76,194,255,.13);--sel:#004C87;--danger:#FF8A80;--ok:#6CCB5F;--hover:rgba(255,255,255,.07);--onaccent:#161616;--dot:#9A9A9A;--dotok:#6CCB5F;--shadow:rgba(0,0,0,.5)}`

// WebUISharedJS is the shared script block for both pages: the manual theme
// toggle (localStorage "sv-theme", following the system only until the user
// picks a side), the Lucide path dictionary, the extension tables, the
// fmtSize/fmtTime formatters and a minimal toast. The management page
// contributed the canonical implementations.
const WebUISharedJS = `const SV_THEME_KEY='sv-theme';
(function(){
 const root=document.documentElement;
 const mq=window.matchMedia?matchMedia('(prefers-color-scheme:dark)'):null;
 const saved=()=>{try{return localStorage.getItem(SV_THEME_KEY)}catch(e){return null}};
 const apply=m=>root.setAttribute('data-theme',m);
 if(saved()==='light'||saved()==='dark')apply(saved());
 else{
  apply(mq&&mq.matches?'dark':'light');
  if(mq&&mq.addEventListener){try{mq.addEventListener('change',e=>{if(!saved())apply(e.matches?'dark':'light')})}catch(e){}}
 }
})();
function svToggleTheme(){
 const m=document.documentElement.getAttribute('data-theme')==='dark'?'light':'dark';
 document.documentElement.setAttribute('data-theme',m);
 try{localStorage.setItem(SV_THEME_KEY,m)}catch(e){}
 return m;
}
const SV_SUN='<circle cx="12" cy="12" r="4"/><path d="M12 3v2.2M12 18.8V21M3 12h2.2M18.8 12H21M5.6 5.6l1.6 1.6M16.8 16.8l1.6 1.6M18.4 5.6l-1.6 1.6M7.2 16.8l-1.6 1.6"/>';
const SV_MOON='<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>';
const EXT={
 video:'mp4 mkv avi mov m4v webm flv ts rmvb wmv 3gp mpg mpeg f4v asf ogv m2ts mts 3g2 m2v m4p',
 audio:'mp3 flac wav aac ogg m4a wma opus ape aiff aif aifc oga mka mp2 weba caf mid amr',
 image:'jpg jpeg jpe jfif pjp pjpeg png gif webp bmp svg ico tif tiff heic heif avif jxl',
 text:'txt md markdown mdx rst adoc tex org log json jsonl ndjson geojson xml html htm css scss sass less js mjs cjs ts jsx tsx vue svelte go py pyw rb java kt kts c h cpp cc hpp cs rs swift m mm php lua dart scala pl pm sh bash zsh fish bat ps1 cmd yml yaml toml ini conf cfg env properties editorconfig gitignore gitattributes dockerfile makefile cmake gradle mod sum proto graphql gql csv tsv sql lock srt vtt ass ssa lrc nfo eml ics vcf diff patch reg url m3u m3u8',
 pdf:'pdf',
 doc:'docx docm dotx dotm xlsx xlsm xltx xltm pptx pptm potx potm ppsx odt ods odp odg rtf epub',
 archive:'zip jar war ear apk aab ipa whl vsix xpi crx nupkg cbz pages numbers key tar gz tgz bz2 tbz2 xz txz zst tzst lz4 7z rar cab'
};
const ICONS={
 dash:'<path d="M4.5 17.5a9 9 0 1 1 15 0"/><path d="M12 13.5l3.5-3.5"/><path d="M12 16.5h.01"/>',
 folder:'<path d="M3.5 6.5a2 2 0 0 1 2-2h4.2l2 2.5h6.8a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/>',
 share:'<circle cx="6.5" cy="12" r="2.4"/><circle cx="17" cy="6.5" r="2.4"/><circle cx="17" cy="17.5" r="2.4"/><path d="M8.7 10.9l6.1-3.3M8.7 13.1l6.1 3.3"/>',
 settings:'<path d="M4 7.5h16M4 16.5h16"/><circle cx="9.5" cy="7.5" r="2.2"/><circle cx="14.5" cy="16.5" r="2.2"/>',
 upload:'<path d="M12 15.5V5M8 9l4-4 4 4"/><path d="M4.5 15.5V18a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-2.5"/>',
 download:'<path d="M12 4.5V15M8 11l4 4 4-4"/><path d="M4.5 15.5V18a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-2.5"/>',
 rename:'<path d="M4.5 19.5l.9-3.6L15.7 5.6a2 2 0 0 1 2.8 2.8L8.2 18.7z"/><path d="M14.5 7l2.8 2.8"/>',
 move:'<path d="M3.5 6.5a2 2 0 0 1 2-2h4.2l2 2.5h6.8a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/><path d="M9 11.5h6M13.5 9l2.5 2.5-2.5 2.5"/>',
 copy:'<rect x="8.5" y="8.5" width="11" height="11" rx="2"/><path d="M5.5 15.5v-10a2 2 0 0 1 2-2h9"/>',
 del:'<path d="M4.5 6.5h15M9.5 6.5V5a1.5 1.5 0 0 1 1.5-1.5h2A1.5 1.5 0 0 1 14.5 5v1.5"/><path d="M6.5 6.5l.8 12a2 2 0 0 0 2 1.9h5.4a2 2 0 0 0 2-1.9l.8-12"/><path d="M10 10.5v6M14 10.5v6"/>',
 eye:'<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="2.8"/>',
 plus:'<path d="M12 5v14M5 12h14"/>',
 refresh:'<path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3"/><path d="M19.5 3.5v4h-4"/>',
 power:'<path d="M12 3.5v7"/><path d="M6.8 6.6a7.5 7.5 0 1 0 10.4 0"/>',
 warn:'<path d="M12 4L2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17.5h.01"/>',
 close:'<path d="M6 6l12 12M18 6L6 18"/>',
 more:'<path stroke-width="2.6" d="M5.5 12h.01M12 12h.01M18.5 12h.01"/>',
 file:'<path d="M6 3.5h7.5L18 8v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-13.5A2 2 0 0 1 6 3.5z"/><path d="M13.5 3.5V8H18"/>',
 image:'<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><circle cx="8.8" cy="9.5" r="1.6"/><path d="M4.5 17.5l5-5 3.5 3.5 3-3 3.5 3.5"/>',
 video:'<rect x="3" y="5.5" width="13" height="13" rx="2.5"/><path d="M16 10l4.5-2.5v9L16 14z"/>',
 audio:'<path d="M9 17.5V6.8l9-1.8v9"/><circle cx="6.5" cy="17.5" r="2.5"/><circle cx="15.5" cy="14" r="2.5"/>',
 text:'<path d="M6 3.5h7.5L18 8v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-13.5A2 2 0 0 1 6 3.5z"/><path d="M8 12h8M8 15.5h8M8 8.5h4"/>',
 caret:'<path d="M6 14.5l6-6 6 6"/>',
 home:'<path d="M3.5 11.5L12 4.5l8.5 7"/><path d="M5.8 10.5V19a1.5 1.5 0 0 0 1.5 1.5h9.4A1.5 1.5 0 0 0 18.2 19v-8.5"/>',
 drive:'<rect x="3" y="8.5" width="18" height="9" rx="2"/><path d="M3 13.5h18"/><path d="M16.8 16h.01M13.8 16h.01"/>',
 folderPlus:'<path d="M3.5 6.5a2 2 0 0 1 2-2h4.2l2 2.5h6.8a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/><path d="M12 11.5v6M9 14.5h6"/>',
 scissors:'<circle cx="6.5" cy="6.5" r="2.5"/><circle cx="6.5" cy="17.5" r="2.5"/><path d="M8.7 8.2L20 19M8.7 15.8L20 5"/>',
 clip:'<path d="M9 4.5h6M8 6.5H6.5a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-10a2 2 0 0 0-2-2H16"/><rect x="8" y="3" width="8" height="3.5" rx="1.2"/>',
 archive:'<rect x="3.5" y="5" width="17" height="4" rx="1.4"/><path d="M5 9v9.5a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 19 18.5V9"/><path d="M10 12.5h4"/>',
 extract:'<rect x="3.5" y="5" width="17" height="4" rx="1.4"/><path d="M5 9v9.5a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 19 18.5V9"/><path d="M12 11.5v5.5M9.5 14.2l2.5 2.5 2.5-2.5"/>',
 info:'<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5.5M12 7.7h.01"/>',
 panel:'<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M15 4.5v15"/>',
 check:'<path d="M5 12.5l4.5 4.5L19 7"/>',
 link:'<path d="M9.5 14.5l5-5"/><path d="M11 7.5l1.2-1.2a3.5 3.5 0 0 1 5 5L16 12.5"/><path d="M13 16.5l-1.2 1.2a3.5 3.5 0 0 1-5-5L8 11.5"/>',
 sort:'<path d="M4.5 7h9M4.5 12h6M4.5 17h3"/><path d="M17.5 5.5v13M14.5 15.5l3 3 3-3"/>',
 grid:'<rect x="3.5" y="3.5" width="7" height="7" rx="1.4"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.4"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.4"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.4"/>',
 terminal:'<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M7 9.5l3 2.5-3 2.5"/><path d="M12.5 15h4.5"/>'
};
function iconSvg(name){return '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">'+(ICONS[name]||ICONS.file)+'</svg>'}
const hex=b=>[...new Uint8Array(b)].map(x=>('0'+x.toString(16)).slice(-2)).join('');
const joinPath=(p,n)=>(p==='/'?'':p)+'/'+n;
function extKind(name){
 const i=name.lastIndexOf('.');
 if(i<0)return'other';
 const e=name.slice(i+1).toLowerCase();
 for(const k in EXT)if((' '+EXT[k]+' ').indexOf(' '+e+' ')>=0)return k;
 return'other';
}
function typeIcon(name){
 const k=extKind(name);
 if(k==='archive')return 'archive';
 if(k==='other'||k==='pdf'||k==='doc')return 'file';
 return k;
}
function svDecodeText(buf){
 const b=new Uint8Array(buf);
 let enc='utf-8',skip=0;
 if(b.length>=2&&b[0]===0xFF&&b[1]===0xFE){enc='utf-16le';skip=2}
 else if(b.length>=2&&b[0]===0xFE&&b[1]===0xFF){enc='utf-16be';skip=2}
 else if(b.length>=3&&b[0]===0xEF&&b[1]===0xBB&&b[2]===0xBF)skip=3;
 else{
  const n=Math.min(b.length-(b.length%2),512);
  if(n>=8){
   let even=0,odd=0,pairs=n/2;
   for(let i=0;i<n;i+=2){if(b[i]===0)even++;if(b[i+1]===0)odd++}
   if(odd*10>=pairs*8&&even*10<pairs*2)enc='utf-16le';
   else if(even*10>=pairs*8&&odd*10<pairs*2)enc='utf-16be';
  }
 }
 try{return new TextDecoder(enc).decode(skip?b.subarray(skip):b)}catch(e){return ''}
}
function svMediaFallback(node,msg){
 node.addEventListener('error',()=>{
  const s=document.createElement('span');
  s.textContent=msg;
  if(node.parentNode)node.replaceWith(s);
 });
}
function svFillPreview(box,data){
 if(!data)return false;
 box.textContent='';
 if(data.kind==='archive'){
  const entries=data.entries||[];
  if(!entries.length){
   box.append(document.createTextNode(data.note||'压缩包是空的'));
   return true;
  }
  const pre=document.createElement('pre');
  const lines=entries.map(e=>(e.dir?'[文件夹] ':'')+e.name+(e.dir?'':'  '+fmtSize(e.size||0)));
  if(data.truncated)lines.push('… 仅列出前 '+entries.length+' 项');
  if(data.note)lines.push(data.note);
  pre.textContent=lines.join('\n');
  box.append(pre);
  return true;
 }
 if(data.text){
  const pre=document.createElement('pre');
  pre.textContent=data.text;
  box.append(pre);
  return true;
 }
 if(data.note){
  box.append(document.createTextNode(data.note));
  return true;
 }
 return false;
}
function fmtSize(n){
 if(n==null||isNaN(n))return'\u2014';
 if(n<1024)return n+' B';
 const u=['KB','MB','GB','TB','PB'];let i=-1;
 do{n/=1024;i++}while(n>=1024&&i<u.length-1);
 return(n>=100?Math.round(n):n.toFixed(1))+' '+u[i];
}
function fmtTime(t){
 if(t==null)return'';
 const d=new Date(typeof t==='number'?(t>1e12?t:t*1000):t);
 return isNaN(d)?'':d.toLocaleString();
}
function toast(msg,ok){
 const box=document.getElementById('toasts');
 if(!box)return;
 const t=document.createElement('div');
 t.className='toast'+(ok?'':' err');
 t.textContent=msg;
 box.append(t);
 setTimeout(()=>{t.classList.add('out');setTimeout(()=>t.remove(),260)},3000);
}`
