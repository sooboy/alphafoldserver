package platform

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Browser RPC code is copied from the verified Node bridge and embedded in the Go binary.
//
//go:embed assets/browser-request.js
var bridgeSource string

type RPCRequest struct {
	RPC     string `json:"rpc"`
	Payload any    `json:"payload"`
	Kind    string `json:"kind,omitempty"`
}
type RPCResult struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	Identity string `json:"identity_hash"`
	Payload  []any  `json:"payload"`
	HTTP     int    `json:"http_status"`
}

func RPC(ctx context.Context, b Browser, method string, payload any, kind string) (RPCResult, error) {
	req, _ := json.Marshal(RPCRequest{method, payload, kind})
	source := "(()=>{const __AF_POOL_REQUEST__=" + string(req) + ";return " + strings.TrimSuffix(strings.TrimSpace(bridgeSource), ";") + ";})()"
	var out RPCResult
	if err := b.Eval(ctx, source, &out); err != nil {
		return out, err
	}
	if !out.OK {
		if out.Error == "" {
			out.Error = "PROTOCOL_CHANGED"
		}
		return out, fail(out.Error)
	}
	return out, nil
}
func waitPlatform(ctx context.Context, b Browser) error {
	for {
		var ready bool
		err := b.Eval(ctx, "!!window.WIZ_global_data?.SNlM0e", &ready)
		if err == nil && ready {
			return nil
		}
		if err != nil && err.Error() != "PLATFORM_PAGE_NOT_READY" {
			return err
		}
		select {
		case <-ctx.Done():
			return fail("PLATFORM_PAGE_NOT_READY")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func foldInput(in Input) []any {
	entities := make([]any, 0, len(in.Sequences))
	for _, r := range in.Sequences {
		entities = append(entities, []any{[]any{nil, r.Sequence, nil, nil, nil, nil, r.Copies, 1}})
	}
	return []any{in.Name, entities}
}
func arr(v any) []any { a, _ := v.([]any); return a }
func at(a []any, i int) any {
	if i >= 0 && i < len(a) {
		return a[i]
	}
	return nil
}
func str(v any) string { s, _ := v.(string); return s }
func num(v any) int {
	n, ok := v.(float64)
	if !ok || n != float64(int(n)) {
		return -1
	}
	return int(n)
}
func zipBytes(ctx context.Context, b Browser, remote string) ([]byte, error) {
	id, _ := json.Marshal(remote)
	expr := `(async()=>{try{const r=await fetch('/_/v2/folds/'+encodeURIComponent(` + string(id) + `)+ '/fold.zip',{credentials:'same-origin',signal:AbortSignal.timeout(45000)});if(!r.ok||r.redirected||!(r.headers.get('content-type')||'').includes('application/zip'))return {error:'DOWNLOAD_REJECTED'};let size=0,chunks=[];const reader=r.body.getReader();for(;;){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>50000000){await reader.cancel();return {error:'DOWNLOAD_TOO_LARGE'}}chunks.push(value)}let s='';for(const c of chunks)for(let i=0;i<c.length;i+=32768)s+=String.fromCharCode(...c.subarray(i,i+32768));return {base64:btoa(s)}}catch{return {error:'DOWNLOAD_FAILED'}}})()`
	var r struct {
		Error  string `json:"error"`
		Base64 string `json:"base64"`
	}
	if err := b.Eval(ctx, expr, &r); err != nil {
		return nil, err
	}
	if r.Error != "" {
		return nil, fail(r.Error)
	}
	bytes, err := base64.StdEncoding.DecodeString(r.Base64)
	if err != nil || len(bytes) < 22 || string(bytes[:4]) != "PK\x03\x04" {
		return nil, fail("INVALID_ZIP")
	}
	return bytes, nil
}
