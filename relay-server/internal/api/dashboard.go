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
<div id="app"><div class="header"><div><h1>Dashboard</h1><div class="sub">clipbridge.ccttkx.xyz</div></div><span class="tick" id="tick">--</span></div>

<div class="section-title">系统</div>
<div class="gauges">
  <div class="g"><div class="gv" id="g-cpu">--</div><div class="gl">CPU</div><div class="bar"><div id="b-cpu"></div></div></div>
  <div class="g"><div class="gv" id="g-mem">--</div><div class="gl">内存</div><div class="bar"><div id="b-mem"></div></div></div>
  <div class="g"><div class="gv" id="g-disk">--</div><div class="gl">磁盘剩余</div><div class="bar"><div id="b-disk"></div></div></div>
</div>

<div class="section-title">服务</div>

<div class="svc" id="svc-cb">
  <div class="svc-h" onclick="tgl('cb')"><div class="dot d0" id="d-cb"></div><div class="svc-n">ClipBridge 中继</div><div class="svc-s" id="s-cb">--</div><span class="arr" id="a-cb">▼</span></div>
  <div class="svc-b hidden" id="b-cb">
    <div class="row"><span class="lbl">在线用户</span><span id="cb-users">--</span></div>
    <div class="row"><span class="lbl">在线设备</span><span id="cb-devs">--</span></div>
    <div id="cb-list"></div>
  </div>
</div>

<div class="svc" id="svc-wg">
  <div class="svc-h" onclick="tgl('wg')"><div class="dot d0" id="d-wg"></div><div class="svc-n">WireGuard VPN</div><div class="svc-s" id="s-wg">--</div><span class="arr" id="a-wg">▼</span></div>
  <div class="svc-b hidden" id="b-wg">
    <div class="row"><span class="lbl">监听端口</span>51820</div>
    <div class="row"><span class="lbl">地址段</span><span class="v0">10.0.0.1/24</span></div>
    <div id="wg-list"></div>
    <div style="margin-top:10px;display:flex;gap:8px;flex-wrap:wrap">
      <button class="btn" onclick="showWGF('add')">+ 添加节点</button>
      <button class="btn" onclick="dload('wg')">导出配置</button>
      <button class="btn" onclick="action('wg','down')">停止服务</button>
      <button class="btn" onclick="action('wg','up')">启动服务</button>
    </div>
    <div class="cfg" id="wg-cfg"></div>
    <div class="form hidden" id="wg-add">
      <input placeholder="公钥" id="wg-pk"><input placeholder="IP (如 10.0.0.5/32)" id="wg-ip">
      <button class="btn" onclick="action('wg','add')">确认添加</button><button class="btn" onclick="showWGF('')">取消</button>
    </div>
  </div>
</div>

