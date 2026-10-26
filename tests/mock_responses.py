#!/usr/bin/env python3
"""Mock OpenAI Responses API (SSE) for the Codex e2e test. Same 3-turn script
as mock_anthropic.py; every request body is appended to $MOCK_LOG."""
import json, os, re, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = os.environ.get("MOCK_LOG", "/tmp/mock_responses.log")
PH = re.compile(r"\{\{[A-Z_]+_[a-z2-7]{8}\}\}")

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass
    def do_GET(self):
        body = b'{"object":"list","data":[{"id":"mock-model","object":"model"}]}'
        self.send_response(200); self.send_header("content-type","application/json"); self.send_header("content-length",str(len(body))); self.end_headers(); self.wfile.write(body)
    def ev(self, data):
        chunk = f"event: {data['type']}\ndata: {json.dumps(data)}\n\n".encode()
        self.wfile.write(f"{len(chunk):x}\r\n".encode() + chunk + b"\r\n")
    def do_POST(self):
        n = int(self.headers.get("content-length", 0))
        raw = self.rfile.read(n)
        if self.headers.get("content-encoding") == "zstd":
            import subprocess
            raw = subprocess.run(["zstd","-dc"], input=raw, capture_output=True).stdout
        with open(LOG, "a") as f:
            f.write(json.dumps({"path": self.path, "body": raw.decode("utf-8","replace")}) + "\n")
        req = json.loads(raw)
        items = req.get("input", [])
        outs = [i for i in items if i.get("type") in ("function_call_output", "custom_tool_call_output")]
        tools = {t.get("name"): t for t in req.get("tools", []) if isinstance(t, dict)}
        def call(cmd):
            if "exec_command" in tools:
                return "exec_command", json.dumps({"cmd": cmd})
            if "shell_command" in tools:
                return "shell_command", json.dumps({"command": cmd})
            return "shell", json.dumps({"command": ["bash", "-lc", cmd]})
        if len(outs) == 0:
            name, args = call("cat secret.env")
            item = {"type":"function_call","id":"fc_1","call_id":"call_1","name":name,"arguments":args,"status":"completed"}
        elif len(outs) == 1:
            ph = PH.findall(json.dumps(outs))
            p = ph[0] if ph else "NO_PLACEHOLDER_SEEN"
            name, args = call(f"printf '%s' '{p}' > out.txt && echo wrote")
            item = {"type":"function_call","id":"fc_2","call_id":"call_2","name":name,"arguments":args,"status":"completed"}
        else:
            ph = PH.findall(json.dumps(items))
            item = {"type":"message","id":"msg_3","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done " + (ph[0] if ph else "none"),"annotations":[]}]}
        self.send_response(200)
        self.send_header("content-type", "text/event-stream"); self.send_header("transfer-encoding","chunked"); self.end_headers()
        rid = f"resp_{len(outs)}"
        self.ev({"type":"response.created","response":{"id":rid,"object":"response","status":"in_progress","output":[]}})
        self.ev({"type":"response.output_item.added","output_index":0,"item":item})
        self.ev({"type":"response.output_item.done","output_index":0,"item":item})
        self.ev({"type":"response.completed","response":{"id":rid,"object":"response","status":"completed","output":[item],
                 "usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}}})
        self.wfile.write(b"0\r\n\r\n")

if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
