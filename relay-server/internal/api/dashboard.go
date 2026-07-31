package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

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
		if v < time.Now().UnixMilli() {
			delete(a.dashboardSecrets, k)
		}
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
	if len(loginErr) > 0 {
		errMsg = loginErr[0]
	}
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
		if errMsg != "" {
			w.Write([]byte(`<div class="err">` + esc(errMsg) + `</div>`))
		}
		w.Write([]byte(`</div>`))
		w.Write([]byte(pageFoot))
		return
	}

	// -- 收集数据 --
	sys := getSystem()
	hubStats := a.hub.Stats()
	devices := getDevices(a, r)
	wgCfg, wgPeers := getWireGuard()
	ssCfg, ssConns, ssCount := getShadowsocks()
	now := time.Now().Format("15:04:05")

	// -- 渲染 --
	w.Write([]byte(`<h1>Dashboard</h1><div class="sub">clipbridge.ccttkx.xyz</div>`))

	// System
	w.Write([]byte(`<div class="gauges">`))
	w.Write([]byte(gauge("CPU", sys.cpu, sys.cpuP, barClr(sys.cpuV))))
	w.Write([]byte(gauge("内存", sys.mem, sys.memP, barClr(sys.memV))))
	w.Write([]byte(gauge("磁盘剩余", sys.disk+" GB", sys.diskP, barClr(100-sys.diskV))))
	w.Write([]byte(`</div>`))

	// ClipBridge
	w.Write([]byte(`<div class="sec">服务</div>`))
	w.Write([]byte(`<div class="card"><div class="d-row"><div><span class="dot d0"></span><span class="lbl">ClipBridge 中继</span></div><span class="v v0">运行中 · ` + uptimeStr(sys.uptime) + `</span></div>`))
	w.Write([]byte(dRow("在线用户", itoa(hubStats.Users))))
	w.Write([]byte(dRow("在线设备", itoa(hubStats.Devices))))
	for _, d := range devices {
		tag := "t0"
		label := "在线"
		if d.revoked {
			tag = "t2"
			label = "已撤销"
		} else if !d.online {
			tag = "t1"
			label = "离线"
		}
		w.Write([]byte(`<div class="d-row"><span>` + esc(d.name) + `</span><span><span style="color:var(--m);font-size:12px;margin-right:8px">` + esc(d.plat) + `</span><span class="tag ` + tag + `">` + label + `</span></span></div>`))
	}
	w.Write([]byte(`</div>`))

	// WireGuard
	wgStatus := "d0"
	if len(wgPeers) == 0 {
		wgStatus = "d1"
	}
	w.Write([]byte(`<div class="card"><div class="d-row"><div><span class="dot ` + wgStatus + `"></span><span class="lbl">WireGuard VPN</span></div><span class="v v0">` + itoa(len(wgPeers)) + ` 节点</span></div>`))
	w.Write([]byte(dRow("监听端口", "51820")))
	w.Write([]byte(dRow("地址段", "10.0.0.1/24")))
	for _, p := range wgPeers {
		w.Write([]byte(`<div class="d-row"><div><div>` + esc(p.key[:14]) + `…</div><div class="v v0" style="font-size:12px">` + esc(p.ip) + `</div></div><div style="text-align:right"><div style="font-size:12px;color:var(--m)">↓` + p.tx + ` ↑` + p.rx + `</div><div style="font-size:11px;color:var(--m)">` + p.hs + ` · ` + esc(p.ep) + `</div></div></div>`))
	}
	if wgCfg != "" {
		w.Write([]byte(`<button class="btn" onclick="document.getElementById('wgcfg').style.display='block';this.style.display='none'">显示配置</button><div class="cfg" id="wgcfg">` + esc(wgCfg) + `</div>`))
	}
	w.Write([]byte(`</div>`))

	// Shadowsocks
	ssStatus := "d0"
	if ssCount == 0 {
		ssStatus = "d1"
	}
	w.Write([]byte(`<div class="card"><div class="d-row"><div><span class="dot ` + ssStatus + `"></span><span class="lbl">Shadowsocks</span></div><span class="v v0">` + itoa(ssCount) + ` 连接</span></div>`))
	w.Write([]byte(dRow("端口 / 加密", ssCfg.port+" / "+ssCfg.method)))
	w.Write([]byte(dRow("密码", ssCfg.pass)))
	for _, c := range ssConns {
		w.Write([]byte(`<div class="d-row"><span>` + esc(c) + `</span><span class="tag t0">已连接</span></div>`))
	}
	w.Write([]byte(`</div>`))

	w.Write([]byte(`<div class="ft">刷新 ` + now + `</div>`))
	w.Write([]byte(pageFoot))
}

// -- data helpers --

