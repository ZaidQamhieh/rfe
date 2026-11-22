FIX = [['Navigation', 'Left sidebar (fixed): Overview, the pages, Computers, Add a computer', 1],
       ['Files', 'Files page (fixed): one tree across every computer, actions on the row, preview on the right', 1],
       ['Settings', 'Settings page (fixed): sections on the left', 1]]

DESIGNS = [
# ------------------------------------------------------------------ 06
dict(n=6, slug='console', title='Console — a command line at the foot of every page',
fonts='family=JetBrains+Mono:wght@400;500;700&family=Inter:wght@400;500',
idea='No top bar. A command line sits at the foot of every page with a status line under it; typing filters files and suggests commands, Enter runs the first. The Overview is the session so far and what you can type.',
map=FIX + [['Commands', 'Command line at the foot; Ctrl K focuses it', 1], ['Transfers', 'Status line; Transfers page', 1], ['Requests', 'Status line; Devices page', 1], ['Notifications', 'Status line bell', 1]],
css=r'''
.D{--bg:#0d0f0c;--sf:#131611;--rz:#1c2019;--t1:#e8e6dc;--t2:#a9a796;--t3:#86846f;--line:rgba(232,230,220,.08);--line2:rgba(232,230,220,.15);--ac:#f0b429;--on-ac:#0d0f0c;--ac-ink:#f5c451;--ac-soft:rgba(240,180,41,.1);--pos:#7cc47f;--neg:#ef6b5b;--warn:#f0b429;--font:"Inter",system-ui,sans-serif;--mono:"JetBrains Mono",monospace;--shadow:0 16px 40px rgba(0,0,0,.5)}
.D .k-qr{background:#e8e6dc}
.D .bot{flex:none;border-top:1px solid var(--line2);background:var(--sf)}
.D .cl{display:flex;align-items:center;gap:10px;height:48px;padding:0 20px;font:14px var(--mono)}.D .cl .p{color:var(--ac)}.D .cl input{flex:1;background:none;border:0;outline:0;color:var(--t1);font:inherit}
.D .sl{display:flex;gap:16px;align-items:center;height:28px;padding:0 20px;font:12px var(--mono);color:var(--t3);border-top:1px solid var(--line)}.D .sl b{color:var(--t1);font-weight:500}.D .sl .m{background:var(--ac);color:var(--on-ac);padding:2px 8px;border-radius:3px;font-weight:700}
.D .sl button{color:inherit;font:inherit}.D .sl .warn{color:var(--warn)}
.D .sess{font:13px/1.9 var(--mono);color:var(--t2);margin:0 0 24px;white-space:pre-wrap}.D .sess b{color:var(--ac);font-weight:500}.D .sess i{color:var(--t1);font-style:normal}
.D h1.c{font:700 15px var(--mono);margin:0 0 12px;color:var(--t1)}
.D .cmds{display:grid;grid-template-columns:repeat(2,1fr);gap:4px 32px}.D .cmds button{display:grid;grid-template-columns:200px 1fr;gap:12px;height:32px;align-items:center;text-align:left;font:13px var(--mono);border-radius:4px;padding:0 8px}.D .cmds button:hover{background:var(--rz)}.D .cmds b{color:var(--ac);font-weight:500}.D .cmds span{color:var(--t3);font-family:var(--font)}
''',
js=r'''
document.getElementById('bot').innerHTML=`<label class="cl"><span class="p">rfe ›</span><input placeholder="${EM?'add 192.168.1.20   or   find   or   help':'type a file name, or: send, get, share, pair, accept, status'}" aria-label="Command line"><kbd>Ctrl K</kbd></label><div class="sl"><span class="m">RFE</span>${EM?'<span>no computers</span><span class="sp" style="flex:1"></span><button data-k="menu:account">sam</button>':`<button data-k="menu:browse">${K.dot('ok')} <b>studio-pc</b> 12 ms LAN</button><span>${K.tag('studio')}</span><button data-k="go:transfers">↑ 71% walkthrough-full.mov · 52.4 MB/s</button><button class="warn" data-k="go:transfers">3 need you</button><button class="warn" data-k="go:devices">Pixel 9 asks to pair</button><span class="sp" style="flex:1"></span><button data-k="menu:notes">3 notes</button><button data-k="menu:account">sam</button>`}</div>`;
const cmds=EM?[['add <address>','Add a computer by its address','dlg:newconn'],['find','Look for computers on this network','dlg:newconn:Find on network'],['code <code>','Add a computer with a pairing code','dlg:newconn:Pairing code'],['help','Every command and key','set:Help']]:[['send <file> <computer>','Upload, or copy between computers','dlg:upload'],['get <file>','Download to ~/Downloads/RFE Desktop','close'],['share <file> [1h]','Make a share link','dlg:share'],['pair','Code and QR for a phone','dlg:pairphone'],['accept <device>','Accept a pairing request','dlg:accept'],['status <computer>','Agent, disk, load','dlg:status'],['pause all','Pause every transfer','close'],['trash','Open Trash','tool:trash'],['add <address>','Add a computer','dlg:newconn'],['help','Every command and key','set:Help']];
const home=()=>(EM?`<pre class="sess"><b>10:40</b> RFE Desktop 1.1.0 started. No computers yet.\n<b>10:40</b> Set up a PC with <i>rfe setup</i>, then type <i>add</i> and its address below.</pre>`:`<pre class="sess"><b>10:46</b> <i>Pixel 9</i> asks to pair with studio-pc · match 4829 1307 · type <i>accept pixel</i>\n<b>10:44</b> send walkthrough-full.mov studio-pc · 71%\n<b>10:41</b> get north-bed-before.jpg · checksum verified\n<b>10:02</b> bed-layout.pdf exists on work-laptop · type <i>keep both</i>, <i>replace</i> or <i>skip</i>\n<b>08:58</b> nas presented a different key · type <i>compare nas</i></pre>`)+`<h1 class="c">${EM?'Start with':'You can type'}</h1><div class="cmds">${cmds.map(c=>`<button data-k="${c[2]}"><b>${K.esc(c[0])}</b><span>${c[1]}</span></button>`).join('')}</div>`;
B.mount({views:{home}});
'''),
# ------------------------------------------------------------------ 07
dict(n=7, slug='statusline', title='Status line — no top bar; one line at the foot holds the live state',
fonts='family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500',
idea='The window has no top bar. One status line at the foot shows the live state: the computer, its key, search, transfers, phones asking, notifications and the account. Each part opens a small panel. The Overview shows each computer as a lane.',
map=FIX + [['Search', 'Status line field, Ctrl K', 1], ['Transfers', 'Status line opens a panel; Transfers page', 1], ['Requests', 'Status line opens a panel with Accept', 1], ['Overview', 'A lane per computer: state, disk, what moves', 0]],
css=r'''
.D{--bg:#fff;--sf:#fafafa;--rz:#f0f0f1;--t1:#18181b;--t2:#52525b;--t3:#71717a;--line:rgba(24,24,27,.08);--line2:rgba(24,24,27,.14);--ac:#16a34a;--ac-ink:#15803d;--ac-soft:rgba(22,163,74,.08);--font:"Geist",system-ui,sans-serif;--mono:"Geist Mono",monospace}
.D .bot{height:40px;flex:none;display:flex;align-items:stretch;border-top:1px solid var(--line2);background:var(--sf);font-size:13px}
.D .seg{display:flex;align-items:center;gap:8px;padding:0 14px;border-right:1px solid var(--line);color:var(--t2);white-space:nowrap}.D .seg:hover{background:var(--rz);color:var(--t1)}.D .seg b{color:var(--t1);font-weight:500}.D .seg small{font:12px var(--mono);color:var(--t3)}
.D .seg.warn small{color:var(--neg)}.D .seg.fill{flex:1;min-width:0;border-right:1px solid var(--line)}.D .seg.fill .k-sbox{min-width:0}.D .seg.fill .k-sbox{width:100%;height:28px;background:none;box-shadow:none}
.D .pop{position:absolute;bottom:48px;width:420px;background:var(--sf);border-radius:12px;box-shadow:0 16px 40px rgba(0,0,0,.16),inset 0 0 0 1px var(--line2);padding:14px 16px;z-index:40;display:none;max-height:440px;overflow:auto}
.D .pop h4{display:flex;align-items:center;margin:0 0 10px;font:500 13px var(--font)}.D .pop h4 .k-btn{margin-left:auto}
.D .ln{display:grid;grid-template-columns:200px 1fr 160px;gap:20px;align-items:center;padding:16px 0;border-bottom:1px solid var(--line)}
.D .ln .nm{display:flex;gap:10px;align-items:center}.D .ln .nm b{font-weight:500}.D .ln .nm small{display:block;color:var(--t3);font:12px var(--mono)}
.D .ln .tk{display:flex;gap:6px;flex-wrap:wrap}.D .ln .tk span{font-size:12px;padding:4px 8px;border-radius:6px;background:var(--rz);color:var(--t2)}.D .ln .tk span.w{background:color-mix(in srgb,var(--warn) 14%,transparent);color:var(--warn)}
.D .ln.ghost{color:var(--t3)}.D h1.o{font:500 22px var(--font);margin:0 0 4px}.D .os{color:var(--t3);margin:0 0 12px}
''',
js=r'''
const pop=document.getElementById('pop');
document.getElementById('bot').innerHTML=EM?`<span class="seg">${E('laptop',16)}<b>This computer</b><small>no computers yet</small></span><button class="seg" data-k="dlg:newconn">${I('plus',14)}Add a computer</button><label class="seg fill">${K.C.searchBox('Search, once a computer is added')}</label><button class="seg" data-k="menu:notes" data-tip="Notifications">${I('bell',14)}</button><button class="seg" data-k="menu:account">sam</button>`
:`<button class="seg" data-k="menu:browse">${K.dot('ok')}<b>studio-pc</b><small>12 ms LAN</small>${I('chevd',12)}</button><button class="seg" data-k="dlg:pins" data-tip="Pinned key">${E('key',14)}<small>9F3A 1EC4</small></button><label class="seg fill">${K.C.searchBox('Search all connected computers')}</label><button class="seg" data-k="pop:x">${K.C.ring(71)}<b>2</b> moving <small>52.4 MB/s</small></button><button class="seg warn" data-k="pop:r">${E('phone',14)}<b>2</b> asking <small>1:42</small></button><button class="seg" data-k="menu:notes" data-tip="Notifications">${I('bell',14)}</button><button class="seg" data-k="menu:account">sam</button>`;
const lane=s=>`<div class="ln"><div class="nm">${K.mach(s,28)}<span><b>${K.dot(s.dot)} ${s.name}</b><small>${s.ms?s.ms+' ms '+s.route:s.state}</small></span></div><div class="tk">${s.id==='studio'?'<span>↑ walkthrough-full.mov 71%</span><span class="w">Pixel 9 asks to join</span>':s.id==='work'?'<span>↓ tax-2025.pdf checking</span><span class="w">bed-layout.pdf: name exists</span>':s.id==='nas'?'<span class="w">Key changed: compare before use</span>':'<span>album-2019.zip paused at 33%</span>'}</div><div>${K.bar(s.used/s.total*100)}<small class="mut mono">${s.used} of ${s.total} GB</small></div></div>`;
const home=()=>`<h1 class="o">Computers</h1><p class="os">${EM?'None yet. Each one you add gets a lane with its state, disk and what is moving.':'Each lane: state, what is happening on it, disk'}</p>`+(EM?`<div class="ln ghost"><div class="nm">${E('desktop_computer',28)}<span><b>Your first PC</b><small>after rfe setup</small></span></div><div class="tk"><span>transfers and requests show here</span></div><div>${K.btn('Add a computer',{i:'plus',k:'sm pri',a:'dlg:newconn'})}</div></div>`:D.servers.map(lane).join(''));
B.mount({views:{home},on:(a,b)=>{if(a==='pop'){const open=pop.style.display==='block'&&pop.dataset.w===b;pop.dataset.w=b;pop.style.display=open?'none':'block';pop.style.left=(b==='x'?560:740)+'px';pop.innerHTML=b==='x'?`<h4>Transfers${K.btn('Open Transfers',{k:'sm ghost',a:'go:transfers'})}</h4>`+K.V.xgroups(['attention','active']):`<h4>Pairing requests${K.btn('Pair a phone…',{k:'sm ghost',a:'dlg:pairphone'})}</h4>`+K.V.requests();}},onGo:()=>{pop.style.display='none'}});
'''),
# ------------------------------------------------------------------ 08
dict(n=8, slug='tabs', title='Tabs — pages and folders open as tabs, the Overview is a new tab',
fonts='family=Plus+Jakarta+Sans:wght@400;500;600&family=JetBrains+Mono:wght@400;500',
idea='Pages and folders open as tabs above the content, like a browser, with an address line under them. The Overview is the new-tab page: search, favorite folders, recent files and computers.',
map=FIX + [['Tabs', 'Above the content; + opens a new tab (the Overview)', 1], ['Address', 'The line under the tabs: type a path or a search', 1], ['Transfers', 'Ring at the end of the address line; Transfers page', 1], ['Favorites', 'New-tab page tiles', 0]],
css=r'''
.D{--bg:#f4f5f7;--sf:#fff;--rz:#eceef1;--t1:#111827;--t2:#4b5563;--t3:#6b7280;--line:rgba(17,24,39,.08);--line2:rgba(17,24,39,.14);--ac:#7c3aed;--ac-ink:#6d28d9;--ac-soft:rgba(124,58,237,.08);--font:"Plus Jakarta Sans",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{flex:none;background:var(--rz);border-bottom:1px solid var(--line)}
.D .tabs{display:flex;gap:2px;padding:8px 8px 0;align-items:flex-end}.D .tab{display:flex;align-items:center;gap:8px;height:34px;padding:0 10px 0 12px;border-radius:8px 8px 0 0;font-size:13px;color:var(--t2);max-width:220px}
.D .tab.on{background:var(--sf);color:var(--t1);box-shadow:0 -1px 0 var(--line)}.D .tab .x{opacity:.5;display:inline-flex}.D .tab:hover .x{opacity:1}
.D .omni{display:flex;align-items:center;gap:6px;padding:6px 10px;background:var(--sf)}
.D .addr{flex:1;display:flex;align-items:center;gap:8px;height:34px;padding:0 12px;border-radius:17px;background:var(--rz);font:13px var(--mono);color:var(--t2)}.D .addr b{color:var(--t1);font-weight:500}.D .addr .lk{display:inline-flex;gap:4px;align-items:center;color:var(--pos);font-family:var(--font);font-size:12px}
.D .nt{max-width:820px;margin:24px auto 0}.D .nt .k-in{height:52px;border-radius:26px;font-size:16px;padding:0 20px;margin-bottom:28px;background:var(--sf);box-shadow:var(--ctl)}
.D .nt h3{font:500 13px var(--font);color:var(--t3);margin:20px 0 10px}
.D .fv{display:grid;grid-template-columns:repeat(6,1fr);gap:12px}.D .fv button{display:grid;justify-items:center;gap:8px;padding:16px 8px;border-radius:14px;font-size:13px}.D .fv button:hover{background:var(--sf);box-shadow:var(--ctl)}.D .fv small{color:var(--t3);font-size:11px}
.D .fv .ic{width:52px;height:52px;border-radius:14px;background:var(--sf);display:grid;place-items:center;box-shadow:var(--ctl)}
''',
js=r'''
const TT=EM?[['home','sparkle','New tab']]:[['files','dir','garden-plan'],['files2','dir','Documents · work-laptop'],['devices','phone','Devices'],['home','x','New tab']];
function drawTop(v){const cur=v==='home'?'home':v;document.getElementById('top').innerHTML=`<div class="tabs">${TT.map(t=>`<button class="tab${(t[0]===cur||(t[0]==='files'&&v==='files'))?' on':''}" data-k="go:${t[0]==='files2'?'files':t[0]}">${t[1]==='x'||t[1]==='sparkle'?I('plus',14):E(t[1],16)}<span>${t[2]}</span><span class="x">${I('x',12)}</span></button>`).join('')}${K.ib('plus','New tab (Ctrl T)','go:home')}</div><div class="omni">${K.ib('left','Back (Alt+Left)')}${K.ib('up','Up one folder (Alt+Up)')}<div class="addr">${EM?'Type an address to add a computer, or a search':v==='files'?`<span class="lk">${I('lock',12)}pinned</span>rfe://studio-pc<b>/home/sam/Projects/garden-plan</b>`:`rfe://${v==='home'?'new-tab':v}`}</div>${EM?'':`<button class="k-xpill" data-k="go:transfers" style="height:32px" data-tip="Transfers">${K.C.ring(71)}2</button>`}${K.C.actions()}</div>`;}
const home=()=>`<div class="nt"><label class="k-in">${I('search',18)}<input placeholder="${EM?'Type an address like 192.168.1.20 to add a computer':'Search every connected computer, or type a path'}"></label>`+(EM?`<h3>Start</h3><div class="fv">${[['electric_plug','Add a computer','by address','dlg:newconn'],['satellite_antenna','Find on network','mDNS','dlg:newconn:Find on network'],['key','Pairing code','from rfe setup','dlg:newconn:Pairing code'],['information','How it works','the tour','dlg:welcome']].map(f=>`<button data-k="${f[3]}"><span class="ic">${E(f[0],28)}</span>${f[1]}<small>${f[2]}</small></button>`).join('')}</div>`:`<h3>Favorites</h3><div class="fv">${[...D.favs.map(f=>['file_folder',f[0],f[1],'go:files']),['wastebasket','Trash','4 items','tool:trash'],['link','Share links','2 live','tool:share'],['toolbox','Apps','studio-pc','tool:apps']].map(f=>`<button data-k="${f[3]}"><span class="ic">${E(f[0],28)}</span>${f[1]}<small>${f[2]}</small></button>`).join('')}</div><h3>Recent files</h3><div class="fv">${[[2,'north-bed-after.jpg'],[1,'north-bed-before.jpg'],[3,'garden-sketch.png']].map(r=>`<button data-k="dlg:preview">${K.photo(r[0],120,76,10)}${r[0]?r[1]:''}<small>${r[1]}</small></button>`).join('')}${[['memo','planting-calendar.md'],['page_facing_up','tax-2025.pdf'],['bar_chart','soil-test-results.csv']].map(r=>`<button data-k="go:files"><span class="ic">${E(r[0],28)}</span><small>${r[1]}</small></button>`).join('')}</div><h3>Computers</h3><div class="fv">${D.servers.map(s=>`<button data-k="${s.dot==='ok'?'go:files':'go:servers'}"><span class="ic">${K.mach(s,28)}</span>${K.dot(s.dot)} ${s.name}<small>${s.ms?s.ms+' ms':s.state}</small></button>`).join('')}</div>`)+`</div>`;
B.mount({views:{home},onGo:drawTop});
'''),
# ------------------------------------------------------------------ 09
dict(n=9, slug='trust', title='Trust — the Overview is who you trust and what each may do',
fonts='family=Sora:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500',
idea='Security first: the Overview shows each computer’s pinned key, then a grid of every device and what it may do, with requests to join on top. Devices is the same grid for one computer. A trust chip in the top bar says at once if any key changed.',
map=FIX + [['Keys', 'Overview, first row; key changed shows red with Compare', 0], ['Permissions', 'Overview grid; a cell opens Permissions', 1], ['Requests', 'Overview top; Devices page', 0], ['Search', 'Top bar or Ctrl K', 1]],
css=r'''
.D{--bg:#0b1020;--sf:#11172a;--rz:#1a2236;--t1:#e6e9f2;--t2:#a3abc2;--t3:#7f88a3;--line:rgba(230,233,242,.08);--line2:rgba(230,233,242,.15);--ac:#5eead4;--on-ac:#0b1020;--ac-ink:#5eead4;--ac-soft:rgba(94,234,212,.1);--pos:#5eead4;--neg:#fb7185;--warn:#fbbf24;--font:"Sora",system-ui,sans-serif;--mono:"IBM Plex Mono",monospace;--shadow:0 16px 40px rgba(0,0,0,.5)}
.D .k-qr{background:#e6e9f2}
.D .top{height:56px;flex:none;display:flex;align-items:center;gap:10px;padding:0 24px;border-bottom:1px solid var(--line)}.D .top .k-sbox{width:340px;margin-right:auto}
.D .chip{display:inline-flex;gap:8px;align-items:center;height:30px;padding:0 12px;border-radius:15px;font-size:12px;background:color-mix(in srgb,var(--neg) 16%,transparent);color:var(--neg)}.D .chip.ok{background:var(--ac-soft);color:var(--ac)}
.D .keys{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:24px}
.D .ky{background:var(--sf);border-radius:14px;box-shadow:inset 0 0 0 1px var(--line);padding:16px;display:grid;gap:8px;cursor:pointer}
.D .ky.bad{box-shadow:inset 0 0 0 1.5px color-mix(in srgb,var(--neg) 55%,transparent)}.D .ky b{font-weight:500;display:flex;gap:8px;align-items:center}.D .ky code{background:none;padding:0;font:500 14px var(--mono);letter-spacing:.06em;color:var(--ac)}.D .ky.bad code{color:var(--neg);text-decoration:line-through}.D .ky small{color:var(--t3);font-size:12px}
.D h2.s{font:500 15px var(--font);margin:24px 0 12px;display:flex;align-items:center;gap:10px}
.D table{width:100%;border-collapse:collapse;font-size:13px}.D th{font:500 12px var(--font);color:var(--t3);text-align:center;padding:8px;border-bottom:1px solid var(--line)}.D th:first-child{text-align:left}
.D td{padding:10px 8px;border-bottom:1px solid var(--line);text-align:center}.D td:first-child{text-align:left}.D td b{font-weight:500;display:flex;gap:8px;align-items:center}.D td small{color:var(--t3);display:block;font-size:11px;margin-left:28px}
.D .tg{display:inline-grid;place-items:center;width:24px;height:24px;border-radius:6px;background:var(--rz);color:transparent;cursor:pointer}.D .tg.on{background:var(--ac-soft);color:var(--ac)}
.D tr.rv{opacity:.45}
.D .ex{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-top:20px}.D .ex div{background:var(--sf);border-radius:14px;box-shadow:inset 0 0 0 1px var(--line);padding:18px;display:grid;gap:6px;align-content:start}.D .ex b{font-weight:500}.D .ex small{color:var(--t2);font-size:13px}
''',
js=r'''
document.getElementById('top').innerHTML=K.C.searchBox('Search all connected computers')+(EM?`<span class="chip ok">${E('shield',14)}Nothing trusted yet</span>`:`<button class="chip" data-k="dlg:keychanged">${E('warning',14)}1 key changed</button>`)+K.C.actions();
const G=['Browse','Download','Upload','Change','Delete','Share','Apps'];
const has={'desk-linux':[1,1,1,1,1,1,1],'Pixel 8':[1,1,1,0,0,1,0],'Galaxy Tab S9':[1,0,0,0,0,0,1],'work-laptop':[1,1,1,1,0,0,0],'old-phone':[0,0,0,0,0,0,0]};
const grid=()=>`<table><tr><th>Device on studio-pc</th>${G.map(g=>`<th>${g}</th>`).join('')}<th>Limited to</th><th></th></tr>${D.devices.map(d=>`<tr class="${d.revoked?'rv':''}"><td><b>${E(d.kind,20)}${d.n}${d.me?' · this computer':''}</b><small>${d.how} · seen ${d.seen}</small></td>${has[d.n].map(x=>`<td><span class="tg${x?' on':''}" data-k="dlg:perms">${I('check',14)}</span></td>`).join('')}<td><small class="mono" style="margin:0">${d.lim||'everywhere'}</small></td><td>${K.ib('more','Revoke, remove, activity','menu:device')}</td></tr>`).join('')}</table>`;
const home=()=>EM?`${K.head('Trust','Who you trust, and what each may do')}<div class="b-empty left" style="margin:0">${E('key',40)}<b>Nothing trusted yet</b><p>RFE trusts a PC once, by the key it shows the first time; after that a different key is refused until you compare. Phones join only when you accept a matching code.</p><div class="k-row">${B.addActs}</div></div><div class="ex">${[['locked_with_key','A key per PC','Compare it with rfe status on the PC, once.'],['mobile_phone','You approve phones','Both screens show the same code before anything joins.'],['shield','Per-device rights','Browse, download, upload, change, delete, share, apps.']].map(x=>`<div>${E(x[0],28)}<b>${x[1]}</b><small>${x[2]}</small></div>`).join('')}</div>`
:`${K.head('Trust','4 keys pinned · 1 changed · 2 devices asking')}<div class="keys">${D.servers.map(s=>`<div class="ky${s.dot==='bad'?' bad':''}" data-k="${s.dot==='bad'?'dlg:keychanged':'dlg:pins'}"><b>${K.mach(s,20)}${s.name}</b><code>${s.tag}</code><small>${s.dot==='bad'?'Changed: compare before use':s.login+' · '+(s.ms?s.ms+' ms':s.state.toLowerCase())}</small></div>`).join('')}</div><h2 class="s">${E('phone',18)}Asking to join <span class="k-count">2</span></h2>${K.V.requests()}<h2 class="s">${E('shield',18)}Who may do what</h2>${grid()}`;
const devices=()=>K.head('Devices','On studio-pc · a tick opens Permissions',K.btn('Pair a phone',{i:'qr',k:'pri',a:'dlg:pairphone'}))+K.V.requests()+'<div style="height:20px"></div>'+grid();
B.mount({views:EM?{home}:{home,devices}});
'''),
# ------------------------------------------------------------------ 10
dict(n=10, slug='mover', title='Mover — the Overview is for moving files between here and there',
fonts='family=Onest:wght@400;500;600&family=Fira+Code:wght@400;500',
idea='The Overview is a moving desk: a computer’s folder on the left, this computer on the right, the queue between them with the live speed. Drag across, or select and press the arrow. Transfers is the same desk.',
map=FIX + [['Move files', 'Overview: drag across, or select and press an arrow', 0], ['Transfers', 'The queue in the middle; Transfers page', 0], ['Speed', 'Top bar, with Pause all', 0], ['Search', 'Top bar or Ctrl K', 1]],
css=r'''
.D{--bg:#0f1512;--sf:#151d19;--rz:#1d2722;--t1:#e4ece7;--t2:#9fb0a6;--t3:#7e9086;--line:rgba(228,236,231,.08);--line2:rgba(228,236,231,.15);--ac:#7bd389;--on-ac:#0f1512;--ac-ink:#8ee09b;--ac-soft:rgba(123,211,137,.1);--pos:#7bd389;--neg:#f87171;--warn:#fbbf24;--font:"Onest",system-ui,sans-serif;--mono:"Fira Code",monospace;--shadow:0 16px 40px rgba(0,0,0,.5)}
.D .k-qr{background:#e4ece7}
.D .top{height:64px;flex:none;display:flex;align-items:center;gap:16px;padding:0 24px;border-bottom:1px solid var(--line)}
.D .top .sp0{display:flex;align-items:baseline;gap:10px;margin-right:auto}.D .top .sp0{white-space:nowrap}.D .top .sp0 b{font:500 26px var(--mono);color:var(--ac)}.D .top .sp0 small{color:var(--t3)}
.D .top .k-sbox{width:300px}
.D .desk{display:grid;grid-template-columns:1fr 300px 1fr;gap:16px;height:100%;min-height:0}
.D .pane{background:var(--sf);border-radius:14px;box-shadow:inset 0 0 0 1px var(--line);display:flex;flex-direction:column;min-height:0}
.D .pane h3{display:flex;align-items:center;gap:8px;margin:0;padding:14px 16px;border-bottom:1px solid var(--line);font:500 14px var(--font)}.D .pane h3 small{color:var(--t3);font:12px var(--mono)}
.D .pane .l{overflow:auto;padding:6px;flex:1}.D .pane .k-lrow{min-height:36px;font-size:13px}.D .pane .k-lrow small{font:12px var(--mono)}
.D .pane .h{padding:10px 16px;border-top:1px solid var(--line);font-size:12px;color:var(--t3);display:flex;gap:8px;align-items:center}
.D .q{display:flex;flex-direction:column;gap:10px;min-height:0;overflow:auto}
.D .arr{display:flex;gap:8px}.D .arr .k-btn{flex:1;justify-content:center}
.D .qi{background:var(--sf);border-radius:12px;box-shadow:inset 0 0 0 1px var(--line);padding:12px;font-size:13px;display:grid;gap:4px}.D .qi small{color:var(--t3);font:11px var(--mono)}.D .qi.w{box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--warn) 50%,transparent)}
.D .stage:has(.desk){overflow:hidden}
''',
js=r'''
document.getElementById('top').innerHTML=`<span class="sp0">${EM?`<b>0 B/s</b><small>nothing moving</small>`:`<b>52.4 MB/s</b><small>2 moving · 1 waiting · <span class="warn">3 need you</span></small>${K.btn('Pause all',{i:'pause',k:'sm'})}`}</span>${K.C.searchBox('Search all connected computers')}${K.C.actions()}`;
const pane=(h,rows,foot)=>`<div class="pane"><h3>${h}</h3><div class="l">${rows}</div><div class="h">${foot}</div></div>`;
const home=()=>`<div class="desk">`+(EM?pane(`${E('desktop_computer',18)}A computer`,B.W('desktop_computer','No computer yet','Add one and its folders open here, ready to drag across.',K.btn('Add a computer',{i:'plus',k:'sm pri',a:'dlg:newconn'})),'')
:pane(`${K.mach(D.servers[0],18)}studio-pc <small>garden-plan</small><span class="sp" style="flex:1"></span>${K.ib('chevd','Choose a computer and folder','menu:browse')}`,D.files.map(f=>`<div class="k-lrow${f.n===D.sel?' sel':''}" data-row="${f.n}">${K.fic(f.k)}<span>${f.n}</span><small>${f.k==='dir'?f.c+' items':f.s}</small></div>`).join(''),`${I('right',12)} drag right to download`))
+`<div class="q"><div class="arr">${K.btn('',{i:'left',k:EM?'':'',dis:EM}).replace('<button','<button data-tip="Upload the selection (Ctrl U)"')}${K.btn('',{i:'right',k:EM?'':'pri',dis:EM}).replace('<button','<button data-tip="Download the selection (Ctrl D)"')}</div>`+(EM?`<p class="mut" style="font-size:13px;text-align:center">The queue shows here: what moves, how fast, and what needs an answer.</p>`:[['↑ walkthrough-full.mov','71% · 23 s','',71],['↓ tax-2025.pdf','checking','',100],['↑ bed-layout.pdf','name exists on work-laptop','w'],['↓ album-2019.zip','paused · media-mini offline','w',33],['↑ seed-order.xlsx','waiting for a slot','',0]].map(x=>`<div class="qi ${x[2]}"><b style="font-weight:500">${x[0]}</b><small>${x[1]}</small>${x[3]!==undefined?K.bar(x[3]):K.btn('Keep both',{k:'sm'})}</div>`).join(''))+`</div>`
+pane(`${E('laptop',18)}This computer <small>~/Downloads</small>`,D.local.map(l=>`<div class="k-lrow" data-row="${l.n}">${K.fic(l.k)}<span>${l.n}</span><small>${l.k==='dir'?l.c+' items':l.s}</small></div>`).join(''),`${I('left',12)} drag left to upload`)+`</div>`;
B.mount({views:{home,transfers:EM?undefined:home}});
'''),
]
