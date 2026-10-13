package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealEmbeddedUI(t *testing.T) {
	if os.Getenv("AF_TEST_BROWSER") != "1" {
		t.Skip("set AF_TEST_BROWSER=1")
	}
	e, _ := fixtureEngine(t, 0)
	_, err := CreateUser(e.Store, "ui-member", "ui-password-1234", "member", "test")
	noerr(t, err)
	web := httptest.NewUnstartedServer(nil)
	origin := "http://" + web.Listener.Addr().String()
	handler, err := NewServer(e, origin)
	noerr(t, err)
	web.Config.Handler = handler
	web.Start()
	defer web.Close()
	exe, err := FindChrome("")
	noerr(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	chrome, err := startChrome(ctx, filepath.Join(t.TempDir(), "ui-profile"), exe, false, origin)
	noerr(t, err)
	defer chrome.Close()
	endpoint := ""
	for endpoint == "" {
		r, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", chrome.port))
		noerr(t, err)
		var pages []struct{ URL, WebSocketDebuggerURL string }
		noerr(t, json.NewDecoder(r.Body).Decode(&pages))
		r.Body.Close()
		for _, p := range pages {
			if strings.HasPrefix(p.URL, origin) {
				endpoint = p.WebSocketDebuggerURL
			}
		}
		if ctx.Err() != nil {
			t.Fatal("page timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
	eval := func(expr string) json.RawMessage {
		t.Helper()
		var result struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			Exception any `json:"exceptionDetails"`
		}
		noerr(t, cdp(ctx, endpoint, "Runtime.evaluate", map[string]any{"expression": expr, "awaitPromise": true, "returnByValue": true}, &result))
		require(t, result.Exception == nil, result.Exception)
		return result.Result.Value
	}
	wait := func(expr string) {
		t.Helper()
		for {
			if string(eval(expr)) == "true" {
				return
			}
			if ctx.Err() != nil {
				t.Fatal("UI wait timeout: " + expr)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	wait(`!!document.querySelector('#login-form')`)
	eval(`document.querySelector('#username').value='ui-member';document.querySelector('#password').value='ui-password-1234';document.querySelector('#login-form').requestSubmit();true`)
	wait(`!!document.querySelector('#workspace')`)
	require(t, string(eval(`![...document.querySelectorAll('nav button')].some(b=>b.textContent.includes('共享账号池'))`)) == "true", "admin controls visible")
	require(t, string(eval(`document.querySelector('#name-0').value.length>0`)) == "true", "default name missing")
	eval(`Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(document.querySelector('#seq-0-0'),'TEACHINGS');document.querySelector('#seq-0-0').dispatchEvent(new Event('input',{bubbles:true}));true`)
	eval(`[...document.querySelectorAll('button')].find(b=>b.textContent.includes('添加序列到此任务')).click();true`)
	wait(`!!document.querySelector('#seq-0-1')`)
	eval(`Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(document.querySelector('#seq-0-1'),'ACDEFG');document.querySelector('#seq-0-1').dispatchEvent(new Event('input',{bubbles:true}));true`)
	eval(`document.querySelector('#preview').click();true`)
	wait(`document.body.textContent.includes('2 个任务') || document.body.textContent.includes('1 个任务校验通过')`)
	require(t, len(e.Store.Snapshot().Jobs) == 0, "preview submitted")
	eval(`window.confirm=()=>true;document.querySelector('#submit').click();true`)
	wait(`document.querySelectorAll('#jobs tr').length===1`)
	require(t, len(e.Store.Snapshot().Jobs) == 1, "submit count")
	eval(`document.querySelector('#submit').click();true`)
	time.Sleep(300 * time.Millisecond)
	require(t, len(e.Store.Snapshot().Jobs) == 1, "duplicate submit")
	require(t, string(eval(`document.querySelector('#draft').disabled`)) == "true", "draft unlocked")
	var apiResult map[string]any
	noerr(t, json.Unmarshal(eval(`(async()=>{const r=await fetch('/api/v1/admin/accounts');return {status:r.status}})()`), &apiResult))
	require(t, apiResult["status"] == float64(403), apiResult)
	noerr(t, cdp(ctx, endpoint, "Page.reload", map[string]any{}, nil))
	wait(`!!document.querySelector('#workspace')`)
	wait(`document.querySelector('#draft').disabled`)
	eval(`document.querySelector('#logout').click();true`)
	wait(`!!document.querySelector('#login-form')`)
	t.Log("embedded UI actual Chromium: login -> owner-only controls -> 2-entity preview (0 jobs) -> queue -> same-request retry (1 job) -> reload keeps locked draft -> admin API 403 -> logout; no upstream prediction")
}