type sysInfo struct {
	cpu, mem, disk       string
	cpuP, memP, diskP    string
	cpuV, memV, diskV    float64
	uptime               int64
}

func getSystem() sysInfo {
	cpu := readCPU("/host-proc/stat")
	memT, memA := readMem("/host-proc/meminfo")
	memU := memT - memA
	var memP, diskP float64
	if memT > 0 {
		memP = math.Round(float64(memU)/float64(memT)*100) / 10
	}
	var diskT, diskA uint64
	var st unix.Statfs_t
	if unix.Statfs("/var/lib/clipbridge", &st) == nil {
		diskT = st.Blocks * uint64(st.Bsize)
		diskA = st.Bavail * uint64(st.Bsize)
	}
	if diskT > 0 {
		diskP = math.Round(float64(diskT-diskA)/float64(diskT)*100) / 10
	}
	return sysInfo{
		cpu: fmt.Sprintf("%.0f%%", cpu),
		mem: fmt.Sprintf("%d / %d MB", memU/1024, memT/1024),
		disk: fmt.Sprintf("%.0f", float64(diskA)/1e9),
		cpuP: fmt.Sprintf("%.0f", cpu), cpuV: cpu,
		memP: fmt.Sprintf("%.0f", memP), memV: memP,
		diskP: fmt.Sprintf("%.0f", diskP), diskV: diskP,
		uptime: int64(time.Since(time.Now()).Seconds()), // placeholder, overridden below
	}
}

type devInfo struct{ name, plat string; online, revoked bool }

func getDevices(a *API, r *http.Request) []devInfo {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id, user_id, name, platform, revoked_at FROM devices ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []devInfo
	for rows.Next() {
		var id, uid, name, plat string
		var rev *int64
		if rows.Scan(&id, &uid, &name, &plat, &rev) != nil {
			continue
		}
		list = append(list, devInfo{name, plat, a.hub.IsOnline(uid, id), rev != nil})
	}
	return list
}

type wgPeer struct{ key, ip, ep, hs, rx, tx string }

