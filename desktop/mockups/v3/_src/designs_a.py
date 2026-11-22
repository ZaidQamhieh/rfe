FIX = [['Navigation', 'Left sidebar (fixed): Overview, the pages, Computers, Add a computer', 1],
       ['Files', 'Files page (fixed): one tree across every computer, actions on the row, preview on the right', 1],
       ['Settings', 'Settings page (fixed): sections on the left', 1]]

DESIGNS = [
# ------------------------------------------------------------------ 01
dict(n=1, slug='harbor', title='Harbor — a calm top bar and an Overview that is a to-do list',
fonts='family=Figtree:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='The Overview is a short list of what needs you, in the order to act on it, then your computers and recent files. Search and the account sit in a plain top bar; transfers show as a pill there.',
map=FIX + [['Needs you', 'Overview, first list; each row has its one action', 0], ['Transfers', 'Pill in the top bar opens the dock; Transfers page', 1], ['Search', 'Top bar field or Ctrl K', 1], ['Pair a phone', 'Top bar', 1]],
css=r'''
.D{--bg:#f6f8fa;--sf:#fff;--rz:#edf1f5;--t1:#0f1720;--t2:#475467;--t3:#667085;--line:rgba(15,23,32,.08);--line2:rgba(15,23,32,.14);--ac:#2563eb;--ac-ink:#1d4ed8;--ac-soft:rgba(37,99,235,.08);--font:"Figtree",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{height:56px;flex:none;display:flex;align-items:center;gap:8px;padding:0 24px;border-bottom:1px solid var(--line);background:var(--sf)}
.D .top .k-sbox{width:380px;margin-right:auto}
.D .k-dockwrap{position:absolute;right:16px;top:64px;width:520px;display:none;z-index:40}.D .k-dockwrap.open{display:block}
.D .hi{font:500 26px var(--font);margin:0}.D .hs{color:var(--t2);margin:4px 0 24px}
.D .need{display:grid;grid-template-columns:28px 1fr auto;gap:14px;align-items:center;padding:14px 16px;background:var(--sf);border-radius:12px;box-shadow:inset 0 0 0 1px var(--line);margin-bottom:8px}
.D .need b{font-weight:500}.D .need small{display:block;color:var(--t3)}.D .need .k-row{flex-wrap:nowrap}
.D .pcs{display:grid;gap:2px}.D .pcl{display:grid;grid-template-columns:20px 1fr 140px 64px;gap:12px;align-items:center;height:44px;padding:0 8px;border-radius:8px}.D .pcl:hover{background:var(--rz)}.D .pcl small{color:var(--t3);font:12px var(--mono)}.D .pcl .k-bar{margin:0}
.D .rcs{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.D .rcs div{font-size:12px;color:var(--t3);cursor:pointer}.D .rcs svg{display:block;width:100%;height:auto;border-radius:8px;margin-bottom:6px}.D .rcs b{display:block;color:var(--t1);font-weight:500;font-size:13px}
.D .chk{display:grid;gap:8px;max-width:640px}.D .chk div{display:grid;grid-template-columns:32px 1fr auto;gap:14px;align-items:center;padding:16px;border-radius:12px;background:var(--sf);box-shadow:inset 0 0 0 1px var(--line)}
.D .chk div.off{opacity:.55}.D .chk b{font-weight:500}.D .chk small{display:block;color:var(--t3)}
.D .num{width:28px;height:28px;border-radius:14px;display:grid;place-items:center;background:var(--rz);font:500 13px var(--mono);color:var(--t2)}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.searchBox(EM?'Search, once a computer is added':'Search all connected computers')+(EM?'':K.C.pill())+K.C.actions();
document.getElementById('dock').innerHTML=K.C.dock();
const needs=[['phone','Pixel 9 asks to join studio-pc','Accept only if it shows 4829 1307 · 1:42 left',K.btn('Reject',{k:'sm ghost'})+K.btn('Accept…',{k:'sm pri',a:'dlg:accept'})],['warning','nas has a different key','Its files stay closed until you compare',K.btn('Review key…',{k:'sm warn',a:'dlg:keychanged'})],['doc','bed-layout.pdf already exists on work-laptop','Upload to /Users/sam/Documents waits',K.btn('Replace',{k:'sm'})+K.btn('Keep both',{k:'sm pri'})],['memo','notes.txt was damaged while uploading','The upload was discarded',K.btn('Retry',{i:'refresh',k:'sm'})]];
const home=()=>EM?`<h1 class="hi">Welcome to RFE</h1><p class="hs">Three steps and your PCs’ files are here. 0 of 3 done</p><div class="chk">
<div><span class="num">1</span><span><b>Set up a PC</b><small>Install RFE on it and run <code>rfe setup</code>; it prints a pairing code</small></span>${K.btn('How',{k:'sm ghost',a:'dlg:welcome'})}</div>
<div><span class="num">2</span><span><b>Add it here</b><small>By its address, from the network, or with the pairing code</small></span>${K.btn('Add a computer',{i:'plus',k:'sm pri',a:'dlg:newconn'})}</div>
<div class="off"><span class="num">3</span><span><b>Pair your phone</b><small>After step 2: scan a QR code with RFE for Android</small></span>${K.btn('Pair a phone',{k:'sm',dis:1})}</div></div>`
:`<h1 class="hi">Good morning, sam</h1><p class="hs">2 of 4 computers connected · 4 things need you</p>${needs.map(n=>`<div class="need">${E(n[0],24)}<span><b>${n[1]}</b><small>${n[2]}</small></span><span class="k-row">${n[3]}</span></div>`).join('')}
<div class="k-g2" style="margin-top:24px">${K.sec('Computers','',`<div class="pcs">${D.servers.map(s=>`<div class="pcl" data-k="${s.dot==='ok'?'go:files':'go:servers'}">${K.mach(s,18)}<span>${K.dot(s.dot)} ${s.name} <small>${s.ms?s.ms+' ms':s.state.toLowerCase()}</small></span>${K.bar(s.used/s.total*100)}<small>${Math.round(s.used/s.total*100)}%</small></div>`).join('')}</div>`)}${K.sec('Recent files',K.btn('Open Files',{k:'sm ghost',a:'go:files'}),`<div class="rcs">${[[2,'north-bed-after.jpg','studio-pc · 12 min'],[1,'north-bed-before.jpg','downloaded'],[3,'garden-sketch.png','this computer']].map(r=>`<div data-k="dlg:preview">${K.photo(r[0],160,100,8)}<b>${r[1]}</b>${r[2]}</div>`).join('')}</div>`)}</div>`;
B.mount({views:{home}});
'''),
# ------------------------------------------------------------------ 02
dict(n=2, slug='night', title='Night — the dashboard Overview, transfers in a corner dock',
fonts='family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500',
idea='A dark dashboard: four numbers, today’s throughput, what waits for you, computers and recent files. Transfers live in a dock pinned to the bottom right of every page, so the pages stay about their subject.',
map=FIX + [['Overview', 'Speed, moved today, computers up, devices; throughput chart', 0], ['Transfers', 'Corner dock on every page; Transfers page for everything', 1], ['Requests', 'Waiting for you card; Devices badge', 1], ['Search', 'Top bar or Ctrl K', 1]],
css=r'''
.D{--bg:#111113;--sf:#18181b;--rz:#222226;--t1:#ececee;--t2:#a0a0a8;--t3:#80808a;--line:rgba(236,236,238,.08);--line2:rgba(236,236,238,.14);--ac:#8fb4ff;--on-ac:#111113;--ac-ink:#a6c3ff;--ac-soft:rgba(143,180,255,.12);--pos:#4cc38a;--neg:#e5484d;--warn:#e0a43b;--font:"Geist",system-ui,sans-serif;--mono:"Geist Mono",monospace;--shadow:0 12px 32px rgba(0,0,0,.45)}
.D .k-btn.pri{background:#ececee;color:#111113;box-shadow:none}.D .k-qr{background:#ececee}
.D .top{height:56px;flex:none;display:flex;align-items:center;gap:12px;padding:0 24px;border-bottom:1px solid var(--line)}.D .top .k-sbox{width:380px;margin-right:auto}
.D .cd{position:absolute;right:16px;bottom:16px;width:380px;background:var(--sf);border-radius:12px;box-shadow:var(--shadow),inset 0 0 0 1px var(--line2);padding:12px 16px;z-index:30}
.D .cd h4{display:flex;align-items:center;gap:8px;margin:0 0 8px;font:500 13px var(--font)}.D .cd h4 small{color:var(--t3);font:12px var(--mono)}
.D .cd .x{display:grid;grid-template-columns:1fr auto;gap:2px 8px;font-size:12px;margin-bottom:8px}.D .cd .x small{color:var(--t3)}.D .cd .x .k-bar{grid-column:1/-1;margin:0}
.D .db{display:grid;grid-template-columns:repeat(12,1fr);gap:16px}
.D .stat{grid-column:span 3;background:var(--sf);border-radius:12px;box-shadow:inset 0 0 0 1px var(--line);padding:16px 20px}.D .stat small{color:var(--t3);font-size:12px}.D .stat b{display:block;font:500 28px/1.2 var(--mono);margin:8px 0 4px}.D .stat span{font-size:12px;color:var(--t2)}
.D .chart{grid-column:span 8;background:var(--sf);border-radius:12px;box-shadow:inset 0 0 0 1px var(--line);padding:16px 20px;position:relative}
.D .chart h2,.D .card h2{font:500 14px var(--font);margin:0;display:flex;align-items:center;gap:12px}.D .chart h2 small{color:var(--t3);font-weight:400}
.D .chart>svg{display:block;width:100%;height:232px;margin-top:12px}.D .chart>svg text{font:11px var(--mono);fill:var(--t3)}
.D .chart .ov{position:absolute;inset:48px 20px 20px;display:grid;place-items:center;color:var(--t3);font-size:13px}
.D .card{grid-column:span 4;background:var(--sf);border-radius:12px;box-shadow:inset 0 0 0 1px var(--line);padding:16px 20px;display:flex;flex-direction:column;gap:12px}.D .card.w{grid-column:span 6}
.D .need{display:grid;grid-template-columns:20px 1fr auto;gap:12px;align-items:center;font-size:13px}.D .need small{display:block;color:var(--t3);font-size:12px}
.D .pcr{display:grid;grid-template-columns:12px 1fr 120px 64px;gap:12px;align-items:center;font-size:13px;height:36px}.D .pcr small{color:var(--t3);font:12px var(--mono)}.D .pcr .k-bar{margin:0}.D .pcr span .k-e{margin-right:6px}
.D .rc{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.D .rc div{font-size:12px;color:var(--t2)}.D .rc svg{border-radius:8px;display:block;width:100%;height:auto;margin-bottom:6px}.D .rc b{display:block;color:var(--t1);font-weight:500;font-size:13px}
.D .nil{color:var(--t3);font-size:13px;display:grid;gap:12px;justify-items:start}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.searchBox('Search all connected computers')+K.C.actions();
const cd=document.getElementById('pop');cd.className='cd';
cd.innerHTML=EM?`<h4>${K.E('updown_arrow',16)}Transfers<small>nothing moving</small></h4><small class="mut">Drag files onto a folder in Files to upload them.</small>`:`<h4>${K.C.ring(64)}Transfers<small>52.4 MB/s · 3 need you</small><span class="sp"></span>${K.ib('pause','Pause all transfers')}${K.ib('right','Open Transfers','go:transfers')}</h4><div class="x"><span>walkthrough-full.mov</span><small>71% · 23 s</small>${K.bar(71)}</div><div class="x"><span>tax-2025.pdf</span><small>checking</small>${K.bar(100)}</div><div class="x"><span class="warn">bed-layout.pdf: name exists</span>${K.btn('Keep both',{k:'sm'})}</div>`;
const up=[2,3,2,6,14,22,18,30,41,38,47,52,49,53,52,51],dn=[1,1,4,9,6,4,12,8,6,11,9,7,14,10,6,8];
const X=i=>40+i*(800/15),Y=v=>200-v*3,line=a=>a.map((v,i)=>`${i?'L':'M'}${X(i).toFixed(1)} ${Y(v)}`).join(' ');
const grid=`${[0,20,40,60].map(v=>`<path d="M40 ${Y(v)}H840" stroke="var(--line)"/><text x="0" y="${Y(v)+4}">${v}</text>`).join('')}${['08:00','09:00','10:00','11:00'].map((t,i)=>`<text x="${40+i*266-16}" y="226">${t}</text>`).join('')}`;
const stats=EM?[['Moving now','—','nothing to move yet'],['Moved today','0 B','since RFE started'],['Computers up','0','add one to start'],['Devices','0','pair a phone after']]:[['Moving now','52.4 MB/s','2 transfers · 23 s left'],['Moved today','1.9 GB','+640 MB vs yesterday'],['Computers up','2 of 4','nas key changed · media-mini off'],['Devices','4 allowed','2 asking to pair']];
const home=()=>`<div class="db">${stats.map(s=>`<div class="stat"><small>${s[0]}</small><b>${s[1]}</b><span>${s[2]}</span></div>`).join('')}
<div class="chart"><h2>Throughput today <small>MB/s · upload and download</small></h2><svg viewBox="0 0 860 232">${grid}${EM?'':`<path d="${line(dn)}" fill="none" stroke="var(--t3)" stroke-width="1.5"/><path d="${line(up)}" fill="none" stroke="var(--ac)" stroke-width="2"/>`}</svg>${EM?'<div class="ov">The line starts with your first transfer</div>':''}</div>
<div class="card"><h2>Waiting for you ${EM?'':'<em class="k-badge bad">4</em>'}</h2>${EM?`<div class="nil">Nothing waits for you.<small>Pairing requests, name conflicts and key changes come here.</small></div>`:[['phone','Pixel 9 asks to join studio-pc','Match 4829 1307 · 1:42 left',K.btn('Accept…',{k:'sm pri',a:'dlg:accept'})],['laptop','sam-thinkpad wants work-laptop’s place','Match 0316 8842',K.btn('Review…',{k:'sm',a:'dlg:accept'})],['doc','bed-layout.pdf exists on work-laptop','Upload to /Users/sam/Documents',K.btn('Keep both',{k:'sm'})],['warning','notes.txt was damaged while uploading','Discarded; try again',K.btn('Retry',{k:'sm',i:'refresh'})]].map(n=>`<div class="need">${E(n[0],20)}<div>${n[1]}<small>${n[2]}</small></div>${n[3]}</div>`).join('')}</div>
<div class="card w"><h2>Computers</h2>${EM?`<div class="nil">No computers yet.${K.btn('Add a computer',{i:'plus',k:'sm pri',a:'dlg:newconn'})}</div>`:D.servers.map(s=>`<div class="pcr">${K.dot(s.dot)}<span>${K.mach(s,16)}${s.name} <small>${s.tag}</small></span>${K.bar(Math.round(s.used/s.total*100))}<small>${Math.round(s.used/s.total*100)}% full</small></div>`).join('')}</div>
<div class="card w"><h2>Recent files</h2>${EM?`<div class="nil">Files you open or move show here.</div>`:`<div class="rc">${[[2,'north-bed-after.jpg','studio-pc · 12 min ago'],[1,'north-bed-before.jpg','downloaded'],[3,'garden-sketch.png','this computer']].map(r=>`<div data-k="dlg:preview" style="cursor:pointer">${K.photo(r[0],180,100,8)}<b>${r[1]}</b>${r[2]}</div>`).join('')}</div>`}</div></div>`;
B.mount({views:{home}});
'''),
# ------------------------------------------------------------------ 03
dict(n=3, slug='daybook', title='Daybook — the Overview is today’s log, a Now rail on the right',
fonts='family=Newsreader:opsz,wght@6..72,400;6..72,500&family=Instrument+Sans:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='The Overview reads like a diary of today: each event with its time, and open ones carry their buttons. A Now rail on the right of every page except Files holds search, what is moving and who is asking.',
map=FIX + [['Overview', 'Today’s log; open entries carry their actions', 0], ['Transfers', 'Now rail on every page; Transfers page', 0], ['Requests', 'Now rail and the log', 0], ['Search', 'Now rail field or Ctrl K', 1]],
css=r'''
.D{--bg:#f8f5ef;--sf:#fdfbf7;--rz:#efe9df;--t1:#1f1b16;--t2:#5e554a;--t3:#7b7266;--line:rgba(31,27,22,.08);--line2:rgba(31,27,22,.15);--ac:#8a4b1f;--ac-ink:#7a3f17;--ac-soft:rgba(138,75,31,.09);--font:"Instrument Sans",system-ui,sans-serif;--display:"Newsreader",Georgia,serif;--mono:"JetBrains Mono",monospace}
.D .rail{width:300px;flex:none;border-left:1px solid var(--line);padding:16px 20px;overflow:auto;background:var(--sf)}
.D .body:has(.ol) .rail{display:none}
.D .rail .r{display:flex;gap:4px;align-items:center;margin-bottom:16px}.D .rail .k-sbox{width:100%;margin-bottom:12px}
.D .rail h3{font:500 18px var(--display);margin:20px 0 10px;display:flex;align-items:center;gap:8px}
.D .rail .x{display:grid;grid-template-columns:1fr auto;gap:2px 8px;font-size:13px;margin-bottom:12px}.D .rail .x small{color:var(--t3);font:11px var(--mono)}.D .rail .x .k-bar{grid-column:1/-1;margin:0}
.D .rail .rq{background:var(--rz);border-radius:10px;padding:12px;font-size:13px;display:grid;gap:6px;margin-bottom:8px}.D .rail .rq b{font:500 18px var(--mono);letter-spacing:.04em}
.D .day{font:400 34px var(--display);margin:0}.D .sub{color:var(--t3);margin:4px 0 20px}
.D .e{display:grid;grid-template-columns:56px 1fr;gap:16px;padding:14px 0;border-top:1px solid var(--line)}
.D .e time{font:12px var(--mono);color:var(--t3);padding-top:2px}.D .e>div{position:relative;padding-left:30px}.D .e .ek{position:absolute;left:0;top:1px}
.D .e p{margin:0}.D .e p small{display:block;color:var(--t3);font-size:12px}
.D .e .acts{display:flex;gap:8px;margin-top:10px;align-items:center}
.D .e.open{background:var(--sf);margin:0 -16px;padding:14px 16px;border-radius:12px;box-shadow:inset 0 0 0 1px var(--line);border-top-color:transparent}.D .e.open+.e{border-top-color:transparent}
.D .e .code{font:500 20px var(--mono);letter-spacing:.04em;margin-right:auto}
''',
js=r'''
const ent=(t,em,body,cls='')=>`<div class="e ${cls}"><time>${t}</time><div><span class="ek">${E(em,18)}</span>${body}</div></div>`;
const rail=()=>`<div class="r">${K.C.actions()}</div>${K.C.searchBox('Search files')}`+(EM?`<h3>Now</h3><p class="mut" style="font-size:13px">Nothing is moving. Transfers and pairing requests show up here while they last.</p>`:`<h3>Now <span class="sp" style="flex:1"></span>${K.ib('pause','Pause all transfers')}</h3><div class="x"><span>walkthrough-full.mov</span><small>71%</small>${K.bar(71)}<small>to studio-pc · 52.4 MB/s</small></div><div class="x"><span>tax-2025.pdf</span><small>checking</small>${K.bar(100)}</div><div class="x"><span>seed-order.xlsx</span><small>queued</small></div><h3>${E('phone',18)}Asking</h3><div class="rq">Pixel 9 · studio-pc<b>4829 1307</b><div class="k-row">${K.btn('Reject',{k:'sm ghost'})}${K.btn('Accept…',{k:'sm pri',a:'dlg:accept'})}</div></div><div class="rq">sam-thinkpad · work-laptop<b>0316 8842</b><div class="k-row">${K.btn('Review…',{k:'sm',a:'dlg:accept'})}</div></div>`);
const home=()=>EM?`<h1 class="day">Sunday, 1 November</h1><p class="sub">1 entry</p>${ent('10:40','sunrise',`<p>RFE Desktop started for the first time<small>Version 1.1.0 · nothing added yet</small></p><div class="acts">${K.btn('Add a computer',{i:'plus',k:'pri sm',a:'dlg:newconn'})}${K.btn('Find on network',{k:'sm',a:'dlg:newconn:Find on network'})}</div>`,'open')}<p class="mut" style="margin-top:24px">Each connection, transfer and request becomes an entry here, newest first.</p>`
:`<h1 class="day">Sunday, 1 November</h1><p class="sub">8 entries · 3 still open</p>
${ent('10:46','phone',`<p>Pixel 9 asks to pair with studio-pc<small>from 192.168.1.40 · accept only if the phone shows the same code · 1:42 left</small></p><div class="acts"><span class="code">4829 1307</span>${K.btn('Reject',{k:'sm ghost'})}${K.btn('Accept…',{k:'pri sm',a:'dlg:accept'})}</div>`,'open')}
${ent('10:44','outbox_tray',`<p>Uploading walkthrough-full.mov to studio-pc<small>4.1 GB · 71% · 23 s left</small></p>`)}
${ent('10:41','inbox_tray',`<p>Downloaded north-bed-before.jpg, checksum verified<small>~/Downloads/RFE Desktop · 4.1 MB</small></p><div class="acts">${K.btn('Show in folder',{k:'sm',i:'folder'})}${K.btn('Open',{k:'sm ghost',a:'dlg:preview'})}</div>`)}
${ent('10:02','warning',`<p>bed-layout.pdf already exists on work-laptop<small>the upload waits for your answer</small></p><div class="acts">${K.btn('Replace',{k:'sm'})}${K.btn('Keep both',{k:'pri sm'})}${K.btn('Skip',{k:'sm ghost'})}</div>`,'open')}
${ent('09:01','locked',`<p>Signed in to studio-pc as sam<small>account · this computer, desk-linux</small></p>`)}
${ent('08:58','key',`<p>nas presented a different key than the one you trusted<small>pinned 41DE 0B93 · RFE will not connect until you compare</small></p><div class="acts">${K.btn('Compare keys…',{k:'sm',a:'dlg:keychanged'})}</div>`,'open')}
${ent('08:14','antenna_bars',`<p>media-mini stopped answering; album-2019.zip paused at 33%<small>it continues when the computer is back</small></p>`)}`;
B.mount({views:{home},onGo:()=>{document.getElementById('rail').innerHTML=rail()}});
'''),
# ------------------------------------------------------------------ 04
dict(n=4, slug='inspector', title='Inspector — every page is a list with an inspector on the right',
fonts='family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500',
idea='Every page except Files is a list, and the inspector on the right shows the selected row in full with its actions. One place to look for details and one place for buttons; rows themselves carry none.',
map=FIX + [['Servers', 'Servers list; inspector holds status, key, actions', 1], ['Devices', 'Devices list; inspector holds permissions', 1], ['Transfers', 'Transfers list; inspector holds the selected transfer', 1], ['Search', 'Top bar or Ctrl K', 1]],
css=r'''
.D{--bg:#fbfcfd;--sf:#fff;--rz:#eef1f5;--t1:#141a24;--t2:#4a5568;--t3:#6b7686;--line:rgba(20,26,36,.08);--line2:rgba(20,26,36,.14);--ac:#0e7490;--ac-ink:#0e6680;--ac-soft:rgba(14,116,144,.08);--font:"IBM Plex Sans",system-ui,sans-serif;--mono:"IBM Plex Mono",monospace}
.D .top{height:52px;flex:none;display:flex;align-items:center;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}.D .top .k-sbox{width:340px;margin-right:auto}
.D .rail{width:340px;flex:none;border-left:1px solid var(--line);padding:20px;overflow:auto;background:var(--sf)}
.D .body:has(.ol) .rail{display:none}
.D .rail h2{font:500 18px var(--font);margin:8px 0 4px;display:flex;gap:10px;align-items:center}.D .rail .k-sub{margin-bottom:16px}
.D .rail .k-dact{margin-top:8px}
.D .li{display:grid;gap:2px}.D .ro{display:grid;grid-template-columns:28px 1fr auto;gap:12px;align-items:center;min-height:52px;padding:6px 12px;border-radius:8px;cursor:pointer}
.D .ro:hover{background:var(--rz)}.D .ro.sel{background:var(--ac-soft);box-shadow:inset 2px 0 0 var(--ac)}.D .ro b{font-weight:500}.D .ro small{display:block;color:var(--t3);font-size:12px}.D .ro .st{font:12px var(--mono);color:var(--t2);display:flex;gap:6px;align-items:center}
.D .rail .none{color:var(--t3);font-size:13px}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.searchBox('Search all connected computers')+K.C.actions();
let pick={servers:'studio',devices:'Pixel 8',transfers:'walkthrough-full.mov',home:'studio'};
const rail=document.getElementById('rail');
const insp={
 servers:()=>{const s=K.S(pick.servers);return `${K.mach(s,40)}<h2>${s.name}</h2><p class="k-sub">${K.dot(s.dot)} ${s.state} · ${s.route}</p><dl class="k-kv"><dt>Address</dt><dd class="mono">${s.addr}</dd><dt>System</dt><dd>${s.sys}</dd><dt>Agent</dt><dd>${s.ver}</dd><dt>Key</dt><dd>${K.tag(s.id)}</dd><dt>Disk</dt><dd>${s.used} GB of ${s.total} GB${K.bar(s.used/s.total*100)}</dd></dl><div class="k-dact">${s.state==='Connected'?K.btn('Open files',{i:'folder',k:'pri',a:'go:files'}):s.state==='Key changed'?K.btn('Review key…',{k:'warn',a:'dlg:keychanged'}):K.btn('Wake computer',{i:'power'})}${K.btn('Status…',{i:'activity',a:'dlg:status'})}${K.btn('Run diagnostics',{i:'radar',a:'dlg:diag'})}${K.btn('Allowed roots…',{i:'drive',a:'dlg:roots'})}${K.btn('Forget…',{k:'ghost',a:'dlg:forget'})}</div>`;},
 devices:()=>{const d=D.devices.find(x=>x.n===pick.devices);return `${E(d.kind,40)}<h2>${d.n}</h2><p class="k-sub">${d.how} · seen ${d.seen}</p><div class="k-perms">${[['Browse',1],['Download',1],['Upload',1],['Change',0],['Delete',0],['Share links',1],['See apps',0],['Start apps',0],['Read-only',0]].map(x=>`<div class="k-set"><div><b>${x[0]}</b></div>${K.sw(x[1])}</div>`).join('')}</div><dl class="k-kv" style="margin-top:12px"><dt>Folder limit</dt><dd class="mono">${d.lim||'none'}</dd></dl><div class="k-dact">${K.btn('Activity',{a:'go:history'})}${K.btn('Revoke access…',{k:'warn',a:'dlg:revoke'})}</div>`;},
 transfers:()=>`${E('outbox_tray',40)}<h2>walkthrough-full.mov</h2><p class="k-sub">Upload to studio-pc · /home/sam/Projects/garden-plan</p>${K.bar(71)}<dl class="k-kv" style="margin-top:12px"><dt>Done</dt><dd>2.9 of 4.1 GB · 71%</dd><dt>Speed</dt><dd>52.4 MB/s · 23 s left</dd><dt>Checks</dt><dd>Each 4 MiB piece and the whole file, by SHA-256</dd></dl><div class="k-dact">${K.btn('Pause',{i:'pause'})}${K.btn('Cancel',{k:'ghost'})}</div>`,
};
const ro=(page,id,icon,t,s,st)=>`<div class="ro${pick[page]===id?' sel':''}" data-k="pick:${page}:${id}">${icon}<span><b>${t}</b><small>${s}</small></span><span class="st">${st}</span></div>`;
const L={
 servers:()=>K.head('Servers',`${D.servers.length} saved`,K.btn('New connection',{i:'plug',k:'pri',a:'dlg:newconn'}))+`<div class="li">${D.servers.map(s=>ro('servers',s.id,K.mach(s,24),s.name,s.addr+' · '+s.route,K.dot(s.dot)+s.state)).join('')}</div>`,
 devices:()=>K.head('Devices','Allowed on studio-pc',K.btn('Pair a phone',{i:'qr',a:'dlg:pairphone'}))+K.sec('Asking to join <span class="k-count">2</span>','',K.V.requests())+`<div class="li">${D.devices.map(d=>ro('devices',d.n,E(d.kind,24),d.n,d.how+' · '+d.seen,d.revoked?'<span class="bad">Revoked</span>':d.lim||'every folder')).join('')}</div>`,
 transfers:()=>K.head('Transfers','52.4 MB/s · 2 moving · 3 need you',K.btn('Pause all',{i:'pause',k:'sm'}))+`<div class="li">${[...D.xfer.attention,...D.xfer.active,...D.xfer.queued,...D.xfer.done].map(t=>ro('transfers',t.n,E(t.d==='up'?'outbox_tray':'inbox_tray',24),t.n,t.where,t.st?`<span class="warn">${t.st}</span>`:t.pct+'%')).join('')}</div>`,
 home:()=>K.head('studio-pc','The computer you use most · '+K.tag('studio'))+`<div class="k-stats">${[['52.4 MB/s','moving now'],['3','need you'],['812 GB','of 2 TB used'],['4','devices allowed']].map(s=>`<div><b class="mono">${s[0]}</b><small>${s[1]}</small></div>`).join('')}</div>`+K.sec('Other computers','',`<div class="li">${D.servers.slice(1).map(s=>ro('home',s.id,K.mach(s,24),s.name,s.addr,K.dot(s.dot)+s.state)).join('')}</div>`),
};
const draw=v=>{rail.innerHTML=EM?`<h2>Nothing selected</h2><p class="none">Pick a row to see it here with everything you can do to it.</p><ol class="b-steps"><li>Install RFE on a PC and run <code>rfe setup</code>.</li><li><b>Add a computer</b>.</li><li>Its details and actions appear here.</li></ol>`:(insp[v]||insp.servers)();};
const views=EM?{home:()=>K.head('Overview')+B.W('desktop_computer','No computers yet','Add one, and this page shows it at a glance with the others below.',B.addActs)}:{home:L.home,servers:L.servers,devices:L.devices,transfers:L.transfers};
B.mount({views,on:(a,b,t)=>{if(a==='pick'){const p=t.dataset.k.split(':');pick[p[1]]=p.slice(2).join(':');K.render();}},onGo:v=>draw(v==='home'?'servers':v)});
'''),
# ------------------------------------------------------------------ 05
dict(n=5, slug='bento', title='Bento — the Overview as tiles, one per thing you own',
fonts='family=Manrope:wght@400;500;600&family=Space+Mono:wght@400;700',
idea='The Overview is a grid of tiles, one per subject: speed, each computer’s disk, phones asking, recent photos, links, sync, trash. Each tile opens its page. Servers are big tiles with a disk ring.',
map=FIX + [['Overview', 'Tiles; each opens its page', 0], ['Servers', 'Big tiles with a disk ring; menu on each', 1], ['Transfers', 'Speed tile; Transfers page', 1], ['Search', 'Top bar or Ctrl K', 1]],
css=r'''
.D{--bg:#f3f1ee;--sf:#fffdfa;--rz:#ebe7e1;--t1:#1d1b18;--t2:#57524b;--t3:#7a746b;--line:rgba(29,27,24,.07);--line2:rgba(29,27,24,.14);--ac:#c2410c;--ac-ink:#b23a09;--ac-soft:rgba(194,65,12,.08);--font:"Manrope",system-ui,sans-serif;--mono:"Space Mono",monospace;--rc:18px}
.D .top{height:60px;flex:none;display:flex;align-items:center;gap:8px;padding:0 24px}.D .top .k-sbox{width:360px;margin-right:auto;border-radius:18px}
.D .bt{display:grid;grid-template-columns:repeat(6,1fr);grid-auto-rows:150px;gap:14px}
.D .t{background:var(--sf);border-radius:18px;box-shadow:inset 0 0 0 1px var(--line);padding:18px;display:flex;flex-direction:column;gap:6px;cursor:pointer;overflow:hidden;position:relative;transition:box-shadow .15s}
.D .t:hover{box-shadow:inset 0 0 0 1px var(--line2),0 6px 18px rgba(0,0,0,.05)}
.D .t h3{margin:0;font:500 13px var(--font);color:var(--t2);display:flex;gap:8px;align-items:center}.D .t b{font:700 30px var(--mono);letter-spacing:-.02em}.D .t small{color:var(--t3)}
.D .w2{grid-column:span 2}.D .w3{grid-column:span 3}.D .h2{grid-row:span 2}
.D .t.ghost{background:none;box-shadow:inset 0 0 0 1.5px var(--line2);border:0;color:var(--t3)}.D .t.ghost h3{color:var(--t3)}
.D .t.add{background:var(--ac);color:#fff;box-shadow:none}.D .t.add h3,.D .t.add small{color:rgba(255,255,255,.85)}.D .t.add b{font-family:var(--font);font-size:24px}
.D .rqt{display:grid;grid-template-columns:1fr auto;gap:6px 12px;align-items:center;padding:10px 0;border-top:1px solid var(--line);font-size:13px}.D .rqt small{display:block;color:var(--t3);font-size:12px}.D .rqt code{background:none;font:700 15px var(--mono);letter-spacing:.04em}.D .rqt .k-btn{grid-column:1/-1;justify-self:start}
.D .ring{display:flex;gap:16px;align-items:center}.D .ring svg{flex:none}
.D .ph3{display:flex;gap:8px;margin-top:auto}.D .ph3 svg{border-radius:10px}
.D .tiles{display:grid;grid-template-columns:repeat(2,1fr);gap:14px}.D .st{background:var(--sf);border-radius:18px;box-shadow:inset 0 0 0 1px var(--line);padding:20px;display:grid;grid-template-columns:auto 1fr auto;gap:16px;align-items:center}
.D .st.bad{box-shadow:inset 0 0 0 1.5px color-mix(in srgb,var(--neg) 45%,transparent)}.D .st b{font-weight:600;font-size:16px}.D .st small{display:block;color:var(--t3);font-size:12px}.D .st .k-row{grid-column:1/-1}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.searchBox('Search all connected computers')+K.C.actions();
const ring=(p,c='var(--ac)',sz=72)=>`<svg width="${sz}" height="${sz}" viewBox="0 0 36 36"><circle cx="18" cy="18" r="15" fill="none" stroke="var(--rz)" stroke-width="4"/><circle cx="18" cy="18" r="15" fill="none" stroke="${c}" stroke-width="4" stroke-dasharray="${p*0.94} 100" transform="rotate(-90 18 18)" stroke-linecap="round"/><text x="18" y="21" text-anchor="middle" style="font:700 8px var(--mono);fill:var(--t1)">${p}%</text></svg>`;
const t=(cls,a,h,body)=>`<div class="t ${cls}" data-k="${a}"><h3>${h}</h3>${body}</div>`;
const home=()=>EM?`<div class="bt">${t('add w2 h2','dlg:newconn',`${I('plus',16)}Start here`,`<b>Add a computer</b><small>Install RFE on a PC, run rfe setup there, then add it by address, network or pairing code.</small>`)}${[['updown_arrow','Speed','w2','The speed of your transfers'],['desktop_computer','Disk','w2','Each computer’s disk use'],['mobile_phone','Phones','w2','Phones asking to pair'],['framed_picture','Recent','w2','Photos and files you opened'],['link','Links','','Live share links'],['counterclockwise_arrows_button','Sync','','Folder sync rules']].map(x=>t('ghost '+x[2],'dlg:newconn',`${E(x[0],18)}${x[1]}`,`<small>${x[3]} appear here after you add a computer.</small>`)).join('')}</div>`
:`<div class="bt">${t('w2','go:transfers',`${E('updown_arrow',18)}Moving now`,`<b>52.4 MB/s</b><small>2 transfers · 23 s left · 3 need you</small>${K.bar(71)}`)}${t('w2','go:servers',`${K.mach(D.servers[0],18)}studio-pc`,`<div class="ring">${ring(41)}<small>812 GB of 2 TB<br>12 ms LAN</small></div>`)}${t('w2 h2','go:devices',`${E('phone',18)}Asking to pair`,`<b>2</b>${D.requests.map(r=>`<div class="rqt"><span>${r.n}<small>${r.exp} left${r.over?' · takes over '+r.over:''}</small></span><code>${r.code}</code>${K.btn('Accept…',{k:'sm pri',a:'dlg:accept'})}</div>`).join('')}`)}${t('w2','go:servers',`${K.mach(D.servers[1],18)}work-laptop`,`<div class="ring">${ring(79,'var(--warn)')}<small>402 of 512 GB<br>48 ms Tailscale</small></div>`)}${t('w2','go:servers',`${E('key',18)}nas`,`<b style="font-size:20px;color:var(--neg)">Key changed</b><small>Compare before its files open</small>`)}${t('w3','go:files',`${E('framed_picture',18)}Recent`,`<div class="ph3">${[2,1,3,0].map(s=>K.photo(s,120,70,10)).join('')}</div>`)}${t('','tool:share',`${E('link',18)}Links`,`<b>2</b><small>live</small>`)}${t('','tool:sync',`${E('counterclockwise_arrows_button',18)}Sync`,`<b>2</b><small>rules</small>`)}${t('','tool:trash',`${E('wastebasket',18)}Trash`,`<b>4</b><small>items</small>`)}</div>`;
const servers=()=>K.head('Servers','4 saved · 2 connected',K.btn('New connection',{i:'plug',k:'pri',a:'dlg:newconn'}))+`<div class="tiles">${D.servers.map(s=>{const p=Math.round(s.used/s.total*100);return `<div class="st${s.dot==='bad'?' bad':''}">${ring(p,s.dot==='ok'?'var(--ac)':'var(--t3)',64)}<span><b>${K.mach(s,18)} ${s.name}</b><small>${K.dot(s.dot)} ${s.state} · ${s.route} · ${s.sys}</small><small class="mono">${s.addr}</small></span>${K.ib('more','More for '+s.name,'menu:server')}<div class="k-row">${s.state==='Connected'?K.btn('Open files',{i:'folder',k:'sm pri',a:'go:files'}):s.state==='Key changed'?K.btn('Review key…',{k:'sm warn',a:'dlg:keychanged'}):K.btn('Wake computer',{i:'power',k:'sm'})}</div></div>`}).join('')}</div>`;
B.mount({views:EM?{home}:{home,servers}});
'''),
]
