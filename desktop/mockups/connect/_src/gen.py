# Builds desktop/mockups/connect/cN-slug.html: five ways to add a computer, on the v4 design 6 shell.
import json
OUT = '/home/zaid/Projects/remote-file-explorer/desktop/mockups/connect'
THEME = r'''
.D{--bg:#f6f6f1;--sf:#fff;--rz:#ecece4;--t1:#1d2016;--t2:#4f5446;--t3:#6e7363;--line:rgba(29,32,22,.08);--line2:rgba(29,32,22,.15);--ac:#4d6b1f;--ac-ink:#435e1a;--ac-soft:rgba(77,107,31,.09);--font:"Figtree",system-ui,sans-serif;--mono:"JetBrains Mono",monospace}
.D .top{height:54px;flex:none;display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:0 20px;border-bottom:1px solid var(--line)}
.D .cx-f{display:flex;align-items:center;gap:10px;height:44px;padding:0 12px;border-radius:10px;box-shadow:inset 0 0 0 1px var(--line2);background:var(--sf);color:var(--t3)}
.D .cx-f.on{box-shadow:inset 0 0 0 1px var(--ac),0 0 0 3px var(--ac-soft)}
.D .cx-f input{flex:1;min-width:0;border:0;outline:0;background:none;font:15px var(--mono);color:var(--t1)}
.D .cx-f input::placeholder{font-family:var(--font);color:var(--t3)}
.D .cx-h{font:500 12px var(--font);color:var(--t3);margin:20px 0 8px;display:flex;align-items:center;gap:8px}.D .cx-h .sp{flex:1}
.D .cx-pc{display:grid;grid-template-columns:28px 1fr auto;gap:12px;align-items:center;padding:10px 12px;border-radius:10px;cursor:pointer;width:100%;text-align:left;color:var(--t1)}
.D .cx-pc:hover{background:var(--rz)}.D .cx-pc.on{background:var(--ac-soft);box-shadow:inset 0 0 0 1px var(--ac)}
.D .cx-pc b{font-weight:500}.D .cx-pc small{display:block;color:var(--t3);font:12px var(--mono)}.D .cx-pc .st{font-size:12px;color:var(--t3)}
.D .cx-tip{font-size:13px;color:var(--t2);margin:12px 0 0;display:flex;gap:8px;align-items:flex-start}.D .cx-tip svg{flex:none;margin-top:2px;color:var(--t3)}
.D .cx-tip code,.D .cx-none code{font:12px var(--mono);background:var(--rz);padding:1px 6px;border-radius:4px}
.D .cx-none{padding:16px;border-radius:10px;background:var(--rz);font-size:13px;color:var(--t2);display:grid;gap:6px}.D .cx-none b{color:var(--t1);font-weight:500}
.D .cx-scan{display:inline-flex;align-items:center;gap:6px;font-size:12px;color:var(--t3)}.D .cx-scan i{width:6px;height:6px;border-radius:3px;background:var(--ac)}
'''
SHARED = r'''
document.getElementById('top').innerHTML=`<button class="b-st" data-k="dlg:newconn">${I('plus',16)}Add a computer</button>`+K.C.actions();
const FOUND=EM?[]:[{n:'garage-pi',a:'192.168.1.44:8765',os:'Raspberry Pi OS',k:'5C1E 88A0',em:'server'},{n:'den-desktop',a:'192.168.1.52:8765',os:'Windows 11',k:'A3F0 19C7',em:'desktop_computer'}];
const SAVED=EM?[]:[{n:'studio-pc',a:'192.168.1.20:8765'}];
const keyc=k=>`<span class="k-tag mono">${I('key',12)}${k}</span>`;
const scan=t=>`<span class="cx-scan"><i></i>${t}</span>`;
const NONE=`<div class="cx-none"><b>Nothing found on this network yet</b><span>On the PC, install RFE and run <code>rfe setup</code>. It starts the agent and shows a pairing code. PCs reached over Tailscale or the internet are not listed here: type their address.</span></div>`;
const dl=(t,body,acts,w,cls='')=>`<div class="k-dlg ${cls}" role="dialog" aria-label="${t}" style="width:${w}px"><div class="k-dh"><h2>${t}</h2>${K.ib('x','Close (Esc)','close')}</div><div class="k-db">${body}</div>${acts?`<div class="k-da">${acts}</div>`:''}</div>`;
'''
DESIGNS = [
dict(n=1, slug='one-field', title='One field — an address or a pairing code, and the PCs on this network below',
idea='No tabs and no form: one field that takes either an address (host:port) or a pairing code and says which it understood. Below it, the PCs found on this network are one click each. The name comes from the PC itself.',
css=r'''
.D .c1 .k-db{padding-top:8px}.D .c1 .kind{font-size:12px;color:var(--ac-ink);margin:8px 2px 0;display:flex;gap:6px;align-items:center}
''',
js=r'''
K.DLG.newconn=()=>dl('Add a computer',`<label class="cx-f on">${I('plug',16)}<input value="${EM?'':'192.168.1.44'}" placeholder="Address like 192.168.1.20, or a pairing code" aria-label="Address or pairing code"></label>${EM?`<p class="cx-tip">${I('info',14)}<span>An address reaches the PC directly; a pairing code (8 letters and digits from <code>rfe pair</code>) also asks the PC to let this computer in.</span></p>`:`<p class="kind">${I('check',14)}An address on this network · port 8765 is assumed</p>`}
<div class="cx-h">On this network<span class="sp"></span>${scan(EM?'Looking…':'2 found')}</div>${EM?NONE:FOUND.map((p,i)=>`<button class="cx-pc${i?'':' on'}">${E(p.em,24)}<span><b>${p.n}</b><small>${p.a} · ${p.os}</small></span>${keyc(p.k)}</button>`).join('')+SAVED.map(p=>`<button class="cx-pc" disabled style="opacity:.6">${E('desktop_computer',24)}<span><b>${p.n}</b><small>${p.a}</small></span><span class="st">already added</span></button>`).join('')}`,
K.btn('Cancel',{a:'close'})+K.btn(EM?'Continue':'Continue to garage-pi',{k:'pri',a:'dlg:trust',dis:EM}),520,'c1');
'''),
dict(n=2, slug='steps', title='Steps — find the PC, check its key, sign in, with the way ahead shown',
idea='Adding a computer is three steps, and the dialog says so: find it, check its key, sign in. The steps sit on the left so you always know how far you are; step one offers the network list and a typed address side by side, never as tabs.',
css=r'''
.D .c2 .k-db{display:grid;grid-template-columns:180px 1fr;gap:0;padding:0}
.D .c2 .sx{background:var(--rz);padding:20px 16px;display:grid;align-content:start;gap:4px;border-radius:0 0 0 16px}
.D .c2 .sx div{display:grid;grid-template-columns:24px 1fr;gap:8px;padding:8px;border-radius:8px;font-size:13px;color:var(--t3)}.D .c2 .sx div.on{background:var(--sf);color:var(--t1);box-shadow:inset 0 0 0 1px var(--line)}
.D .c2 .sx i{width:22px;height:22px;border-radius:11px;display:grid;place-items:center;font:500 12px var(--mono);font-style:normal;box-shadow:inset 0 0 0 1px var(--line2)}.D .c2 .sx .on i{background:var(--ac);color:#fff;box-shadow:none}
.D .c2 .sx small{display:block;font-size:12px;color:var(--t3)}
.D .c2 .pane{padding:20px 24px}.D .c2 .pane h3{font:500 15px var(--font);margin:0 0 4px}.D .c2 .pane p{margin:0 0 12px;font-size:13px}
.D .c2 .or{display:flex;align-items:center;gap:12px;color:var(--t3);font-size:12px;margin:16px 0 12px}.D .c2 .or:before,.D .c2 .or:after{content:"";flex:1;height:1px;background:var(--line)}
''',
js=r'''
K.DLG.newconn=()=>dl('Add a computer',`<div class="sx">${[['Find the PC','on the network or by address'],['Check its key','compare with the PC'],['Sign in','account or pairing code']].map((s,i)=>`<div class="${i?'':'on'}"><i>${i+1}</i><span>${s[0]}<small>${s[1]}</small></span></div>`).join('')}</div>
<div class="pane"><h3>Which PC?</h3><p>Pick one RFE found on this network, or type the address of any other.</p>${EM?NONE:FOUND.map((p,i)=>`<button class="cx-pc${i?'':' on'}">${E(p.em,24)}<span><b>${p.n}</b><small>${p.a} · ${p.os}</small></span><span class="st">${scan('online')}</span></button>`).join('')}
<div class="or">or type an address</div><label class="cx-f">${I('globe',16)}<input placeholder="host:port, e.g. 100.71.4.18:8765" aria-label="Address"></label></div>`,
K.btn('Cancel',{a:'close'})+K.btn('Next: check the key',{k:'pri',a:'dlg:trust',i:'right',dis:EM}),720,'c2');
'''),
dict(n=3, slug='network-first', title='Network first — the PCs around you as cards, typing only when it is not there',
idea='Most people add a PC sitting on the same network, so the dialog opens on what it found: one card per PC with its system and key. Typing an address is the fallback, one line at the foot, for Tailscale and the internet.',
css=r'''
.D .c3 .grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-top:4px}
.D .c3 .card{display:grid;gap:6px;justify-items:start;padding:16px;border-radius:12px;box-shadow:inset 0 0 0 1px var(--line2);text-align:left;color:var(--t1);cursor:pointer}
.D .c3 .card:hover{background:var(--rz)}.D .c3 .card.on{box-shadow:inset 0 0 0 1.5px var(--ac);background:var(--ac-soft)}
.D .c3 .card b{font-weight:500;font-size:15px}.D .c3 .card small{color:var(--t3);font:12px var(--mono)}
.D .c3 .card.add{place-content:center;justify-items:center;color:var(--t3);box-shadow:inset 0 0 0 1px var(--line2);border:1px dashed transparent;background:repeating-linear-gradient(0deg,transparent 0 0)}
.D .c3 .foot{display:flex;gap:8px;align-items:center;margin-top:20px;padding-top:16px;border-top:1px solid var(--line)}.D .c3 .foot .cx-f{flex:1;height:40px}
.D .c3 .big{display:grid;justify-items:center;text-align:center;gap:8px;padding:28px 24px;color:var(--t2);font-size:13px}.D .c3 .big b{color:var(--t1);font-weight:500;font-size:15px}.D .c3 .big p{margin:0;max-width:420px}
.D .c3 .big pre{font:13px var(--mono);background:var(--rz);padding:8px 14px;border-radius:8px;margin:4px 0}
''',
js=r'''
K.DLG.newconn=()=>dl('Add a computer',`<div class="cx-h" style="margin-top:4px">On this network<span class="sp"></span>${scan(EM?'Looking for RFE on 192.168.1.0/24…':'Found 2 · looking for more')}</div>
${EM?`<div class="big">${E('satellite_antenna',40)}<b>No RFE on this network yet</b><p>On the PC you want to reach, install RFE and run</p><pre>rfe setup</pre><p>It appears here within a few seconds.</p></div>`:`<div class="grid">${FOUND.map((p,i)=>`<button class="card${i?'':' on'}">${E(p.em,32)}<b>${p.n}</b><small>${p.a}</small><small>${p.os}</small>${keyc(p.k)}</button>`).join('')}${SAVED.map(p=>`<button class="card" disabled style="opacity:.55">${E('desktop_computer',32)}<b>${p.n}</b><small>${p.a}</small><small>already added</small></button>`).join('')}</div>`}
<div class="foot"><label class="cx-f">${I('globe',16)}<input placeholder="Not listed? Type its address or a pairing code" aria-label="Address or pairing code"></label>${K.btn('Use',{k:'sm'})}</div>`,
K.btn('Cancel',{a:'close'})+K.btn(EM?'Add':'Add garage-pi',{k:'pri',a:'dlg:trust',dis:EM}),680,'c3');
'''),
dict(n=4, slug='code-first', title='Code first — type the code the PC shows, and that is the whole job',
idea='Built around the pairing code that rfe setup prints on the PC: eight boxes, typed once, and the PC is found, checked and let in. An address or the network list are quiet alternatives below, for PCs you sign in to with an account.',
css=r'''
.D .c4 .k-db{text-align:center;padding:12px 32px 20px}.D .c4 .k-db p{margin:0 auto 16px;max-width:400px}
.D .c4 .code{display:flex;justify-content:center;gap:8px;margin:8px 0 6px}.D .c4 .code span{width:44px;height:56px;border-radius:10px;box-shadow:inset 0 0 0 1px var(--line2);display:grid;place-items:center;font:500 24px var(--mono);color:var(--t1);background:var(--sf)}
.D .c4 .code span.on{box-shadow:inset 0 0 0 1.5px var(--ac),0 0 0 3px var(--ac-soft)}.D .c4 .code .gap{width:12px;box-shadow:none;background:none}
.D .c4 .code-note{font-size:12px;color:var(--t3)}
.D .c4 .alt{display:grid;gap:4px;margin-top:20px;padding-top:16px;border-top:1px solid var(--line);text-align:left}
.D .c4 .alt button{display:flex;align-items:center;gap:10px;height:40px;padding:0 12px;border-radius:8px;font-size:13px;color:var(--t2)}.D .c4 .alt button:hover{background:var(--rz);color:var(--t1)}.D .c4 .alt button small{margin-left:auto;color:var(--t3);font:12px var(--mono)}
.D .c4 .hit{display:inline-flex;gap:8px;align-items:center;font-size:13px;color:var(--ac-ink);margin-top:8px}
''',
js=r'''
const ch=EM?['','','','','','','','']:['K','7','Q','M','2','X','P',''];
K.DLG.newconn=()=>dl('Add a computer',`${E('keyboard',36)}<p>Type the code that <code style="font:12px var(--mono);background:var(--rz);padding:1px 6px;border-radius:4px">rfe setup</code> or <code style="font:12px var(--mono);background:var(--rz);padding:1px 6px;border-radius:4px">rfe pair</code> shows on the PC.</p>
<div class="code">${ch.map((c,i)=>(i===4?'<span class="gap"></span>':'')+`<span class="${(EM?i===0:i===7)?'on':''}">${c}</span>`).join('')}</div><div class="code-note">8 letters and digits · valid for 10 minutes</div>${EM?'':`<div class="hit">${scan('')}garage-pi on 192.168.1.44 is waiting for this code</div>`}
<div class="alt"><button>${I('globe',16)}Sign in with an account instead<small>address + password</small></button><button>${I('radar',16)}Pick from this network<small>${EM?'none found yet':'2 found'}</small></button></div>`,
K.btn('Cancel',{a:'close'})+K.btn('Connect',{k:'pri',a:'dlg:trust',dis:true}),500,'c4');
'''),
dict(n=5, slug='live-check', title='Live check — a side sheet that tests the address as you type and shows the key at once',
idea='Not a modal: a sheet on the right, in the same place and style as the inspector. As you type, it checks the address live: reachable, which agent, and the key you will be asked to trust, so comparing it with the PC happens here, not in a second dialog.',
css=r'''
.D .k-ov:has(.c5){place-items:stretch end;background:rgba(29,32,22,.12)}
.D .c5{height:100%;max-height:none;border-radius:0;width:400px!important;box-shadow:-16px 0 40px rgba(29,32,22,.14),-1px 0 0 var(--line2)}
.D .c5 .k-db{display:grid;align-content:start;gap:0}
.D .c5 .ck{display:grid;gap:8px;margin-top:12px;padding:12px;border-radius:10px;background:var(--rz);font-size:13px}
.D .c5 .ck div{display:grid;grid-template-columns:18px 1fr auto;gap:8px;align-items:center}.D .c5 .ck .ok{color:var(--pos)}.D .c5 .ck .wait{color:var(--t3)}.D .c5 .ck small{color:var(--t3);font:12px var(--mono)}
.D .c5 .fp{margin-top:12px;padding:16px;border-radius:12px;box-shadow:inset 0 0 0 1px var(--line2)}
.D .c5 .fp h4{margin:0 0 4px;font:500 14px var(--font)}.D .c5 .fp p{margin:0 0 12px;font-size:12px;color:var(--t3)}
.D .c5 .fp .g{display:grid;grid-template-columns:repeat(4,1fr);gap:6px 10px;font:500 15px var(--mono);color:var(--t1)}.D .c5 .fp .g span:nth-child(-n+4){color:var(--ac-ink)}
''',
js=r'''
const G='5C1E 88A0 77B0 2D19 C5E8 0A41 6B2F 93D7 E810 54CA 2F6E 1B98 D03C 7A15 46E2 B0F9'.split(' ');
K.DLG.newconn=()=>dl('Add a computer',`<label class="cx-f on">${I('plug',16)}<input value="${EM?'':'192.168.1.44'}" placeholder="Address or pairing code" aria-label="Address or pairing code"></label>
${EM?`<div class="ck">${[['Reachable'],['RFE agent'],['Key']].map(x=>`<div><span class="wait">${I('clock',14)}</span><span class="mut">${x[0]}</span><small>waits for an address</small></div>`).join('')}</div><div class="cx-h">On this network<span class="sp"></span>${scan('Looking…')}</div>${NONE}`:`<div class="ck"><div><span class="ok">${I('check',14)}</span><span>Reachable</span><small>3 ms · LAN</small></div><div><span class="ok">${I('check',14)}</span><span>RFE agent garage-pi</span><small>1.3.0 · Raspberry Pi OS</small></div><div><span class="ok">${I('check',14)}</span><span>Key received</span><small>not trusted yet</small></div></div>
<div class="fp"><h4>${E('key',16)} Compare this key with the PC</h4><p>On garage-pi run <code style="font:12px var(--mono)">rfe status</code>. All 16 groups must match.</p><div class="g">${G.map(g=>`<span>${g}</span>`).join('')}</div></div>
<div class="cx-h">Also on this network</div>${FOUND.slice(1).map(p=>`<button class="cx-pc">${E(p.em,24)}<span><b>${p.n}</b><small>${p.a}</small></span>${keyc(p.k)}</button>`).join('')}`}`,
K.btn('Cancel',{a:'close'})+K.btn(EM?'Trust and add':'They match: trust and add',{k:'pri',dis:EM}),400,'c5');
'''),
]
files = [f"c{d['n']}-{d['slug']}.html" for d in DESIGNS]
open(f'{OUT}/_files.js', 'w').write('window.MK_FILES=' + json.dumps(files) + ';\n')
for d, fn in zip(DESIGNS, files):
    html = f'''<!doctype html><html lang="en"><head><meta charset="utf-8"><title>{d['title']}</title>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Figtree:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap">
<link rel="stylesheet" href="_frame.css"><link rel="stylesheet" href="kit.css"><link rel="stylesheet" href="base.css">
<style>
{(THEME + d['css']).replace('.D', '.dc')}
</style>
<script src="_files.js"></script><script src="_nav.js"></script><script src="kit.js"></script><script src="base.js"></script></head>
<body class="mk"><div class="win rfe dc">
 <aside class="side" id="side"></aside>
 <div class="main"><header class="top" id="top"></header><div class="body"><main class="stage" id="st"></main><aside class="rail" id="rail"></aside></div><footer class="bot" id="bot"></footer></div>
 <div class="k-dockwrap" id="dock"></div><div class="pop" id="pop"></div>
</div>
<script>
window.IDEA={json.dumps(d['idea'])};
window.MAP=[];
const I=K.I,D=K.D,E=K.E,EM=B.empty;
{SHARED}
{d['js']}
B.mount({{}});
</script></body></html>
'''
    open(f'{OUT}/{fn}', 'w').write(html)
print(len(DESIGNS), 'connect designs')
