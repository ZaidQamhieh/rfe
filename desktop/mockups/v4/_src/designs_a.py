FIX = [['Navigation', 'Left sidebar (fixed): Overview, the pages, Computers, Add a computer', 1],
       ['Files', 'Files page (fixed): one tree across every computer, actions on the row', 1],
       ['Overview, Servers, Devices, Transfers, Tools, History, Settings', 'Fixed pages; lists open their details in the inspector on the right', 1]]
DESIGNS = [
# ------------------------------------------------------------------ 01
dict(n=1, slug='lantern', title='Lantern — a search window with a preview of the highlighted file',
fonts='family=Instrument+Sans:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='Search is a window over the page. Results are grouped by computer, and the highlighted one shows on the right with its preview and the three things you do with a found file: open it where it lives, download it, share it.',
map=FIX + [['Search', 'Search button in the top bar, Ctrl K, / or Search in the sidebar', 1], ['Scope', 'Chips under the field: everywhere, one computer, this computer', 2], ['Act on a result', 'Preview pane: Open in Files, Download, Share link', 2]],
css=r'''
.D{--bg:#f8f5ef;--sf:#fdfbf7;--rz:#efe9df;--t1:#1f1b16;--t2:#5e554a;--t3:#7b7266;--line:rgba(31,27,22,.08);--line2:rgba(31,27,22,.15);--ac:#a8471f;--ac-ink:#9a3f1a;--ac-soft:rgba(168,71,31,.09);--font:"Instrument Sans",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{height:56px;flex:none;display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}
.D .b-sr{background:rgba(31,27,22,.24)}
.D .b-sr .m{left:50%;top:56px;transform:translateX(-50%);width:900px;height:580px;background:var(--sf);border-radius:16px;box-shadow:0 24px 64px rgba(31,27,22,.25),0 0 0 1px var(--line2);display:grid;grid-template-rows:auto auto minmax(0,1fr) auto;overflow:hidden}
.D .f{display:flex;align-items:center;gap:12px;padding:18px 22px 10px;font-size:20px;color:var(--t3)}.D .f input{flex:1;border:0;background:none;font:400 20px var(--font);color:var(--t1);outline:0}
.D .sc{display:flex;gap:6px;padding:0 22px 14px;border-bottom:1px solid var(--line)}
.D .sc button{height:28px;padding:0 10px;border-radius:14px;font-size:13px;color:var(--t2);box-shadow:inset 0 0 0 1px var(--line2);display:inline-flex;gap:6px;align-items:center}.D .sc button.on{background:var(--t1);color:var(--sf);box-shadow:none}.D .sc small{font:11px var(--mono);opacity:.7}
.D .bd{display:grid;grid-template-columns:1fr 320px;min-height:0}
.D .rs{overflow:auto;padding:8px 10px}.D .rs h4{font:500 12px var(--font);color:var(--t3);margin:12px 12px 4px;display:flex;gap:8px;align-items:center}
.D .qr{display:grid;grid-template-columns:24px 1fr auto;gap:10px;align-items:center;padding:8px 12px;border-radius:8px;cursor:pointer}.D .qr:hover{background:var(--rz)}.D .qr.on{background:var(--ac-soft)}
.D .qr small{display:block;color:var(--t3);font:12px var(--mono)}.D .qr .z{font:12px var(--mono);color:var(--t3)}
.D .pv{border-left:1px solid var(--line);padding:18px;overflow:auto;display:grid;align-content:start;gap:10px}.D .pv>svg{border-radius:10px;width:100%;height:auto;display:block}.D .pv h3{font:500 15px var(--font);margin:4px 0 0}.D .pv .k-kv{margin:0}
.D .ft{display:flex;gap:16px;align-items:center;padding:10px 22px;border-top:1px solid var(--line);font-size:12px;color:var(--t3)}.D .ft .sp{flex:1}
.D .no{grid-column:1/-1;display:grid;justify-items:center;align-content:center;text-align:center;gap:8px;color:var(--t2);padding:24px}.D .no b{color:var(--t1);font-weight:500;font-size:16px}.D .no p{max-width:420px;margin:0 0 8px}
''',
js=r'''
document.getElementById('top').innerHTML=`<button class="b-st" data-k="go:search">${I('search',16)}Search${sk('Ctrl K')}</button>`+K.C.actions();
const grp=pc=>S.res.filter(r=>r.pc===pc);
const search=()=>`<div class="m" role="dialog" aria-label="Search"><label class="f">${I('search',20)}<input value="${S.q}" placeholder="Search file names on your computers" aria-label="Search">${sk('Esc')}</label>
<div class="sc">${EM?`<button class="on">This computer<small>Downloads</small></button>`:[['Everywhere',6],['studio-pc',4],['work-laptop',2],['This computer',0]].map((c,i)=>`<button class="${i?'':'on'}" data-k="sr:pick">${c[0]}<small>${c[1]}</small></button>`).join('')}</div>
<div class="bd">${EM?`<div class="no">${E('magnifying_glass_tilted_left',40)}<b>Nothing to search yet</b><p>Search looks through every computer you add, and this computer’s Downloads folder. Type a name to look there now.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:`<div class="rs">${['studio-pc','work-laptop'].map(pc=>`<h4>${E(pc==='studio-pc'?'desktop_computer':'laptop',16)}${pc}<small>${grp(pc).length}</small></h4>${grp(pc).map(r=>`<div class="qr${r.n==='north-bed-after.jpg'?' on':''}" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)}<small>${r.path}</small></span><span class="z">${r.size}</span></div>`).join('')}`).join('')}</div>
<div class="pv">${K.photo(2,280,180,10)}<h3>north-bed-after.jpg</h3><dl class="k-kv"><dt>On</dt><dd>studio-pc</dd><dt>Folder</dt><dd class="mono">…/Projects/garden-plan</dd><dt>Changed</dt><dd>1 Nov, 09:12</dd><dt>Size</dt><dd>3.8 MB · 4032 × 3024</dd></dl><div class="k-dact">${K.btn('Open in Files',{i:'folder',k:'pri',a:'sr:go:files'})}<div class="k-row">${K.btn('Download',{i:'download'})}${K.btn('Share link…',{i:'link',a:'dlg:share'})}</div></div></div>`}</div>
<div class="ft">${EM?'<span>Nothing indexed yet</span>':`<span>${S.line}</span>`}<span class="sp"></span><span>${sk('↑')} ${sk('↓')} move</span><span>${sk('Enter')} open</span><span>${sk('Ctrl D')} download</span></div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 02
dict(n=2, slug='slate', title='Slate — a dropdown under the search icon, filters as tokens in the field',
fonts='family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500',
idea='Search drops from its icon like any other menu and stays small. Filters are tokens inside the field (where, what kind, when), each one a menu; Add filter offers the rest. The full list opens in Files.',
map=FIX + [['Search', 'Search icon in the top bar, Ctrl K, / or the sidebar', 1], ['Filters', 'Tokens in the field: in, type, changed; Add filter', 2], ['All results', 'Show all in Files at the foot', 2]],
css=r'''
.D{--bg:#f6f7f9;--sf:#fff;--rz:#eceef2;--t1:#15171c;--t2:#4b5160;--t3:#6b7181;--line:rgba(21,23,28,.08);--line2:rgba(21,23,28,.14);--ac:#2f5bd3;--ac-ink:#2a51bd;--ac-soft:rgba(47,91,211,.08);--font:"Geist",system-ui,sans-serif;--mono:"Geist Mono",monospace}
.D .top{height:52px;flex:none;display:flex;align-items:center;justify-content:flex-end;gap:6px;padding:0 16px;border-bottom:1px solid var(--line);background:var(--sf)}
.D.sr-open .top [data-k="go:search"]{background:var(--rz);color:var(--t1)}
.D .b-sr .m{top:48px;right:132px;width:560px;background:var(--sf);border-radius:12px;box-shadow:0 16px 48px rgba(21,23,28,.16),0 0 0 1px var(--line2);overflow:hidden}
.D .f{display:flex;flex-wrap:wrap;gap:6px;align-items:center;padding:10px 12px;border-bottom:1px solid var(--line)}
.D .f input{flex:1;min-width:120px;border:0;outline:0;background:none;font:15px var(--font);color:var(--t1);height:30px}
.D .tk{display:inline-flex;align-items:center;gap:4px;height:26px;padding:0 6px 0 8px;border-radius:6px;background:var(--rz);font-size:12px;color:var(--t2)}.D .tk b{color:var(--t1);font-weight:500}.D .tk.add{background:none;box-shadow:inset 0 0 0 1px dashed;box-shadow:inset 0 0 0 1px var(--line2);color:var(--t3)}
.D .g{padding:6px}.D .g h4{font:500 11px var(--font);color:var(--t3);margin:6px 8px;display:flex;justify-content:space-between}
.D .qr{display:grid;grid-template-columns:22px 1fr auto;gap:10px;align-items:center;padding:7px 8px;border-radius:7px;cursor:pointer;font-size:13px}.D .qr:hover,.D .qr.on{background:var(--rz)}.D .qr small{color:var(--t3);font:11px var(--mono);margin-left:8px}
.D .qpc{font:11px var(--mono);color:var(--t2);padding:2px 6px;border-radius:4px;box-shadow:inset 0 0 0 1px var(--line2)}
.D .ft{display:flex;align-items:center;gap:8px;padding:8px 12px;border-top:1px solid var(--line);font-size:12px;color:var(--t3)}.D .ft .sp{flex:1}
.D .ft .k-btn{height:28px}
.D .no{padding:20px 16px;font-size:13px;color:var(--t2);display:grid;gap:10px;justify-items:start}.D .no b{color:var(--t1);font-weight:500}
''',
js=r'''
document.getElementById('top').innerHTML=K.ib('search','Search (Ctrl K)','go:search')+K.C.actions();
const tok=(k,v)=>`<button class="tk" data-k="sr:pick" data-multi>${k} <b>${v}</b>${I('chevd',12)}</button>`;
const search=()=>`<div class="m" role="dialog" aria-label="Search"><div class="f">${I('search',16)}${EM?tok('in','this computer'):tok('in','all computers')+tok('type','any')}<input value="${S.q}" placeholder="${EM?'Search Downloads on this computer':'File or folder name'}" aria-label="Search"><button class="tk add">${I('plus',12)}Add filter</button></div>
${EM?`<div class="no"><b>No computers to search</b>Until you add one, search looks only in this computer’s Downloads folder, where there is nothing yet.${K.btn('Add a computer',{i:'plus',k:'sm pri',a:'dlg:newconn'})}</div>`:`<div class="g"><h4>Files <span>6 found</span></h4>${S.res.map((r,i)=>`<div class="qr${i?'':' on'}" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)}<small>${r.path}</small></span><span class="qpc">${r.pc}</span></div>`).join('')}</div><div class="g"><h4>Recent searches</h4>${S.recent.map(x=>`<div class="qr">${I('history',14)}<span>${x}</span><span></span></div>`).join('')}</div>`}
<div class="ft">${EM?'<span>Ctrl K opens this from anywhere</span>':'<span>48,210 items · media-mini offline, not searched</span>'}<span class="sp"></span>${EM?'':K.btn('Show all in Files',{k:'sm',i:'right',a:'sr:go:files'})}</div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 03
dict(n=3, slug='sentence', title='Sentence — search written as a sentence you fill in',
fonts='family=Newsreader:opsz,wght@6..72,400;6..72,500&family=IBM+Plex+Sans:wght@400;500&family=IBM+Plex+Mono:wght@400;500',
idea='The search reads as a sentence: find anything named bed on every computer, changed any time. Each underlined word is a menu, so the filters explain themselves. The sheet drops from the top of the page.',
map=FIX + [['Search', 'Find… in the top bar, Ctrl K, / or the sidebar', 1], ['Filters', 'Each underlined word in the sentence is a menu', 2], ['Open a result', 'Click a row: it opens in Files', 2]],
css=r'''
.D{--bg:#f7f4ec;--sf:#fffdf8;--rz:#eee8da;--t1:#211d16;--t2:#5a5246;--t3:#7a7163;--line:rgba(33,29,22,.09);--line2:rgba(33,29,22,.16);--ac:#2d6a4f;--ac-ink:#245a42;--ac-soft:rgba(45,106,79,.09);--font:"IBM Plex Sans",system-ui,sans-serif;--mono:"IBM Plex Mono",monospace;--display:"Newsreader",Georgia,serif}
.D .top{height:56px;flex:none;display:flex;align-items:center;gap:8px;padding:0 24px;border-bottom:1px solid var(--line)}.D .top .d{font:400 18px var(--display);margin-right:auto}
.D .b-sr{left:240px;background:rgba(33,29,22,.18)}
.D .b-sr .m{left:0;right:0;top:0;background:var(--sf);box-shadow:0 18px 40px rgba(33,29,22,.18);padding:28px 40px 20px;max-height:640px;display:grid;gap:16px}
.D .sn{font:400 30px/1.5 var(--display);color:var(--t2);margin:0}
.D .sn button{font:inherit;color:var(--t1);border-bottom:2px dotted var(--ac);padding:0 2px;border-radius:0}.D .sn button:hover{background:var(--ac-soft)}
.D .sn input{font:inherit;color:var(--ac);width:120px;border:0;border-bottom:2px solid var(--ac);background:none;outline:0;padding:0 2px}
.D .ln{font-size:12px;color:var(--t3);display:flex;gap:16px;align-items:center}.D .ln .sp{flex:1}
.D ol{list-style:none;margin:0;padding:0;display:grid;grid-template-columns:1fr 1fr;gap:2px 24px;max-height:330px;overflow:auto}
.D li{display:grid;grid-template-columns:24px 1fr auto;gap:12px;align-items:center;padding:10px 8px;border-top:1px solid var(--line);cursor:pointer}.D li:hover{background:var(--rz)}
.D li b{font:400 18px var(--display)}.D li small{display:block;color:var(--t3);font:12px var(--mono)}.D li .z{font:12px var(--mono);color:var(--t3);text-align:right}
.D .no{display:flex;gap:16px;align-items:center;padding:16px 0;border-top:1px solid var(--line);color:var(--t2)}.D .no p{margin:0;flex:1}
''',
js=r'''
document.getElementById('top').innerHTML=`<span class="d">Sunday, 1 November</span><button class="b-st" data-k="go:search">${I('search',16)}Find…${sk('Ctrl K')}</button>`+K.C.actions();
const w=t=>`<button data-k="sr:pick">${t}</button>`;
const search=()=>`<div class="m" role="dialog" aria-label="Search"><p class="sn">Find ${w('anything')} named <input value="${S.q}" aria-label="Name" placeholder="…"> on ${w(EM?'this computer’s Downloads':'every computer')}, changed ${w('any time')}${EM?'':`, of ${w('any size')}`}.</p>
${EM?`<div class="no">${E('seedling',32)}<p>Your computers are not here yet. Once you add one, this sentence can name it, and search reaches every folder it lets you open.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:`<div class="ln"><span>6 found on studio-pc and work-laptop · media-mini is offline, nas is not trusted</span><span class="sp"></span>${K.btn('Save this sentence',{k:'sm ghost',i:'star'})}${K.btn('Open all in Files',{k:'sm',a:'sr:go:files'})}</div><ol>${S.res.map(r=>`<li data-k="sr:go:files">${K.fic(r.k)}<span><b>${hl(r.n)}</b><small>${r.pc} · ${r.path}</small></span><span class="z">${r.date}<br>${r.size}</span></li>`).join('')}</ol>`}</div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 04
dict(n=4, slug='darkroom', title='Darkroom — pictures shown as pictures, kind chips with counts',
fonts='family=Sora:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='A dark room for people who keep photos and video on their PCs. Search shows what it found by kind: pictures as a contact strip, everything else as rows. Each kind chip carries its count so you know before you click.',
map=FIX + [['Search', 'Search icon in the top bar, Ctrl K, / or the sidebar', 1], ['By kind', 'Chips with counts: all, images, documents, folders, text', 2], ['Preview', 'Click a picture: preview; Enter: open in Files', 2]],
css=r'''
.D{--bg:#121212;--sf:#1a1a1a;--rz:#252525;--t1:#eeeae4;--t2:#aaa49b;--t3:#857f77;--line:rgba(238,234,228,.08);--line2:rgba(238,234,228,.14);--ac:#e2a03f;--on-ac:#121212;--ac-ink:#efb662;--ac-soft:rgba(226,160,63,.13);--pos:#5fbf86;--neg:#e5604d;--warn:#e2a03f;--font:"Sora",system-ui,sans-serif;--mono:"JetBrains Mono",monospace;--shadow:0 16px 40px rgba(0,0,0,.5)}
.D .k-btn.pri{background:var(--ac);color:#121212;box-shadow:none}.D .k-qr{background:#eeeae4}
.D .top{height:54px;flex:none;display:flex;align-items:center;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}.D .top .cs{display:flex;gap:14px;margin-right:auto;font-size:12px;color:var(--t2)}.D .top .cs span{display:flex;gap:6px;align-items:center}
.D .b-sr{background:rgba(0,0,0,.55)}
.D .b-sr .m{left:calc(50% + 120px);top:64px;transform:translateX(-50%);width:820px;background:var(--sf);border-radius:14px;box-shadow:var(--shadow),0 0 0 1px var(--line2);overflow:hidden}
.D .f{display:flex;align-items:center;gap:12px;padding:16px 20px;color:var(--t3)}.D .f input{flex:1;border:0;outline:0;background:none;font:400 19px var(--font);color:var(--t1)}
.D .ch{display:flex;gap:6px;padding:0 20px 14px;border-bottom:1px solid var(--line)}
.D .ch button{height:30px;padding:0 12px;border-radius:8px;font-size:13px;color:var(--t2);background:var(--rz);display:inline-flex;gap:8px;align-items:center}.D .ch button.on{background:var(--ac);color:#121212}.D .ch small{font:11px var(--mono);opacity:.75}
.D .px{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;padding:16px 20px 4px}.D .px div{font-size:12px;color:var(--t2);cursor:pointer}.D .px svg{width:100%;height:auto;border-radius:8px;display:block;margin-bottom:6px}.D .px b{display:block;color:var(--t1);font-weight:500}
.D h4{font:500 11px var(--font);color:var(--t3);margin:14px 20px 4px;letter-spacing:.02em}
.D .rw{display:grid;grid-template-columns:22px 1fr 130px 70px;gap:12px;align-items:center;padding:8px 20px;font-size:13px;cursor:pointer}.D .rw:hover{background:var(--rz)}.D .rw small{color:var(--t3);font:12px var(--mono)}
.D .ft{display:flex;gap:16px;padding:10px 20px;border-top:1px solid var(--line);font-size:12px;color:var(--t3);margin-top:8px}.D .ft .sp{flex:1}
.D .no{padding:32px 20px;display:grid;grid-template-columns:auto 1fr auto;gap:16px;align-items:center;color:var(--t2);font-size:13px}.D .no b{display:block;color:var(--t1);font-weight:500;font-size:15px;margin-bottom:4px}
''',
js=r'''
document.getElementById('top').innerHTML=`<span class="cs">${EM?'<span>No computers yet</span>':D.servers.map(s=>`<span>${K.dot(s.dot)}${s.name}</span>`).join('')}</span>`+K.ib('search','Search (Ctrl K)','go:search')+K.C.actions();
const pics=S.res.filter(r=>r.k==='img'),rest=S.res.filter(r=>r.k!=='img');
const search=()=>`<div class="m" role="dialog" aria-label="Search"><label class="f">${I('search',20)}<input value="${S.q}" placeholder="Search names of photos, videos and files" aria-label="Search">${sk('Esc')}</label>
<div class="ch">${(EM?[['All',0]]:[['All',6],['Images',2],['Documents',2],['Folders',1],['Text',1],['Video',0]]).map((c,i)=>`<button class="${i?'':'on'}" data-k="sr:pick">${c[0]}<small>${c[1]}</small></button>`).join('')}</div>
${EM?`<div class="no">${E('framed_picture',40)}<div><b>Nothing here to look through</b>Add a computer and search finds its photos, video and documents by name, and shows pictures as pictures.</div>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:`<h4>Images</h4><div class="px">${pics.map((r,i)=>`<div data-k="dlg:preview">${K.photo(i?1:2,200,130,8)}<b>${hl(r.n)}</b>${r.pc} · ${r.size}</div>`).join('')}<div data-k="dlg:preview">${K.photo(3,200,130,8)}<b>raised-beds/<mark>bed</mark>-01.jpg</b>studio-pc · in a folder</div></div><h4>Everything else</h4>${rest.map(r=>`<div class="rw" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)} <small>${r.path}</small></span><small>${r.pc}</small><small>${r.size}</small></div>`).join('')}`}
<div class="ft"><span>${EM?'Searches this computer’s Downloads until you add one':S.line}</span><span class="sp"></span><span>${sk('Space')} preview</span><span>${sk('Enter')} open in Files</span></div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 05
dict(n=5, slug='facets', title='Facets — a wide sheet: filters with counts, results, saved searches',
fonts='family=Public+Sans:wght@400;500;600&family=Fira+Code:wght@400;500',
idea='For people who search a lot. The sheet opens under the top bar across the page: on the left every filter with how many results it keeps, in the middle the results as a table, on the right your saved and recent searches.',
map=FIX + [['Search', 'Search button at the left of the top bar, Ctrl K, / or the sidebar', 1], ['Filters', 'Left column: kind, computer, changed, size, each with counts', 2], ['Saved searches', 'Right column; star to save the current one', 2]],
css=r'''
.D{--bg:#f5f8f8;--sf:#fff;--rz:#e9f0f0;--t1:#132226;--t2:#43575c;--t3:#647a80;--line:rgba(19,34,38,.08);--line2:rgba(19,34,38,.15);--ac:#0f766e;--ac-ink:#0d6961;--ac-soft:rgba(15,118,110,.08);--font:"Public Sans",system-ui,sans-serif;--mono:"Fira Code",monospace}
.D .top{height:52px;flex:none;display:flex;align-items:center;gap:6px;padding:0 16px;border-bottom:1px solid var(--line);background:var(--sf)}.D .top .b-st{margin-right:auto}
.D.sr-open .top .b-st{background:var(--ac-soft);color:var(--ac-ink)}
.D .b-sr{left:240px;top:52px;background:rgba(19,34,38,.16)}
.D .b-sr .m{left:0;right:0;top:0;height:500px;background:var(--sf);box-shadow:0 18px 40px rgba(19,34,38,.16);display:grid;grid-template-columns:220px 1fr 220px;grid-template-rows:auto minmax(0,1fr)}
.D .f{grid-column:1/-1;display:flex;align-items:center;gap:12px;padding:12px 20px;border-bottom:1px solid var(--line);color:var(--t3)}.D .f input{flex:1;border:0;outline:0;background:none;font:17px var(--font);color:var(--t1)}
.D .fc{border-right:1px solid var(--line);padding:12px 16px;overflow:auto}.D .fc h5,.D .sv h5{font:600 12px var(--font);color:var(--t2);margin:12px 0 6px}
.D .fc label{display:flex;align-items:center;gap:8px;font-size:13px;height:28px;color:var(--t1)}.D .fc label small{margin-left:auto;font:11px var(--mono);color:var(--t3)}.D .fc label.d{color:var(--t3)}
.D .fc input{accent-color:var(--ac);margin:0}
.D .tb{overflow:auto;padding:4px 0}.D .qth,.D .qtr{display:grid;grid-template-columns:24px 1fr 110px 72px 72px;gap:10px;align-items:center;padding:0 16px}.D .qth{height:34px;font-size:12px;color:var(--t3);border-bottom:1px solid var(--line)}
.D .qtr{min-height:44px;font-size:13px;cursor:pointer;border-bottom:1px solid var(--line)}.D .qtr:hover{background:var(--rz)}.D .qtr small{display:block;color:var(--t3);font:11px var(--mono)}.D .qtr .mono{font-size:12px;color:var(--t2)}
.D .sv{border-left:1px solid var(--line);padding:12px 16px;overflow:auto}.D .sv button{display:flex;gap:8px;align-items:center;width:100%;height:30px;font-size:13px;border-radius:6px;padding:0 6px;color:var(--t1)}.D .sv button:hover{background:var(--rz)}
.D .tl{display:flex;align-items:center;gap:8px;padding:10px 16px;font-size:12px;color:var(--t3)}.D .tl .sp{flex:1}
.D .no{display:grid;justify-items:center;text-align:center;gap:8px;padding:56px 24px;color:var(--t2)}.D .no b{color:var(--t1);font-weight:500}.D .no p{margin:0 0 8px;max-width:380px}
''',
js=r'''
document.getElementById('top').innerHTML=`<button class="b-st" data-k="go:search">${I('search',16)}Search${sk('Ctrl K')}</button>`+K.C.actions();
const cb=(t,n,on,d)=>`<label class="${d?'d':''}"><input type="checkbox"${on?' checked':''}${d?' disabled':''}>${t}<small>${n}</small></label>`;
const search=()=>`<div class="m" role="dialog" aria-label="Search"><label class="f">${I('search',18)}<input value="${S.q}" placeholder="Search by name across your computers" aria-label="Search">${EM?'':K.ib('star','Save this search')}${sk('Esc')}</label>
<div class="fc"><h5>Kind</h5>${EM?cb('Any kind','',1,1):S.types.map(t=>cb(t[0],t[2],0)).join('')}<h5>Computer</h5>${EM?cb('This computer','',1,1):S.pcs.map(p=>cb(p[0],p[2]||p[1],p[1]>0,!p[1])).join('')}<h5>Changed</h5>${['Any time','Today','This week','This month'].map((x,i)=>`<label class="${EM?'d':''}"><input type="radio" name="ch"${i?'':' checked'}>${x}</label>`).join('')}<h5>Size</h5>${['Any size','Over 10 MB','Over 100 MB'].map((x,i)=>`<label class="${EM?'d':''}"><input type="radio" name="sz"${i?'':' checked'}>${x}</label>`).join('')}</div>
<div class="tb">${EM?`<div class="no">${E('card_index_dividers',40)}<b>The filters fill in when there is something to search</b><p>Each computer you add becomes a filter here, with how many results it holds.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:`<div class="tl"><span>6 results, newest first</span><span class="sp"></span><span>media-mini offline · nas not trusted: not searched</span></div><div class="qth"><span></span><span>Name</span><span>Computer</span><span>Changed</span><span>Size</span></div>${S.res.map(r=>`<div class="qtr" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)}<small>${r.path}</small></span><span class="mono">${r.pc}</span><span class="mono">${r.date}</span><span class="mono">${r.size}</span></div>`).join('')}`}</div>
<div class="sv"><h5>Saved</h5>${EM?'<p class="mut" style="font-size:12px">Star a search to keep it here.</p>':S.saved.map(x=>`<button>${E('star',14)}${x}</button>`).join('')}<h5>Recent</h5>${EM?'<p class="mut" style="font-size:12px">None yet.</p>':S.recent.map(x=>`<button>${I('history',14)}${x}</button>`).join('')}</div></div>`;
B.mount({search});
'''),
]
