package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// -- Auth & Page --

func (a *API) dashLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if a.dashboardPass == "" || r.FormValue("password") != a.dashboardPass {
		a.dashPage(w, r, "密码错误")
		return
	}
	tok := make([]byte, 32)
	rand.Read(tok)
	token := hex.EncodeToString(tok)
	a.dashboardMu.Lock()
	a.dashboardSecrets[token] = time.Now().Add(24 * time.Hour).UnixMilli()
	for k, v := range a.dashboardSecrets {
		if v < time.Now().UnixMilli() { delete(a.dashboardSecrets, k) }
	}
	a.dashboardMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "dash_token", Value: token, Path: "/dashboard",
		MaxAge: 86400, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *API) dashPage(w http.ResponseWriter, r *http.Request, loginErr ...string) {
	errMsg := ""
	if len(loginErr) > 0 { errMsg = loginErr[0] }
	tok, _ := r.Cookie("dash_token")
	authed := false
	if tok != nil {
		a.dashboardMu.Lock()
		exp, ok := a.dashboardSecrets[tok.Value]
		a.dashboardMu.Unlock()
		authed = ok && exp > time.Now().UnixMilli()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(pageHead))

	if !authed {
		w.Write([]byte(`<div class="login-box"><h1>Dashboard</h1><p>输入密码查看服务器状态</p><form method="POST"><input type="password" name="password" placeholder="密码" autofocus><button type="submit">进入</button></form>`))
		if errMsg != "" { w.Write([]byte(`<div class="err">` + esc(errMsg) + `</div>`)) }
		w.Write([]byte(`</div>`))
		w.Write([]byte(pageFoot))
		return
	}
	// Initial render with embedded data
	data := a.collectData(r)
	jsonData, _ := json.Marshal(data)
	w.Write([]byte(`<script>window._INIT=` + string(jsonData) + `</script>`))
	w.Write([]byte(panelHTML))
	w.Write([]byte(pageFoot))
}

// -- Data API --

func (a *API) dashData(w http.ResponseWriter, r *http.Request) {
	tok, _ := r.Cookie("dash_token")
	if tok == nil { a.write(w, 401, map[string]any{"error": "未授权"}); return }
	a.dashboardMu.Lock()
	exp, ok := a.dashboardSecrets[tok.Value]
	a.dashboardMu.Unlock()
	if !ok || exp <= time.Now().UnixMilli() { a.write(w, 401, map[string]any{"error": "未授权"}); return }
	a.write(w, 200, a.collectData(r))
}

func (a *API) collectData(r *http.Request) map[string]any {
	sys := getSystem()
	hubStats := a.hub.Stats()
	devices := getDevices(a, r)
	wgCfg, wgPeers := getWireGuard()
	ssCfg, ssConns, ssCount := getShadowsocks()

	return map[string]any{
		"cpu": sys.cpu, "cpu_pct": sys.cpuV,
		"mem": sys.mem, "mem_pct": sys.memV,
		"disk": sys.disk, "disk_pct": sys.diskV,
		"uptime": sys.uptime,
		"users": hubStats.Users, "online_devices": hubStats.Devices,
		"devices": devices, "wg_peers": wgPeers, "wg_config": wgCfg,
		"ss_config": ssCfg, "ss_conns": ssConns, "ss_count": ssCount,
	}
}

// -- Action APIs --

