// Injects the gallery strip: ← prev · NN/20 title · next →, plus arrow-key paging.
(function(){
  const m=location.pathname.match(/(\d\d)-[^/]+\.html$/); if(!m) return;
  const n=+m[1], files=window.MK_FILES||[];
  const f=i=>files[i-1]; const s=document.createElement('div'); s.className='mk-strip';
  const prev=n>1?f(n-1):null, next=n<20?f(n+1):null;
  s.innerHTML=`<a href="index.html">▦ All 20 designs</a><span>·</span><b>${String(n).padStart(2,'0')} / 20 — ${document.title}</b><span class="sp"></span>`+
    (prev?`<a href="${prev}">← prev</a>`:'')+(next?`<a href="${next}">next →</a>`:'');
  document.body.prepend(s);
  addEventListener('keydown',e=>{if(e.target.closest('input,textarea'))return;
    if(e.key==='['&&prev)location=prev; if(e.key===']'&&next)location=next;});
})();
