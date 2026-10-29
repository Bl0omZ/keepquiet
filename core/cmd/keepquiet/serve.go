package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"syscall"
)

// serve is a line-delimited JSON loop on stdio, for the Pi extension: one
// long-lived process, so the vault is loaded once and kept in memory.
//
//	→ {"id":1,"op":"maskRequest","payload":{...}}
//	← {"id":1,"payload":{...},"masked":2,"blocked":false,"summary":"API_KEY×2"}
//	→ {"id":2,"op":"restore","value":...}      ← {"id":2,"value":...,"changed":true}
//	→ {"id":3,"op":"maskValue","value":...}    ← {"id":3,"value":...,"masked":1,...}
//	← {"id":n,"error":"..."} when a request failed: the caller must fail closed.
type serveReq struct {
	ID       int64           `json:"id"`
	Op       string          `json:"op"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	ToolName string          `json:"toolName,omitempty"`
}

func serve(r io.Reader, w io.Writer) error {
	e, err := newEngine()
	if err != nil {
		return err
	}
	lock, err := e.st.serveLock(syscall.LOCK_SH)
	if err != nil {
		return err
	}
	defer lock.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		var q serveReq
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			_ = enc.Encode(map[string]any{"id": 0, "error": "bad request: " + err.Error()})
			continue
		}
		_ = enc.Encode(serveOne(e, q))
	}
	return sc.Err()
}

func serveOne(e *engine, q serveReq) (resp map[string]any) {
	defer func() {
		if p := recover(); p != nil {
			resp = map[string]any{"id": q.ID, "error": fmt.Sprint("panic: ", p)}
		}
	}()
	e.hits = nil
	cfg, err := loadConfig()
	if err != nil {
		return map[string]any{"id": q.ID, "error": err.Error()}
	}
	e.cfg = cfg
	switch q.Op {
	case "ping":
		return map[string]any{"id": q.ID, "ok": true}
	case "maskRequest":
		out := e.maskJSON(q.Payload)
		if err := e.save(); err != nil {
			return map[string]any{"id": q.ID, "error": err.Error()}
		}
		return map[string]any{"id": q.ID, "payload": json.RawMessage(out), "masked": e.masked(), "blocked": e.blocked(), "summary": e.summary(), "warning": e.warning()}
	case "maskValue", "restore":
		var v any
		if err := decodeJSON(q.Value, &v); err != nil {
			return map[string]any{"id": q.ID, "error": err.Error()}
		}
		var changed bool
		if q.Op == "restore" {
			if q.ToolName == "" || e.cfg.restoreToolInput() && !e.cfg.restoreDenied(q.ToolName) {
				v, changed = e.restoreValue(v)
			}
		} else {
			v, changed = e.maskValue(v)
			if err := e.save(); err != nil {
				return map[string]any{"id": q.ID, "error": err.Error()}
			}
		}
		return map[string]any{"id": q.ID, "value": v, "changed": changed, "masked": e.masked(), "blocked": e.blocked(), "summary": e.summary()}
	case "reload":
		return map[string]any{"id": q.ID, "ok": true}
	}
	return map[string]any{"id": q.ID, "error": "unknown op " + q.Op}
}