func (a *API) dashWGAction(w http.ResponseWriter, r *http.Request) {
	if !a.dashCookieOK(r) { a.write(w, 401, map[string]any{"error": "未授权"}); return }
	action := r.FormValue("action")
	switch action {
	case "add":
		pub := r.FormValue("pubkey")
		ip := r.FormValue("ip")
		if pub == "" || ip == "" { a.write(w, 400, map[string]any{"error": "缺少参数"}); return }
		exec.Command("nsenter", "--target", "1", "--net", "--mount",
			"wg", "set", "wg0", "peer", pub, "allowed-ips", ip).Run()
		// 追加到配置文件
		cfg, _ := os.ReadFile("/etc/wireguard/wg0.conf")
		newLine := fmt.Sprintf("\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", pub, ip)
		os.WriteFile("/etc/wireguard/wg0.conf", append(cfg, []byte(newLine)...), 0600)
		// 刷新 cron dump
		exec.Command("nsenter", "--target", "1", "--net", "wg", "show", "wg0", "dump").Output()
	case "remove":
		pub := r.FormValue("pubkey")
		if pub == "" { a.write(w, 400, map[string]any{"error": "缺少公钥"}); return }
		exec.Command("nsenter", "--target", "1", "--net", "--mount",
			"wg", "set", "wg0", "peer", pub, "remove").Run()
		// 从配置文件移除
		cfg, _ := os.ReadFile("/etc/wireguard/wg0.conf")
		lines := strings.Split(string(cfg), "\n")
		var out []string
		skip := false
		for _, line := range lines {
			if strings.HasPrefix(line, "[Peer]") {
				skip = false
			}
			if strings.Contains(line, pub) {
				skip = true; continue
			}
			if !skip { out = append(out, line) }
		}
		os.WriteFile("/etc/wireguard/wg0.conf", []byte(strings.Join(out, "\n")), 0600)
	case "up":
		exec.Command("nsenter", "--target", "1", "--net", "--mount",
			"systemctl", "start", "wg-quick@wg0").Run()
	case "down":
		exec.Command("nsenter", "--target", "1", "--net", "--mount",
			"systemctl", "stop", "wg-quick@wg0").Run()
	}
	a.write(w, 200, map[string]any{"ok": true})
}

func (a *API) dashSSAction(w http.ResponseWriter, r *http.Request) {
	if !a.dashCookieOK(r) { a.write(w, 401, map[string]any{"error": "未授权"}); return }
	action := r.FormValue("action")
	switch action {
	case "up":
		exec.Command("nsenter", "--target", "1", "--mount", "systemctl", "start", "shadowsocks-libev").Run()
	case "down":
		exec.Command("nsenter", "--target", "1", "--mount", "systemctl", "stop", "shadowsocks-libev").Run()
	}
	a.write(w, 200, map[string]any{"ok": true})
}

func (a *API) dashCookieOK(r *http.Request) bool {
	tok, _ := r.Cookie("dash_token")
	if tok == nil { return false }
	a.dashboardMu.Lock()
	exp, ok := a.dashboardSecrets[tok.Value]
	a.dashboardMu.Unlock()
	return ok && exp > time.Now().UnixMilli()
}

// -- Data collectors --

type sysInfo struct {
	cpu, mem, disk       string
	cpuV, memV, diskV    float64
	uptime               int64
}

func getSystem() sysInfo {
	cpu := readCPU("/host-proc/stat")
	memT, memA := readMem("/host-proc/meminfo")
	memU := memT - memA
	var memP, diskP float64
	if memT > 0 { memP = math.Round(float64(memU)/float64(memT)*100) / 10 }
	var diskT, diskA uint64
	var st unix.Statfs_t
	if unix.Statfs("/var/lib/clipbridge", &st) == nil {
		diskT = st.Blocks * uint64(st.Bsize)
		diskA = st.Bavail * uint64(st.Bsize)
	}
	if diskT > 0 { diskP = math.Round(float64(diskT-diskA)/float64(diskT)*100) / 10 }
	return sysInfo{
		cpu: fmt.Sprintf("%.0f%%", cpu), mem: fmt.Sprintf("%d / %d MB", memU/1024, memT/1024),
		disk: fmt.Sprintf("%.0f", float64(diskA)/1e9),
		cpuV: cpu, memV: memP, diskV: diskP, uptime: int64(time.Since(time.Now()).Seconds()),
	}
}

type devInfo struct{ Name, Plat string; Online, Revoked bool }

