# Builds desktop/mockups/v3/NN-slug.html from the design definitions in designs_*.py.
import glob, runpy, os, json
OUT = '/home/zaid/Projects/remote-file-explorer/desktop/mockups/v3'
D = []
for f in sorted(glob.glob('/tmp/mkshots/v3src/designs_*.py')): D += runpy.run_path(f)['DESIGNS']
D.sort(key=lambda d: d['n'])
files = [f"{d['n']:02d}-{d['slug']}.html" for d in D]
open(f'{OUT}/_files.js', 'w').write('window.MK_FILES=' + json.dumps(files) + ';\n')
for d, fn in zip(D, files):
    c = f"d{d['n']:02d}"
    html = f'''<!doctype html><html lang="en"><head><meta charset="utf-8"><title>{d['title']}</title>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?{d['fonts']}&display=swap">
<link rel="stylesheet" href="_frame.css"><link rel="stylesheet" href="kit.css"><link rel="stylesheet" href="base.css">
<style>
{d['css'].replace('.D', '.' + c)}
</style>
<script src="_files.js"></script><script src="_nav.js"></script><script src="kit.js"></script><script src="base.js"></script></head>
<body class="mk"><div class="win rfe {c}">
 <aside class="side" id="side"></aside>
 <div class="main"><header class="top" id="top"></header><div class="body"><main class="stage" id="st"></main><aside class="rail" id="rail"></aside></div><footer class="bot" id="bot"></footer></div>
 <div class="k-dockwrap" id="dock"></div><div class="pop" id="pop"></div>
</div>
<script>
window.IDEA={json.dumps(d['idea'])};
window.MAP={json.dumps(d['map'])};
const I=K.I,D=K.D,E=K.E,EM=B.empty;
{d['js']}
</script></body></html>
'''
    open(f'{OUT}/{fn}', 'w').write(html)
print(len(D), 'designs')
