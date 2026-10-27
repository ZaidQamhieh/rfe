import http.server,time,re
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        m=re.search(r'/(\d+)',self.path); time.sleep(int(m.group(1))/1000 if m else 0)
        self.send_response(200); self.send_header('Content-Type','image/gif'); self.send_header('Content-Length','43'); self.end_headers()
        self.wfile.write(b'GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;')
    def log_message(self,*a): pass
http.server.ThreadingHTTPServer(('127.0.0.1',8799),H).serve_forever()
