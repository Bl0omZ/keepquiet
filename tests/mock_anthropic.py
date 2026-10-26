#!/usr/bin/env python3
"""Mock Anthropic Messages API (SSE) for agent-redact e2e tests (Claude Code "Bash" / Pi "bash").

Turn logic (by the last user message):
  - no tool_result yet           -> tool_use Bash "cat secret.env"
  - tool_result of cat            -> tool_use Bash writing the placeholder it saw to out.txt
  - tool_result of the write      -> final text echoing the placeholder
Every request body is appended to $MOCK_LOG (one JSON per line).
"""
import json, os, re, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = os.environ.get("MOCK_LOG", "/tmp/mock_anthropic.log")
PH = re.compile(r"\{\{[A-Z_]+_[a-z2-7]{8}\}\}")

def sse(h, ev, data):
    h.wfile.write(f"event: {ev}\ndata: {json.dumps(data)}\n\n".encode())

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        self.send_response(404); self.end_headers()
    def do_POST(self):
        n = int(self.headers.get("content-length", 0))
        body = self.rfile.read(n)
        with open(LOG, "a") as f:
            f.write(json.dumps({"path": self.path, "body": body.decode("utf-8", "replace")}) + "\n")
        if not self.path.startswith("/v1/messages") or "count_tokens" in self.path:
            self.send_response(200); self.send_header("content-type","application/json"); self.end_headers()
            self.wfile.write(b'{"input_tokens":10}'); return
        req = json.loads(body)
        msgs = req.get("messages", [])
        last = msgs[-1] if msgs else {}
        results = []
        if isinstance(last.get("content"), list):
            for c in last["content"]:
                if c.get("type") == "tool_result":
                    results.append(json.dumps(c.get("content")))
        n_results = sum(1 for m in msgs if isinstance(m.get("content"), list) for c in m["content"] if c.get("type")=="tool_result")
        tools = [t.get("name") for t in req.get("tools", [])]
        bash = next((t for t in tools if t in ("Bash", "bash")), None)
        if bash is None:
            block = {"type": "text", "text": "ok"}
        elif n_results == 0:
            block = {"type": "tool_use", "id": "toolu_01", "name": bash, "input": {"command": "cat secret.env", "description": "read"}}
        elif n_results == 1:
            ph = PH.findall(" ".join(results))
            p = ph[0] if ph else "NO_PLACEHOLDER_SEEN"
            block = {"type": "tool_use", "id": "toolu_02", "name": bash, "input": {"command": f"printf '%s' '{p}' > out.txt && echo wrote", "description": "write"}}
        else:
            allph = PH.findall(json.dumps(msgs))
            block = {"type": "text", "text": "done " + (allph[0] if allph else "none") + "\n"}
        self.send_response(200)
        self.send_header("content-type", "text/event-stream"); self.end_headers()
        sse(self, "message_start", {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":req.get("model","m"),"content":[],"stop_reason":None,"stop_sequence":None,"usage":{"input_tokens":10,"output_tokens":1}}})
        if block["type"] == "text":
            sse(self, "content_block_start", {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}})
            sse(self, "content_block_delta", {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":block["text"]}})
            stop = "end_turn"
        else:
            sse(self, "content_block_start", {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":block["id"],"name":block["name"],"input":{}}})
            sse(self, "content_block_delta", {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":json.dumps(block["input"])}})
            stop = "tool_use"
        sse(self, "content_block_stop", {"type":"content_block_stop","index":0})
        sse(self, "message_delta", {"type":"message_delta","delta":{"stop_reason":stop,"stop_sequence":None},"usage":{"output_tokens":5}})
        sse(self, "message_stop", {"type":"message_stop"})

if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
