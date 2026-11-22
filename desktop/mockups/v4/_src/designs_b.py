FIX = [['Navigation', 'Left sidebar (fixed): Overview, the pages, Computers, Add a computer', 1],
       ['Files', 'Files page (fixed): one tree across every computer, actions on the row', 1],
       ['Overview, Servers, Devices, Transfers, Tools, History, Settings', 'Fixed pages; lists open their details in the inspector on the right', 1]]
DESIGNS = [
# ------------------------------------------------------------------ 06
dict(n=6, slug='drawer', title='Drawer — search slides in from the right, results grouped by folder',
fonts='family=Figtree:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='Search is a drawer on the right, where details live on every other page, so the page you were on stays in view. It says which computers it searched and groups what it found by folder, because a folder is usually what you were after.',
map=FIX + [['Search', 'Find files in the top bar, Ctrl K, / or the sidebar', 1], ['Which computers', 'The line under the field; each computer can be switched off', 2], ['By folder', 'Results grouped by folder; Open folder on each group', 2]],
css=r'''
.D{--bg:#f6f6f1;--sf:#fff;--rz:#ecece4;--t1:#1d2016;--t2:#4f5446;--t3:#6e7363;--line:rgba(29,32,22,.08);--line2:rgba(29,32,22,.15);--ac:#4d6b1f;--ac-ink:#435e1a;--ac-soft:rgba(77,107,31,.09);--font:"Figtree",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{height:54px;flex:none;display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}
.D .b-sr{background:rgba(29,32,22,.14)}
.D .b-sr .m{right:0;top:0;bottom:0;width:460px;background:var(--sf);box-shadow:-16px 0 40px rgba(29,32,22,.14),-1px 0 0 var(--line2);display:grid;grid-template-rows:auto auto minmax(0,1fr) auto}
.D .qf{display:flex;align-items:center;gap:10px;padding:16px 20px 12px}.D .qf label{flex:1;display:flex;align-items:center;gap:10px;height:42px;padding:0 12px;border-radius:10px;box-shadow:inset 0 0 0 1px var(--ac),0 0 0 3px var(--ac-soft);color:var(--t3)}.D .qf input{flex:1;border:0;outline:0;background:none;font:16px var(--font);color:var(--t1)}
.D .qw{padding:0 20px 12px;border-bottom:1px solid var(--line);display:grid;gap:6px}.D .qw p{margin:0;font-size:12px;color:var(--t3)}
.D .qw .qp{display:flex;flex-wrap:wrap;gap:6px}.D .qw .qp button{display:inline-flex;align-items:center;gap:6px;height:26px;padding:0 10px;border-radius:13px;font-size:12px;color:var(--t2);box-shadow:inset 0 0 0 1px var(--line2)}.D .qw .qp button.on{background:var(--ac-soft);color:var(--ac-ink);box-shadow:inset 0 0 0 1px var(--ac)}.D .qw .qp button:disabled{opacity:.55}
.D .qg{overflow:auto;padding:8px 12px}
.D .qh{display:flex;align-items:center;gap:8px;padding:12px 8px 6px;font-size:13px;font-weight:500}.D .qh small{font:11px var(--mono);color:var(--t3);font-weight:400}.D .qh .k-btn{margin-left:auto;height:26px}
.D .qi{display:grid;grid-template-columns:22px 1fr auto;gap:10px;align-items:center;padding:7px 8px 7px 30px;border-radius:7px;font-size:13px;cursor:pointer}.D .qi:hover{background:var(--rz)}.D .qi small{font:11px var(--mono);color:var(--t3)}
.D .qb{display:flex;align-items:center;gap:8px;padding:12px 20px;border-top:1px solid var(--line);font-size:12px;color:var(--t3)}.D .qb .sp{flex:1}
.D .qn{display:grid;gap:10px;padding:28px 20px;color:var(--t2);font-size:13px;justify-items:start}.D .qn b{color:var(--t1);font-size:15px;font-weight:500}.D .qn p{margin:0}
''',
js=r'''
document.getElementById('top').innerHTML=`<button class="b-st" data-k="go:search">${I('search',16)}Find files${sk('Ctrl K')}</button>`+K.C.actions();
const groups=[['garden-plan','studio-pc','/home/sam/Projects',['north-bed-after.jpg','north-bed-before.jpg','bed-layout.pdf']],['Garden','work-laptop','/Users/sam/Documents',['bed-layout-v2.pdf']],['2025','studio-pc','/home/sam/Photos',['raised-beds']],['Desktop','work-laptop','/Users/sam',['bedroom-paint.txt']]];
const R=n=>S.res.find(r=>r.n===n);
const search=()=>`<div class="m" role="dialog" aria-label="Search"><div class="qf"><label>${I('search',18)}<input value="${S.q}" placeholder="File or folder name" aria-label="Search"></label>${K.ib('x','Close (Esc)','sr:close')}</div>
<div class="qw"><p>${EM?'Searching this computer’s Downloads':'Searching 2 of 4 computers'}</p><div class="qp">${EM?'<button class="on">This computer</button>':D.servers.map(s=>`<button class="${s.dot==='ok'?'on':''}" data-k="sr:pick" data-multi${s.dot==='ok'?'':' disabled'}>${K.dot(s.dot)}${s.name}${s.dot==='ok'?'':`<small>· ${s.dot==='bad'?'not trusted':'offline'}</small>`}</button>`).join('')}</div></div>
<div class="qg">${EM?`<div class="qn">${E('file_folder',36)}<b>No computers to look in</b><p>Search finds files by name on every computer you add, and groups them by the folder they sit in.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:groups.map(g=>`<div class="qh">${E('file_folder',16)}${g[0]}<small>${g[1]} · ${g[2]}</small>${K.btn('Open folder',{k:'sm ghost',a:'sr:go:files'})}</div>${g[3].map(n=>{const r=R(n);return `<div class="qi" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)}</span><small>${r.size}</small></div>`}).join('')}`).join('')}</div>
<div class="qb">${EM?'<span>Esc closes</span>':'<span>6 files in 4 folders</span><span class="sp"></span>'+K.btn('Show all in Files',{k:'sm',a:'sr:go:files'})}</div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 07
dict(n=7, slug='launcher', title='Launcher — one field finds files on the left and offers actions on the right',
fonts='family=Inter+Tight:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='One field for both questions: where is it, and what do I do. Files it found are on the left, the actions that fit them and the app’s common jobs on the right, each with its key. Nothing is mixed into one long list.',
map=FIX + [['Search', 'Search or do in the top bar, Ctrl K, / or the sidebar', 1], ['Actions', 'Right column: actions for the found files, then common jobs', 2], ['Keys', 'Each row shows its key; Tab moves between the columns', 2]],
css=r'''
.D{--bg:#0f1420;--sf:#151b29;--rz:#1e2636;--t1:#e7ebf3;--t2:#a2abbd;--t3:#7e889b;--line:rgba(231,235,243,.08);--line2:rgba(231,235,243,.14);--ac:#7aa2f7;--on-ac:#0f1420;--ac-ink:#9ab8fa;--ac-soft:rgba(122,162,247,.13);--pos:#5cc48d;--neg:#ef6b73;--warn:#e7b45c;--font:"Inter Tight",system-ui,sans-serif;--mono:"JetBrains Mono",monospace;--shadow:0 20px 50px rgba(0,0,0,.5)}
.D .k-btn.pri{background:var(--ac);color:#0f1420;box-shadow:none}.D .k-qr{background:#e7ebf3}
.D .top{height:54px;flex:none;display:flex;align-items:center;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}.D .top .b-st{margin-right:auto}
.D .b-sr{background:rgba(5,8,14,.6)}
.D .b-sr .m{left:calc(50% + 120px);top:70px;transform:translateX(-50%);width:860px;background:var(--sf);border-radius:14px;box-shadow:var(--shadow),0 0 0 1px var(--line2);overflow:hidden}
.D .qf{display:flex;align-items:center;gap:12px;padding:16px 20px;border-bottom:1px solid var(--line);color:var(--t3)}.D .qf input{flex:1;border:0;outline:0;background:none;font:19px var(--font);color:var(--t1)}
.D .qc{display:grid;grid-template-columns:1fr 320px;min-height:360px}
.D .qc>div{padding:10px}.D .qc>div+div{border-left:1px solid var(--line);background:rgba(0,0,0,.12)}
.D h4{font:500 11px var(--font);color:var(--t3);margin:8px 10px 6px;letter-spacing:.03em}
.D .qi{display:grid;grid-template-columns:22px 1fr auto;gap:10px;align-items:center;padding:8px 10px;border-radius:8px;font-size:13px;cursor:pointer;width:100%;text-align:left;color:var(--t1)}.D .qi:hover,.D .qi.on{background:var(--rz)}.D .qi.on{box-shadow:inset 0 0 0 1px var(--line2)}
.D .qi small{display:block;color:var(--t3);font:11px var(--mono)}.D .qi .k-ic{color:var(--t2)}
.D .qb{display:flex;gap:16px;padding:10px 20px;border-top:1px solid var(--line);font-size:12px;color:var(--t3)}.D .qb .sp{flex:1}
.D .qn{padding:40px 16px;display:grid;gap:8px;justify-items:start;color:var(--t2);font-size:13px}.D .qn b{color:var(--t1);font-size:15px;font-weight:500}
''',
js=r'''
document.getElementById('top').innerHTML=`<button class="b-st" data-k="go:search">${I('search',16)}Search or do${sk('Ctrl K')}</button>`+K.C.actions();
const act=(ic,t,k,a='',on='')=>`<button class="qi ${on}" ${a?`data-k="${a}"`:''}>${I(ic,16)}<span>${t}</span>${k?sk(k):''}</button>`;
const search=()=>`<div class="m" role="dialog" aria-label="Search or do"><label class="qf">${I('search',20)}<input value="${S.q}" placeholder="${EM?'Type a file name, or what you want to do':'File name or an action'}" aria-label="Search or do">${sk('Esc')}</label>
<div class="qc"><div>${EM?`<h4>Files</h4><div class="qn">${E('magnifying_glass_tilted_left',36)}<b>No files to find yet</b>Add a computer and its files show up here as you type. The actions on the right work now.</div>`:`<h4>Files · 6</h4>${S.res.map((r,i)=>`<div class="qi${i?'':' on'}" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)}<small>${r.pc} · ${r.path}</small></span><small>${r.size}</small></div>`).join('')}`}</div>
<div>${EM?`<h4>Do</h4>${act('plus','Add a computer','Enter','dlg:newconn','on')}${act('radar','Find computers on this network','','dlg:newconn:Find on network')}${act('sliders','Open Settings','Ctrl ,','sr:go:settings')}${act('info','Show the welcome tour','','dlg:welcome')}`:`<h4>For north-bed-after.jpg</h4>${act('folder','Open in Files','Enter','sr:go:files')}${act('download','Download','Ctrl D')}${act('link','Share link…','Ctrl L','dlg:share')}${act('eye','Preview','Space','dlg:preview')}<h4>Do</h4>${act('qr','Pair a phone','','dlg:pairphone')}${act('pause','Pause all transfers','')}${act('plus','Add a computer','','dlg:newconn')}`}</div></div>
<div class="qb"><span>${sk('↑')} ${sk('↓')} move</span><span>${sk('Tab')} files ⇄ actions</span><span class="sp"></span><span>${EM?'Searches this computer’s Downloads':'media-mini offline, not searched'}</span></div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 08
dict(n=8, slug='index', title='Index — results as a table, each column filters itself',
fonts='family=Atkinson+Hyperlegible:wght@400;700&family=JetBrains+Mono:wght@400;500',
idea='Search answers as a table you can read across: name, computer, folder, changed, size. The head of each column is its own filter, so there is no separate filter panel to learn. Sort by any column; save the view.',
map=FIX + [['Search', 'Search icon in the top bar, Ctrl K, / or the sidebar', 1], ['Filters', 'In the head of each column', 2], ['Sort and save', 'Click a column name to sort; Save view', 2]],
css=r'''
.D{--bg:#fafaf8;--sf:#fff;--rz:#f0f0ec;--t1:#161616;--t2:#4a4a46;--t3:#6d6d68;--line:rgba(22,22,22,.08);--line2:rgba(22,22,22,.15);--ac:#b4351f;--ac-ink:#a32f1b;--ac-soft:rgba(180,53,31,.08);--font:"Atkinson Hyperlegible",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{height:52px;flex:none;display:flex;align-items:center;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}.D .top .ct{margin-right:auto;font-size:13px;color:var(--t2)}
.D .b-sr{background:rgba(22,22,22,.3)}
.D .b-sr .m{left:calc(50% + 120px);top:48px;transform:translateX(-50%);width:980px;background:var(--sf);border-radius:10px;box-shadow:0 24px 60px rgba(0,0,0,.22),0 0 0 1px var(--line2);overflow:hidden}
.D .qhd{display:flex;align-items:center;gap:12px;padding:14px 18px;border-bottom:1px solid var(--line)}.D .qhd b{font-size:16px}.D .qhd small{color:var(--t3);font-size:12px}.D .qhd .sp{flex:1}
.D .qt{display:grid;grid-template-columns:28px 1.5fr 120px 1.4fr 96px 80px;align-items:center}
.D .qcol{display:grid;gap:4px;padding:10px 8px;background:var(--rz);border-bottom:1px solid var(--line2);font-size:12px;color:var(--t2);align-self:stretch}.D .qcol button{display:flex;align-items:center;gap:4px;font-weight:700;color:var(--t1);font-size:12px}
.D .qcol input,.D .qcol select{height:28px;border-radius:6px;border:0;box-shadow:inset 0 0 0 1px var(--line2);background:var(--sf);font:12px var(--font);padding:0 8px;color:var(--t1);width:100%;min-width:0}
.D .qcol input.on{box-shadow:inset 0 0 0 1.5px var(--ac)}
.D .qc{padding:0 8px;height:44px;display:flex;align-items:center;gap:6px;border-bottom:1px solid var(--line);font-size:13px;min-width:0;cursor:pointer;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.D .qc.mono{font:12px var(--mono);color:var(--t2)}.D .qc:first-child{padding-left:14px}
.D .qrow:hover .qc{background:var(--rz)}.D .qrow{display:contents}
.D .qb{display:flex;align-items:center;gap:12px;padding:10px 18px;font-size:12px;color:var(--t3)}.D .qb .sp{flex:1}
.D .qn{grid-column:1/-1;display:flex;gap:16px;align-items:center;padding:28px 18px;color:var(--t2);font-size:13px}.D .qn p{margin:0;flex:1}
''',
js=r'''
document.getElementById('top').innerHTML=`<span class="ct">${EM?'No computers yet':'2 of 4 computers up · 48,210 items indexed'}</span>`+K.ib('search','Search (Ctrl K)','go:search')+K.C.actions();
const col=(t,f,arrow='')=>`<div class="qcol"><button>${t}${arrow}</button>${f}</div>`;
const opt=a=>`<select aria-label="filter">${a.map(o=>`<option>${o}</option>`).join('')}</select>`;
const search=()=>`<div class="m" role="dialog" aria-label="Search"><div class="qhd">${E('card_index_dividers',22)}<b>Search</b><small>${EM?'this computer’s Downloads':'6 of 48,210 items · media-mini offline and nas not trusted were skipped'}</small><span class="sp"></span>${EM?'':K.btn('Save view',{k:'sm ghost',i:'star'})}${K.ib('x','Close (Esc)','sr:close')}</div>
<div class="qt"><div class="qcol"></div>${col('Name',`<input class="on" value="${S.q}" placeholder="contains…" aria-label="Name contains">`,I('chevd',12))}${col('Computer',opt(EM?['This computer']:['Any','studio-pc','work-laptop']))}${col('Folder','<input placeholder="under…" aria-label="Folder">')}${col('Changed',opt(['Any time','Today','This week','This month','This year']),I('chevd',12))}${col('Size',opt(['Any','> 1 MB','> 100 MB','> 1 GB']))}
${EM?`<div class="qn">${E('desktop_computer',32)}<p>No rows yet. Each computer you add brings its files into this table, one row per match.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:S.res.map(r=>`<div class="qrow" data-k="sr:go:files"><span class="qc">${K.fic(r.k)}</span><span class="qc"><span>${hl(r.n)}</span></span><span class="qc mono">${r.pc}</span><span class="qc mono">${r.path}</span><span class="qc mono">${r.date}</span><span class="qc mono">${r.size}</span></div>`).join('')}</div>
<div class="qb"><span>${sk('Enter')} open in Files</span><span>${sk('Ctrl D')} download</span><span class="sp"></span><span>Sorted by changed, newest first</span></div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 09
dict(n=9, slug='pocket', title='Pocket — search opens beside the sidebar, from the Search item itself',
fonts='family=Karla:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='Search lives where you click it: it opens as a panel beside the sidebar, next to the Search item, and the page stays visible on the right. Recent and saved searches wait above the field until you type.',
map=FIX + [['Search', 'Search in the sidebar, Ctrl K or /', 1], ['Recent and saved', 'Above the results; one click repeats one', 2], ['Open a result', 'Click: it opens in Files and the panel closes', 2]],
css=r'''
.D{--bg:#f3f6f5;--sf:#fff;--rz:#e6eceb;--t1:#122120;--t2:#425654;--t3:#627573;--line:rgba(18,33,32,.08);--line2:rgba(18,33,32,.15);--ac:#00756a;--ac-ink:#00685e;--ac-soft:rgba(0,117,106,.09);--font:"Karla",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{height:52px;flex:none;display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}
.D.sr-open .side [data-k="go:search"]{background:var(--ac-soft);color:var(--ac-ink)}
.D .b-sr{left:240px;background:rgba(18,33,32,.12)}
.D .b-sr .m{left:0;top:0;bottom:0;width:420px;background:var(--sf);box-shadow:16px 0 40px rgba(18,33,32,.12),1px 0 0 var(--line2);display:grid;grid-template-rows:auto auto minmax(0,1fr) auto}
.D .qf{display:flex;align-items:center;gap:10px;margin:14px 16px 10px;height:40px;padding:0 12px;border-radius:10px;background:var(--rz);color:var(--t3)}.D .qf input{flex:1;border:0;outline:0;background:none;font:16px var(--font);color:var(--t1)}
.D .qs{padding:0 16px 10px;display:flex;flex-wrap:wrap;gap:6px;border-bottom:1px solid var(--line)}
.D .qs button{display:inline-flex;gap:6px;align-items:center;height:26px;padding:0 10px;border-radius:13px;font-size:12px;color:var(--t2);background:var(--rz)}
.D .qs small{width:100%;font-size:11px;color:var(--t3)}
.D .ql{overflow:auto;padding:6px 8px}.D h4{font:600 12px var(--font);color:var(--t3);margin:10px 8px 4px}
.D .qi{display:grid;grid-template-columns:22px 1fr;gap:10px;align-items:center;padding:8px;border-radius:8px;font-size:14px;cursor:pointer}.D .qi:hover,.D .qi.on{background:var(--rz)}.D .qi small{display:block;font:11px var(--mono);color:var(--t3)}
.D .qb{display:flex;align-items:center;gap:12px;padding:10px 16px;border-top:1px solid var(--line);font-size:12px;color:var(--t3)}
.D .qn{display:grid;gap:10px;justify-items:start;padding:24px 8px;color:var(--t2);font-size:14px}.D .qn b{color:var(--t1);font-weight:600}.D .qn p{margin:0}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.actions();
const search=()=>`<div class="m" role="dialog" aria-label="Search"><label class="qf">${I('search',18)}<input value="${S.q}" placeholder="Search your computers" aria-label="Search">${sk('Esc')}</label>
<div class="qs">${EM?'<small>Recent and saved searches appear here.</small>':`<small>Saved and recent</small>${S.saved.map(x=>`<button>${E('star',12)}${x}</button>`).join('')}${S.recent.map(x=>`<button>${I('history',12)}${x}</button>`).join('')}`}</div>
<div class="ql">${EM?`<div class="qn">${E('mobile_phone_with_arrow',36)}<b>Add a computer to search it</b><p>Until then, search looks in this computer’s Downloads folder.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:['studio-pc','work-laptop'].map(pc=>`<h4>${pc}</h4>${S.res.filter(r=>r.pc===pc).map((r,i)=>`<div class="qi${pc==='studio-pc'&&!i?' on':''}" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)}<small>${r.path} · ${r.size}</small></span></div>`).join('')}`).join('')}</div>
<div class="qb">${EM?'<span>Ctrl K opens this from anywhere</span>':'<span>6 found · media-mini offline</span>'}</div></div>`;
B.mount({search});
'''),
# ------------------------------------------------------------------ 10
dict(n=10, slug='dock', title='Dock — a search pill at the foot of every page that opens upward',
fonts='family=Bricolage+Grotesque:opsz,wght@12..96,400;12..96,500;12..96,600&family=JetBrains+Mono:wght@400;500',
idea='No search in the top bar at all: a small pill sits at the foot of every page, where your eyes land after reading. It opens upward into results, with the scope switch beside the field, and closes back into the pill.',
map=FIX + [['Search', 'The pill at the foot of every page, Ctrl K, / or the sidebar', 1], ['Scope', 'Switch beside the field: all computers, one, this computer', 2], ['Open a result', 'Click a row: it opens in Files', 2]],
css=r'''
.D{--bg:#16130f;--sf:#1e1a15;--rz:#2a251e;--t1:#f1ebe2;--t2:#b3a99b;--t3:#8f8577;--line:rgba(241,235,226,.08);--line2:rgba(241,235,226,.14);--ac:#9ccf6a;--on-ac:#16130f;--ac-ink:#b2dc88;--ac-soft:rgba(156,207,106,.12);--pos:#9ccf6a;--neg:#ec6a5a;--warn:#e8b45a;--font:"Bricolage Grotesque",system-ui,sans-serif;--mono:"JetBrains Mono",monospace;--shadow:0 20px 50px rgba(0,0,0,.55)}
.D .k-btn.pri{background:var(--ac);color:#16130f;box-shadow:none}.D .k-qr{background:#f1ebe2}
.D .top{height:52px;flex:none;display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}
.D .stage{padding-bottom:72px}
.D #pop{left:calc(50% + 120px);bottom:18px;transform:translateX(-50%);display:flex;align-items:center;gap:10px;height:40px;padding:0 8px 0 14px;border-radius:20px;background:var(--rz);box-shadow:var(--shadow),inset 0 0 0 1px var(--line2);color:var(--t2);font-size:13px;cursor:pointer;z-index:20}
.D #pop:hover{color:var(--t1)}.D.sr-open #pop{display:none}
.D .b-sr{background:rgba(0,0,0,.45)}
.D .b-sr .m{left:calc(50% + 120px);bottom:18px;transform:translateX(-50%);width:680px;background:var(--sf);border-radius:18px;box-shadow:var(--shadow),0 0 0 1px var(--line2);overflow:hidden;display:flex;flex-direction:column}
.D .ql{max-height:430px;overflow:auto;padding:10px}.D h4{font:500 12px var(--font);color:var(--t3);margin:8px 10px 4px}
.D .qi{display:grid;grid-template-columns:22px 1fr auto auto;gap:12px;align-items:center;padding:9px 10px;border-radius:10px;font-size:14px;cursor:pointer}.D .qi:hover,.D .qi.on{background:var(--rz)}.D .qi small{font:11px var(--mono);color:var(--t3)}.D .qi .qpc{font:11px var(--mono);color:var(--t2)}
.D .qf{display:flex;align-items:center;gap:10px;padding:10px 10px 10px 16px;border-top:1px solid var(--line);color:var(--t3)}.D .qf input{flex:1;border:0;outline:0;background:none;font:17px var(--font);color:var(--t1)}
.D .qf .k-seg{flex:none}
.D .qn{display:flex;gap:14px;align-items:center;padding:20px 12px;color:var(--t2);font-size:14px}.D .qn p{margin:0;flex:1}.D .qn b{display:block;color:var(--t1);font-weight:500}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.actions();
const pill=document.getElementById('pop');pill.dataset.k='go:search';pill.innerHTML=`${I('search',16)}<span>Search your computers</span>${sk('/')}`;
const search=()=>`<div class="m" role="dialog" aria-label="Search"><div class="ql">${EM?`<div class="qn">${E('magnifying_glass_tilted_left',36)}<p><b>Nothing to search yet</b>Search covers every computer you add, and this computer’s Downloads.</p>${K.btn('Add a computer',{i:'plus',k:'pri',a:'dlg:newconn'})}</div>`:`<h4>Recent</h4>${S.recent.slice(0,2).map(x=>`<div class="qi">${I('history',16)}<span>${x}</span><span></span><span></span></div>`).join('')}<h4>Found · 6</h4>${S.res.map((r,i)=>`<div class="qi${i?'':' on'}" data-k="sr:go:files">${K.fic(r.k)}<span>${hl(r.n)} <small>${r.path}</small></span><span class="qpc">${r.pc}</span><small>${r.size}</small></div>`).join('')}`}</div>
<label class="qf">${I('search',18)}<input value="${S.q}" placeholder="Search your computers" aria-label="Search">${EM?K.seg(['This computer'],'This computer'):K.seg(['All','studio-pc','work-laptop','This computer'],'All')}</label></div>`;
B.mount({search});
'''),
]