func getDevices(a *API, r *http.Request) []devInfo {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id, user_id, name, platform, revoked_at FROM devices ORDER BY created_at`)
	if err != nil { return nil }
	defer rows.Close()
	var list []devInfo
	for rows.Next() {
		var id, uid, name, plat string; var rev *int64
		if rows.Scan(&id, &uid, &name, &plat, &rev) != nil { continue }
		list = append(list, devInfo{name, plat, a.hub.IsOnline(uid, id), rev != nil})
	}
	return list
}

type wgPeer struct{ Key, IP, EP, HS, Rx, Tx string }

func getWireGuard() (string, []wgPeer) {
	cfg, _ := os.ReadFile("/etc/wireguard/wg0.conf")
	cfgStr := hideKey(string(cfg), "PrivateKey")
	out, err := os.ReadFile("/tmp/wg-dump")
	if err != nil { return cfgStr, nil }
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var peers []wgPeer
	for i, line := range lines {
		if i == 0 { continue }
		parts := strings.Split(line, "\t")
		if len(parts) < 8 { continue }
		rx, _ := strconv.ParseInt(parts[5], 10, 64)
		tx, _ := strconv.ParseInt(parts[6], 10, 64)
		hs := "--"
		if parts[4] != "0" { if sec, _ := strconv.ParseInt(parts[4], 10, 64); sec > 0 { hs = formatSec(time.Now().Unix() - sec) } }
		peers = append(peers, wgPeer{parts[0], parts[3], parts[2], hs, trafficStr(rx), trafficStr(tx)})
	}
	return cfgStr, peers
}

type ssCfgInfo struct{ Port, Method, Pass string }

func getShadowsocks() (ssCfgInfo, []string, int) {
	cfg, _ := os.ReadFile("/etc/shadowsocks-libev/config.json")
	var m map[string]any; json.Unmarshal(cfg, &m)
	pass := fmt.Sprintf("%v", m["password"])
	if len(pass) > 4 { pass = pass[:4] + "****" }
	info := ssCfgInfo{fmt.Sprintf("%v", m["server_port"]), fmt.Sprintf("%v", m["method"]), pass}
	data, _ := os.ReadFile("/tmp/ss-connections")
	var conns []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if ip := strings.TrimSpace(line); ip != "" { conns = append(conns, ip) }
	}
	countData, _ := os.ReadFile("/tmp/ss-count")
	count, _ := strconv.Atoi(strings.TrimSpace(string(countData)))
	return info, conns, count
}

// -- HTML --

const panelHTML = `
<div id="app">
<div class="topbar">
  <div><span class="logo">&#9670;</span><span class="host">clipbridge.ccttkx.xyz</span></div>
  <span class="tick" id="tick"></span>
</div>

<div class="gauges">
  <div class="g"><svg class="gr" viewBox="0 0 36 36"><circle cx="18" cy="18" r="15" fill="none" stroke="var(--border)" stroke-width="3"/><circle id="cpu-ring" cx="18" cy="18" r="15" fill="none" stroke="var(--green)" stroke-width="3" stroke-dasharray="0 94" stroke-linecap="round" transform="rotate(-90 18 18)"/></svg><div class="gv" id="g-cpu">--</div><div class="gl">CPU</div></div>
  <div class="g"><svg class="gr" viewBox="0 0 36 36"><circle cx="18" cy="18" r="15" fill="none" stroke="var(--border)" stroke-width="3"/><circle id="mem-ring" cx="18" cy="18" r="15" fill="none" stroke="var(--green)" stroke-width="3" stroke-dasharray="0 94" stroke-linecap="round" transform="rotate(-90 18 18)"/></svg><div class="gv" id="g-mem">--</div><div class="gl">Memory</div></div>
  <div class="g"><svg class="gr" viewBox="0 0 36 36"><circle cx="18" cy="18" r="15" fill="none" stroke="var(--border)" stroke-width="3"/><circle id="disk-ring" cx="18" cy="18" r="15" fill="none" stroke="var(--green)" stroke-width="3" stroke-dasharray="0 94" stroke-linecap="round" transform="rotate(-90 18 18)"/></svg><div class="gv" id="g-disk">--</div><div class="gl">Disk</div></div>
</div>

<div class="svc" id="svc-cb">
  <div class="svc-h" onclick="tgl('cb')">
    <div class="svc-icon cb">&#9769;</div>
    <div class="svc-n">ClipBridge<span class="svc-desc">E2E 剪贴板中继</span></div>
    <div class="svc-s" id="s-cb"></div>
    <span class="arr" id="a-cb">&#9660;</span>
  </div>
  <div class="svc-b hidden" id="b-cb"></div>
</div>

<div class="svc" id="svc-wg">
  <div class="svc-h" onclick="tgl('wg')">
    <div class="svc-icon wg">&#9881;</div>
    <div class="svc-n">WireGuard<span class="svc-desc">VPN 隧道</span></div>
    <div class="svc-s" id="s-wg"></div>
    <span class="arr" id="a-wg">&#9660;</span>
  </div>
  <div class="svc-b hidden" id="b-wg"></div>
</div>

<div class="svc" id="svc-ss">
  <div class="svc-h" onclick="tgl('ss')">
    <div class="svc-icon ss">&#10004;</div>
    <div class="svc-n">Shadowsocks<span class="svc-desc">代理 · :8388</span></div>
    <div class="svc-s" id="s-ss"></div>
    <span class="arr" id="a-ss">&#9660;</span>
  </div>
  <div class="svc-b hidden" id="b-ss"></div>
</div>
</div>`

// -- Helpers --

func esc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;"); s = strings.ReplaceAll(s, "<", "&lt;"); s = strings.ReplaceAll(s, ">", "&gt;"); s = strings.ReplaceAll(s, "\"", "&#34;")
	return s
}
func itoa(n int) string { return strconv.Itoa(n) }
func uptimeStr(s int64) string {
	if s <= 0 { return "--" }; d := s/86400; h := (s%86400)/3600; m := (s%3600)/60
	if d > 0 { return fmt.Sprintf("%d天 %d时", d, h) }; if h > 0 { return fmt.Sprintf("%d时 %d分", h, m) }; return fmt.Sprintf("%d分", m)
}
func trafficStr(b int64) string {
	if b < 0 { return "0 B" }; if b > 1e9 { return fmt.Sprintf("%.1f GB", float64(b)/1e9) }; if b > 1e6 { return fmt.Sprintf("%.1f MB", float64(b)/1e6) }; if b > 1e3 { return fmt.Sprintf("%.1f KB", float64(b)/1e3) }; return fmt.Sprintf("%d B", b)
}
func hideKey(s, key string) string {
	idx := strings.Index(s, key); if idx < 0 { return s }; end := strings.Index(s[idx:], "\n"); if end < 0 { end = len(s[idx:]) }; line := s[idx : idx+end]
	if strings.Index(line, "=") > 0 { return s[:idx] + key + " = ****" + s[idx+end:] }; return s
}
func formatSec(sec int64) string {
	if sec < 60 { return fmt.Sprintf("%ds", sec) }; if sec < 3600 { return fmt.Sprintf("%dm", sec/60) }; if sec < 86400 { return fmt.Sprintf("%dh", sec/3600) }; return fmt.Sprintf("%dd", sec/86400)
}

// -- Page template --

const pageHead = `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Dashboard</title><style>
:root{--bg:#060608;--card:#0d0d14;--border:#1a1a28;--text:#e0e0ec;--muted:#5c5c72;--green:#2dd4bf;--red:#f87171;--amber:#fbbf24;--blue:#60a5fa}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:var(--bg);color:var(--text);min-height:100vh;display:flex;justify-content:center;padding:48px 24px 64px}
.main{width:100%;max-width:680px}
/* login */
.login-box{background:var(--card);border:1px solid var(--border);border-radius:16px;padding:48px;text-align:center;max-width:360px;margin:140px auto}
.login-box h1{font-size:22px;font-weight:600;letter-spacing:-0.3px;margin-bottom:6px}
.login-box p{color:var(--muted);font-size:14px;margin-bottom:28px}
.login-box input{width:100%;padding:14px 18px;border-radius:10px;border:1px solid var(--border);background:var(--bg);color:var(--text);font-size:15px;margin-bottom:14px;outline:none;transition:border-color .2s}
.login-box input:focus{border-color:var(--green)}
.login-box button{width:100%;padding:14px;border-radius:10px;border:none;background:var(--green);color:#000;font-size:15px;font-weight:600;cursor:pointer;transition:opacity .2s}
.login-box button:hover{opacity:.85}
.login-box .err{color:var(--red);font-size:13px;margin-top:10px}
/* topbar */
.topbar{display:flex;justify-content:space-between;align-items:center;margin-bottom:36px}
.logo{font-size:18px;color:var(--green);margin-right:10px}
.host{color:var(--text);font-size:14px;font-weight:500;letter-spacing:-0.2px}
.tick{color:var(--muted);font-size:12px}
/* gauges */
.gauges{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:32px}
.g{position:relative;background:var(--card);border:1px solid var(--border);border-radius:14px;padding:20px 16px 18px;text-align:center}
.gv{font-size:28px;font-weight:600;letter-spacing:-0.5px;margin:4px 0;position:relative;z-index:1}
.gl{font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px}
.gr{position:absolute;top:10px;left:50%;transform:translateX(-50%);width:56px;height:56px}
/* services */
.svc{margin-bottom:10px}
.svc-h{display:flex;align-items:center;gap:14px;background:var(--card);border:1px solid var(--border);border-radius:14px;padding:20px 22px;cursor:pointer;transition:border-color .2s,border-radius .3s}
.svc-h:hover{border-color:#28283e}
.svc.open .svc-h{border-radius:14px 14px 0 0;border-bottom-color:transparent}
.svc-icon{width:40px;height:40px;border-radius:10px;display:flex;align-items:center;justify-content:center;font-size:17px;flex-shrink:0}
.svc-icon.cb{background:rgba(45,212,191,.1);color:var(--green)}
.svc-icon.wg{background:rgba(96,165,250,.1);color:var(--blue)}
.svc-icon.ss{background:rgba(45,212,191,.1);color:var(--green)}
.svc-n{flex:1;font-size:15px;font-weight:500;line-height:1.3}
.svc-desc{display:block;font-size:12px;color:var(--muted);font-weight:400;margin-top:1px}
.svc-s{font-size:12px;color:var(--muted);text-align:right;line-height:1.4}
.arr{color:var(--muted);font-size:10px;transition:transform .3s;flex-shrink:0}
.svc.open .arr{transform:rotate(180deg)}
.svc-b{background:var(--card);border:1px solid var(--border);border-top:none;border-radius:0 0 14px 14px;padding:4px 22px 22px;overflow:hidden;animation:slideDown .25s ease}
.svc-b.hidden{display:none}
@keyframes slideDown{from{opacity:0;max-height:0}to{opacity:1;max-height:2000px}}
/* rows */
.row{display:flex;justify-content:space-between;align-items:center;padding:7px 0;font-size:13px}
.row+.row{border-top:1px solid rgba(255,255,255,.04)}
.lbl{color:var(--muted);font-size:13px}
.v0{color:var(--green);font-weight:500}
/* tags */
.tag{display:inline-block;padding:3px 10px;border-radius:5px;font-size:11px;font-weight:500}
.t0{background:rgba(45,212,191,.1);color:var(--green)}.t1{background:rgba(248,113,113,.1);color:var(--red)}.t2{background:rgba(251,191,36,.1);color:var(--amber)}
/* buttons */
.btn{padding:8px 16px;border-radius:8px;font-size:12px;font-weight:500;cursor:pointer;border:1px solid var(--border);background:transparent;color:var(--text);transition:all .15s;white-space:nowrap}
.btn:hover{border-color:var(--green);color:var(--green)}
.btn.danger:hover{border-color:var(--red);color:var(--red)}
.btn-group{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px;padding-top:14px;border-top:1px solid rgba(255,255,255,.04)}
/* form */
.f-row{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
.f-row input{padding:9px 14px;border-radius:8px;border:1px solid var(--border);background:var(--bg);color:var(--text);font-size:13px;flex:1;min-width:140px;outline:none;transition:border-color .2s}
.f-row input:focus{border-color:var(--green)}
.f-row.hidden{display:none}
/* config */
.cfg{background:var(--bg);border-radius:10px;padding:14px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:180px;overflow-y:auto;color:var(--muted);margin-top:12px;display:none;border:1px solid var(--border)}
/* peer card */
.peer{border:1px solid var(--border);border-radius:10px;padding:14px;margin-top:8px}
.peer-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:6px}
.peer-key{font-size:12px;color:var(--muted);font-family:monospace}
.peer-ip{font-size:14px;font-weight:600;color:var(--green)}
.peer-meta{display:flex;gap:20px;font-size:11px;color:var(--muted);margin-top:6px}
</style></head><body><div class="main">`

const pageFoot = `</div><script>
var D=window._INIT||{},C=94.25;
function $(id){return document.getElementById(id)}
function ring(id,pct){var r=$(id),d=C*pct/100;r.setAttribute('stroke-dasharray',d+' '+(C-d));r.setAttribute('stroke',pct>80?'var(--red)':pct>60?'var(--amber)':'var(--green)')}
function up(s){if(!s||s<=0)return'--';var d=Math.floor(s/86400),h=Math.floor((s%86400)/3600),m=Math.floor((s%3600)/60);if(d)return d+'天 '+h+'时';if(h)return h+'时 '+m+'分';return m+'分'}
function tgl(id){var b=$('b-'+id);b.classList.toggle('hidden');var s=$('svc-'+id);s.classList.toggle('open')}

function render(d){
  $('g-cpu').textContent=d.cpu;ring('cpu-ring',d.cpu_pct);
  $('g-mem').textContent=d.mem;ring('mem-ring',d.mem_pct);
  $('g-disk').textContent=d.disk+' GB';ring('disk-ring',d.disk_pct);
  $('tick').textContent=new Date().toLocaleTimeString('zh-CN');

  // ClipBridge
  $('s-cb').innerHTML='运行中<br>'+up(d.uptime);
  var ch='<div class="row"><span class="lbl">在线用户</span><span>'+d.users+'</span></div>';
  ch+='<div class="row"><span class="lbl">在线设备</span><span>'+(d.online_devices||0)+'</span></div>';
  var devs=d.devices||[];
  for(var i=0;i<devs.length;i++){
    var dd=devs[i],tag='t1',label='离线';
    if(dd.Revoked){tag='t2';label='已撤销'}else if(dd.Online){tag='t0';label='在线'}
    ch+='<div class="row"><span>'+esc(dd.Name)+'</span><span><span style="color:var(--muted);font-size:11px;margin-right:8px">'+esc(dd.Plat)+'</span><span class="tag '+tag+'">'+label+'</span></span></div>'
  }
  $('b-cb').innerHTML=ch;

  // WireGuard
  var peers=d.wg_peers||[];
  $('s-wg').innerHTML=(peers.length||0)+' 节点';
  var wh='<div class="row"><span class="lbl">端口</span>51820</div><div class="row"><span class="lbl">地址段</span><span class="v0">10.0.0.1/24</span></div>';
  for(var i=0;i<peers.length;i++){
    var p=peers[i];
    wh+='<div class="peer"><div class="peer-head"><span class="peer-key">'+esc(p.Key.substring(0,16))+'…</span><button class="btn danger" onclick="action(\'wg\',\'remove\',\''+p.Key+'\')">&#10005;</button></div><div class="peer-ip">'+esc(p.IP)+'</div><div class="peer-meta"><span>'+esc(p.EP)+'</span><span>'+p.HS+'</span><span>↓'+p.Tx+' ↑'+p.Rx+'</span></div></div>'
  }
  wh+='<div class="btn-group"><button class="btn" onclick="showWGF(\'add\')">+ 添加节点</button><button class="btn" onclick="dload(\'wg\')">导出配置</button><button class="btn" onclick="action(\'wg\',\'down\')">停止</button><button class="btn" onclick="action(\'wg\',\'up\')">启动</button></div>';
  wh+='<div class="f-row hidden" id="wg-add"><input placeholder="公钥" id="wg-pk"><input placeholder="IP (10.0.0.5/32)" id="wg-ip"><button class="btn" onclick="action(\'wg\',\'add\')">确认</button><button class="btn" onclick="showWGF(\'\')">取消</button></div>';
  wh+='<div class="cfg" id="wg-cfg"></div>';
  $('b-wg').innerHTML=wh;
  if(d.wg_config){$('wg-cfg').textContent=d.wg_config}

  // Shadowsocks
  var ss=d.ss_config||{};
  $('s-ss').innerHTML=''+d.ss_count+' 连接';
  var sh='<div class="row"><span class="lbl">端口</span><span>'+ss.Port+'</span></div>';
  sh+='<div class="row"><span class="lbl">加密</span><span>'+ss.Method+'</span></div>';
  sh+='<div class="row"><span class="lbl">密码</span><span>'+ss.Pass+'</span></div>';
  var conns=d.ss_conns||[];
  if(conns.length){
    sh+='<div style="margin-top:8px;font-size:12px;color:var(--muted)">客户端</div>';
    for(var i=0;i<conns.length;i++){sh+='<div class="row"><span>'+esc(conns[i])+'</span><span class="tag t0">已连接</span></div>'}
  }
  sh+='<div class="btn-group"><button class="btn" onclick="dload(\'ss\')">导出配置</button><button class="btn" onclick="action(\'ss\',\'down\')">停止</button><button class="btn" onclick="action(\'ss\',\'up\')">启动</button></div>';
  $('b-ss').innerHTML=sh;
}

function esc(s){var d=document.createElement('div');d.textContent=s;return d.innerHTML}

async function refresh(){
  try{var r=await fetch('/dashboard/data');if(r.ok)render(await r.json())}catch(e){}
}

function showWGF(v){if(v){$('wg-pk').value='';$('wg-ip').value=''}$('wg-add').classList.toggle('hidden',!v)}

function dload(t){
  var el;
  if(t==='wg'){el=$('wg-cfg');el.style.display=el.style.display==='block'?'none':'block';return}
  if(t==='ss'){
    el=document.getElementById('ss-dl-el');
    if(!el){el=document.createElement('pre');el.id='ss-dl-el';el.style.cssText='position:fixed;top:-9999px';document.body.appendChild(el)}
    var c=(ssC||{}).Port+':'+(ssC||{}).Method;
    el.textContent='端口: '+c+'\n密码: ****';
  }
  if(!el)return;
  var b=new Blob([el.textContent],{type:'text/plain'});
  var a=document.createElement('a');a.href=URL.createObjectURL(b);a.download=(t==='wg'?'wg0.conf':'ss-config.txt');a.click();
}
var ssC={};

async function action(svc,act,pubkey){
  if(act==='remove'&&!confirm('确认删除？'))return;
  if((act==='up'||act==='down')&&!confirm('确认'+(act==='up'?'启动':'停止')+'？'))return;
  var fd=new FormData();fd.append('action',act);
  if(pubkey)fd.append('pubkey',pubkey);
  if(act==='add'){fd.append('pubkey',$('wg-pk').value);fd.append('ip',$('wg-ip').value)}
  try{await fetch('/dashboard/'+svc,{method:'POST',body:fd});setTimeout(refresh,500);if(act==='add')showWGF('')}catch(e){}
}

render(D);
setInterval(refresh,5000);
</script></body></html>`