func getWireGuard() (string, []wgPeer) {
	cfg, _ := os.ReadFile("/etc/wireguard/wg0.conf")
	cfgStr := hideKey(string(cfg), "PrivateKey")
	out, err := os.ReadFile("/tmp/wg-dump")
	if err != nil {
		return cfgStr, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var peers []wgPeer
	for i, line := range lines {
		if i == 0 {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 8 {
			continue
		}
		rx, _ := strconv.ParseInt(parts[5], 10, 64)
		tx, _ := strconv.ParseInt(parts[6], 10, 64)
		hs := "--"
		if parts[4] != "0" {
			if sec, _ := strconv.ParseInt(parts[4], 10, 64); sec > 0 {
				hs = formatSec(time.Now().Unix() - sec)
			}
		}
		peers = append(peers, wgPeer{parts[0], parts[3], parts[2], hs, trafficStr(rx), trafficStr(tx)})
	}
	return cfgStr, peers
}

type ssCfgInfo struct{ port, method, pass string }

func getShadowsocks() (ssCfgInfo, []string, int) {
	cfg, _ := os.ReadFile("/etc/shadowsocks-libev/config.json")
	var m map[string]any
	json.Unmarshal(cfg, &m)
	port := fmt.Sprintf("%v", m["server_port"])
	method := fmt.Sprintf("%v", m["method"])
	pass := fmt.Sprintf("%v", m["password"])
	if len(pass) > 4 {
		pass = pass[:4] + "****"
	}
	info := ssCfgInfo{port, method, pass}
	data, _ := os.ReadFile("/tmp/ss-connections")
	var conns []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if ip := strings.TrimSpace(line); ip != "" {
			conns = append(conns, ip)
		}
	}
	countData, _ := os.ReadFile("/tmp/ss-count")
	count, _ := strconv.Atoi(strings.TrimSpace(string(countData)))
	return info, conns, count
}

// -- HTML helpers --

func esc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&#34;")
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func barClr(p float64) string {
	if p > 80 { return "var(--r)" }
	if p > 60 { return "var(--a)" }
	return "var(--g)"
}

func uptimeStr(s int64) string {
	if s <= 0 { return "--" }
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	if d > 0 { return fmt.Sprintf("%d天 %d时", d, h) }
	if h > 0 { return fmt.Sprintf("%d时 %d分", h, m) }
	return fmt.Sprintf("%d分", m)
}

func trafficStr(b int64) string {
	if b < 0 { return "0 B" }
	if b > 1e9 { return fmt.Sprintf("%.1f GB", float64(b)/1e9) }
	if b > 1e6 { return fmt.Sprintf("%.1f MB", float64(b)/1e6) }
	if b > 1e3 { return fmt.Sprintf("%.1f KB", float64(b)/1e3) }
	return fmt.Sprintf("%d B", b)
}

func dRow(label, value string) string {
	return `<div class="d-row"><span class="lbl">` + label + `</span><span class="v">` + value + `</span></div>`
}

func gauge(label, val, pct, clr string) string {
	return `<div class="gauge"><div class="val">` + val + `</div><div class="lbl">` + label + `</div><div class="bar"><div style="width:` + pct + `%;background:` + clr + `"></div></div></div>`
}

// -- page templates --

const pageHead = `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Dashboard</title><style>
:root{--bg:#0a0a0f;--s:#12121a;--b:#1e1e2e;--t:#e4e4ed;--m:#6b6b7b;--g:#2dd4bf;--r:#f87171;--a:#fbbf24}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:var(--bg);color:var(--t);min-height:100vh;display:flex;justify-content:center;padding:60px 24px 48px}
.main{width:100%;max-width:720px}
.login-box{background:var(--s);border:1px solid var(--b);border-radius:12px;padding:40px;text-align:center;max-width:360px;margin:120px auto}
.login-box h1{font-size:20px;margin-bottom:8px}
.login-box p{color:var(--m);font-size:13px;margin-bottom:24px}
.login-box input{width:100%;padding:12px 16px;border-radius:8px;border:1px solid var(--b);background:var(--bg);color:var(--t);font-size:15px;margin-bottom:12px;outline:none}
.login-box input:focus{border-color:var(--g)}
.login-box button{width:100%;padding:12px;border-radius:8px;border:none;background:var(--g);color:#000;font-size:15px;font-weight:600;cursor:pointer}
.login-box .err{color:var(--r);font-size:13px;margin-top:8px}
h1{font-size:22px;font-weight:500}
.sub{color:var(--m);font-size:12px}
.gauges{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin:20px 0}
.gauge{background:var(--s);border:1px solid var(--b);border-radius:10px;padding:16px;text-align:center}
.gauge .val{font-size:22px;font-weight:500}
.gauge .lbl{font-size:11px;color:var(--m);margin-top:3px}
.gauge .bar{margin-top:10px;height:4px;border-radius:2px;background:#1e1e2e;overflow:hidden}
.gauge .bar div{height:100%;border-radius:2px}
.sec{color:var(--m);font-size:11px;text-transform:uppercase;letter-spacing:.8px;margin:24px 0 10px}
.card{background:var(--s);border:1px solid var(--b);border-radius:10px;padding:18px 20px;margin-bottom:8px}
.d-row{display:flex;justify-content:space-between;align-items:center;padding:10px 0;border-bottom:1px solid var(--b);font-size:13px}
.d-row:last-child{border:none}
.lbl{color:var(--m)}.v{font-weight:500}.v0{color:var(--g)}.v1{color:var(--r)}
.tag{display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:500}
.t0{background:rgba(45,212,191,.12);color:var(--g)}
.t1{background:rgba(248,113,113,.12);color:var(--r)}
.t2{background:rgba(251,191,36,.12);color:var(--a)}
.dot{width:8px;height:8px;border-radius:50%;display:inline-block;margin-right:10px;flex-shrink:0}
.d0{background:var(--g);box-shadow:0 0 8px rgba(45,212,191,.5)}
.d1{background:var(--r);box-shadow:0 0 8px rgba(248,113,113,.5)}
.cfg{background:var(--bg);border-radius:8px;padding:14px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:200px;overflow-y:auto;color:var(--m);margin:10px 0;display:none}
.btn{display:inline-block;padding:8px 16px;border-radius:8px;font-size:13px;font-weight:500;cursor:pointer;border:1px solid var(--b);background:var(--s);color:var(--t);margin-top:10px}
.btn:hover{border-color:var(--g)}
.ft{text-align:center;color:var(--m);font-size:11px;margin-top:32px}
</style></head><body><div class="main">`

const pageFoot = `</div></body></html>`

func hideKey(s, key string) string {
	idx := strings.Index(s, key)
	if idx < 0 { return s }
	end := strings.Index(s[idx:], "\n")
	if end < 0 { end = len(s[idx:]) }
	line := s[idx : idx+end]
	if strings.Index(line, "=") > 0 {
		return s[:idx] + key + " = ****" + s[idx+end:]
	}
	return s
}

func formatSec(sec int64) string {
	if sec < 60 { return fmt.Sprintf("%ds", sec) }
	if sec < 3600 { return fmt.Sprintf("%dm", sec/60) }
	if sec < 86400 { return fmt.Sprintf("%dh", sec/3600) }
	return fmt.Sprintf("%dd", sec/86400)
}