<div class="svc" id="svc-ss">
  <div class="svc-h" onclick="tgl('ss')"><div class="dot d0" id="d-ss"></div><div class="svc-n">Shadowsocks</div><div class="svc-s" id="s-ss">--</div><span class="arr" id="a-ss">▼</span></div>
  <div class="svc-b hidden" id="b-ss">
    <div class="row"><span class="lbl">端口</span><span id="ss-port">--</span></div>
    <div class="row"><span class="lbl">加密</span><span id="ss-method">--</span></div>
    <div class="row"><span class="lbl">密码</span><span id="ss-pass">--</span></div>
    <div class="row"><span class="lbl">活跃连接</span><span id="ss-count">--</span></div>
    <div id="ss-list"></div>
    <div style="margin-top:10px;display:flex;gap:8px">
      <button class="btn" onclick="dload('ss')">导出配置</button>
      <button class="btn" onclick="action('ss','down')">停止服务</button>
      <button class="btn" onclick="action('ss','up')">启动服务</button>
    </div>
    <div class="cfg" id="ss-cfg"></div>
  </div>
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
:root{--bg:#0a0a0f;--s:#12121a;--b:#1e1e2e;--t:#e4e4ed;--m:#6b6b7b;--g:#2dd4bf;--r:#f87171;--a:#fbbf24}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:var(--bg);color:var(--t);min-height:100vh;display:flex;justify-content:center;padding:40px 24px}
.main{width:100%;max-width:740px}
.login-box{background:var(--s);border:1px solid var(--b);border-radius:12px;padding:40px;text-align:center;max-width:360px;margin:120px auto}
.login-box h1{font-size:20px;margin-bottom:8px}
.login-box p{color:var(--m);font-size:13px;margin-bottom:24px}
.login-box input{width:100%;padding:12px 16px;border-radius:8px;border:1px solid var(--b);background:var(--bg);color:var(--t);font-size:15px;margin-bottom:12px;outline:none}
.login-box input:focus{border-color:var(--g)}
.login-box button{width:100%;padding:12px;border-radius:8px;border:none;background:var(--g);color:#000;font-size:15px;font-weight:600;cursor:pointer}
.login-box .err{color:var(--r);font-size:13px;margin-top:8px}
.header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:24px}
h1{font-size:22px;font-weight:500}.sub{color:var(--m);font-size:12px;margin-top:2px}
.tick{color:var(--m);font-size:11px}
.section-title{color:var(--m);font-size:11px;text-transform:uppercase;letter-spacing:.8px;margin:20px 0 8px}
.gauges{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:4px}
.g{background:var(--s);border:1px solid var(--b);border-radius:10px;padding:14px;text-align:center}
.gv{font-size:20px;font-weight:500}.gl{font-size:10px;color:var(--m);margin-top:2px}
.bar{margin-top:8px;height:4px;border-radius:2px;background:#1e1e2e;overflow:hidden}
.bar div{height:100%;border-radius:2px;transition:width .5s}
.svc{margin-bottom:6px}
.svc-h{display:flex;align-items:center;gap:12px;background:var(--s);border:1px solid var(--b);border-radius:10px;padding:16px 18px;cursor:pointer;transition:border-radius .2s}
.svc.open .svc-h{border-radius:10px 10px 0 0;border-bottom-color:transparent}
.svc-h:hover{border-color:#2a2a3e}
.svc-n{flex:1;font-size:14px;font-weight:500}
.svc-s{font-size:12px;color:var(--m)}
.arr{color:var(--m);font-size:11px;transition:transform .2s}
.svc.open .arr{transform:rotate(180deg)}
.svc-b{background:var(--s);border:1px solid var(--b);border-top:none;border-radius:0 0 10px 10px;padding:12px 18px 16px}
.svc-b.hidden{display:none}
.row{display:flex;justify-content:space-between;align-items:center;padding:9px 0;border-bottom:1px solid var(--b);font-size:13px}
.row:last-child{border:none}
.lbl{color:var(--m)}.v0{color:var(--g)}.v1{color:var(--r)}
.dot{width:8px;height:8px;border-radius:50%;display:inline-block;flex-shrink:0}
.d0{background:var(--g);box-shadow:0 0 8px rgba(45,212,191,.5)}
.d1{background:var(--r);box-shadow:0 0 8px rgba(248,113,113,.5)}
.tag{display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:500}
.t0{background:rgba(45,212,191,.12);color:var(--g)}.t1{background:rgba(248,113,113,.12);color:var(--r)}.t2{background:rgba(251,191,36,.12);color:var(--a)}
.btn{padding:7px 14px;border-radius:7px;font-size:12px;font-weight:500;cursor:pointer;border:1px solid var(--b);background:var(--s);color:var(--t);white-space:nowrap}
.btn:hover{border-color:var(--g)}.btn.danger{color:var(--r);border-color:var(--r)}
.cfg{background:var(--bg);border-radius:8px;padding:12px;font-size:11px;white-space:pre-wrap;word-break:break-all;max-height:160px;overflow-y:auto;color:var(--m);margin-top:10px;display:none}
.form{margin-top:10px;display:flex;gap:8px;flex-wrap:wrap}.form input{padding:7px 10px;border-radius:6px;border:1px solid var(--b);background:var(--bg);color:var(--t);font-size:13px;flex:1;min-width:150px;outline:none}
.form.hidden{display:none}
.ft{text-align:center;color:var(--m);font-size:11px;margin-top:24px}
</style></head><body><div class="main">`

const pageFoot = `<div class="ft">Dashboard · SecureCore</div></div><script>
var D=window._INIT||{};
function $(id){return document.getElementById(id)}
function bar(v,id){var e=$(id);e.style.width=v+'%';e.style.background=v>80?'var(--r)':v>60?'var(--a)':'var(--g)'}
function up(s){if(!s||s<=0)return'--';var d=Math.floor(s/86400),h=Math.floor((s%86400)/3600),m=Math.floor((s%3600)/60);if(d)return d+'天 '+h+'时';if(h)return h+'时 '+m+'分';return m+'分'}
function tgl(id){var b=$('b-'+id);b.classList.toggle('hidden');var s=$('svc-'+id);s.classList.toggle('open')}

function render(d){
  $('g-cpu').textContent=d.cpu;bar(d.cpu_pct,'b-cpu');
  $('g-mem').textContent=d.mem;bar(d.mem_pct,'b-mem');
  $('g-disk').textContent=d.disk+' GB';bar(d.disk_pct,'b-disk');
  $('tick').textContent=new Date().toLocaleTimeString('zh-CN');

  // ClipBridge
  $('d-cb').className='dot d0';
  $('s-cb').textContent='运行中 · '+up(d.uptime);
  $('cb-users').textContent=d.users||'--';
  $('cb-devs').textContent=d.online_devices||'--';
  var devs=d.devices||[],h='';
  for(var i=0;i<devs.length;i++){
    var dd=devs[i],tag='t1',label='离线';
    if(dd.Revoked){tag='t2';label='已撤销'}else if(dd.Online){tag='t0';label='在线'}
    h+='<div class="row"><span>'+esc(dd.Name)+'</span><span><span style="color:var(--m);font-size:11px;margin-right:6px">'+esc(dd.Plat)+'</span><span class="tag '+tag+'">'+label+'</span></span></div>'
  }
  $('cb-list').innerHTML=h||'<div class="row"><span class="lbl">暂无设备</span></div>';

  // WireGuard
  var peers=d.wg_peers||[];
  $('d-wg').className='dot '+(peers.length?'d0':'d1');
  $('s-wg').textContent=peers.length+' 节点';
  var wh='';
  for(var i=0;i<peers.length;i++){
    var p=peers[i];
    wh+='<div class="row" style="flex-wrap:wrap"><div style="width:100%;display:flex;justify-content:space-between;align-items:center"><span style="font-size:12px">'+esc(p.Key.substring(0,14))+'…</span><button class="btn danger" onclick="action(\'wg\',\'remove\',\''+p.Key+'\')">删除</button></div><div style="width:100%;display:flex;justify-content:space-between;margin-top:4px"><span class="v0" style="font-size:12px">'+esc(p.IP)+'</span><span style="font-size:11px;color:var(--m)">↓'+p.Tx+' ↑'+p.Rx+'</span></div><div style="width:100%;display:flex;justify-content:space-between;margin-top:2px"><span style="font-size:11px;color:var(--m)">'+esc(p.EP)+'</span><span style="font-size:11px;color:var(--m)">'+p.HS+'</span></div></div>'
  }
  $('wg-list').innerHTML=wh||'<div class="row"><span class="lbl">暂无节点</span></div>';
  if(d.wg_config){$('wg-cfg').textContent=d.wg_config;$('wg-cfg').style.display='none'}

  // Shadowsocks
  var ss=d.ss_config||{};
  $('d-ss').className='dot '+(d.ss_count?'d0':'d1');
  $('s-ss').textContent=(d.ss_count||0)+' 活跃连接';
  $('ss-port').textContent=ss.Port||'--';
  $('ss-method').textContent=ss.Method||'--';
  $('ss-pass').textContent=ss.Pass||'--';
  $('ss-count').textContent=(d.ss_count||0);
  var ch='';
  var conns=d.ss_conns||[];
  for(var i=0;i<conns.length;i++){ch+='<div class="row"><span>'+esc(conns[i])+'</span><span class="tag t0">已连接</span></div>'}
  $('ss-list').innerHTML=ch||'';
}

function esc(s){var d=document.createElement('div');d.textContent=s;return d.innerHTML}

async function refresh(){
  try{
    var r=await fetch('/dashboard/data');if(!r.ok)return;
    var d=await r.json();render(d);
  }catch(e){}
}

function showWGF(v){if(v){$('wg-pk').value='';$('wg-ip').value=''}$('wg-add').classList.toggle('hidden',!v)}

function dload(t){
  var el;
  if(t==='wg'){el=$('wg-cfg');el.style.display='block'}
  if(t==='ss'){
    var c=($('ss-port').textContent)+':'+($('ss-method').textContent);
    el=document.createElement('pre');el.textContent='端口: '+c+'\n密码: ****';
    document.body.appendChild(el);
  }
  if(!el)return;
  var b=new Blob([el.textContent],{type:'text/plain'});
  var a=document.createElement('a');a.href=URL.createObjectURL(b);a.download=(t==='wg'?'wg0.conf':'ss-config.txt');a.click();
  if(t==='ss')document.body.removeChild(el);
}

async function action(svc,act,pubkey){
  if(act==='remove'&&!confirm('确认删除此节点？'))return;
  if(act==='up'||act==='down'){if(!confirm('确认'+(act==='up'?'启动':'停止')+'服务？'))return}
  var fd=new FormData();
  fd.append('action',act);
  if(pubkey)fd.append('pubkey',pubkey);
  if(act==='add'){fd.append('pubkey',$('wg-pk').value);fd.append('ip',$('wg-ip').value)}
  try{
    await fetch('/dashboard/'+svc,{method:'POST',body:fd});
    setTimeout(refresh,500);
    if(act==='add')showWGF('');
  }catch(e){}
}

render(D);
refresh();
setInterval(refresh,5000);
</script></body></html>`
