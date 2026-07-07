package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"gopkg.in/yaml.v3"
)

const webUIPort = 12888

var (
	pShellExecuteW = shell32.NewProc("ShellExecuteW")
	webOnce        sync.Once
	webURL         string
	webHost        string
	webToken       string
	webSrv         *http.Server
)

func startWebUIServer() {
	webOnce.Do(func() {
		addr := fmt.Sprintf("127.0.0.1:%d", webUIPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			logMsg("webui: listen failed: %v", err)
			return
		}
		webHost = listener.Addr().String()
		var tokenErr error
		webToken, tokenErr = newWebToken()
		if tokenErr != nil {
			listener.Close()
			logMsg("webui: token generation failed: %v", tokenErr)
			return
		}
		webURL = fmt.Sprintf("http://%s/?token=%s", webHost, webToken)

		mux := http.NewServeMux()
		mux.HandleFunc("/", serveIndex)
		mux.HandleFunc("/api/config", handleConfig)
		mux.HandleFunc("/api/autostart", handleAutostart)

		webSrv = &http.Server{Handler: mux}
		go webSrv.Serve(listener)
		logMsg("webui: listening on http://%s", webHost)
	})
}

func openConfigUI() {
	startWebUIServer()
	if webURL == "" {
		showError("无法启动配置服务")
		return
	}
	urlPtr, _ := syscall.UTF16PtrFromString(webURL)
	openPtr, _ := syscall.UTF16PtrFromString("open")
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(openPtr)),
		uintptr(unsafe.Pointer(urlPtr)), 0, 0, 5)
}

func stopWebUI() {
	if webSrv != nil {
		webSrv.Close()
	}
}

func newWebToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, indexHTML)
}

type configResponse struct {
	Message      string             `json:"message"`
	Speed        int                `json:"speed"`
	Opacity      int                `json:"opacity"`
	LockPeriods  []lockPeriod       `json:"lock_periods"`
	NetworkCheck networkCheckConfig `json:"network_check"`
	AutoStart    bool               `json:"autostart"`
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if !validAPIRequest(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(configPath())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var cfg configFile
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		speed := cfg.Speed
		if speed < 1 {
			speed = 2
		}
		opacity := cfg.Opacity
		if opacity < 1 || opacity > 255 {
			opacity = 240
		}
		netCfg, err := normalizeNetworkCheckConfig(cfg.NetworkCheck)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		resp := configResponse{
			Message:      cfg.Message,
			Speed:        speed,
			Opacity:      opacity,
			LockPeriods:  cfg.LockPeriods,
			NetworkCheck: netCfg,
			AutoStart:    isAutoStartEnabled(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodPost {
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			http.Error(w, "content-type must be application/json", 415)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var req configResponse
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if !strings.Contains(string(body), "network_check") {
			netCfg, err := normalizeNetworkCheckConfig(networkCheckConfigFile{})
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			req.NetworkCheck = netCfg
		}
		allowedCountries := normalizeCountryCodes(req.NetworkCheck.AllowedCountries)
		if req.NetworkCheck.Enabled && len(allowedCountries) == 0 {
			http.Error(w, "开启网络检查时至少选择一个允许国家/地区", 400)
			return
		}
		providers := normalizeProviderIDs(req.NetworkCheck.Providers)
		if req.NetworkCheck.Enabled && len(providers) == 0 {
			http.Error(w, "开启网络检查时至少选择一个 IP 检查站点", 400)
			return
		}
		forceTimes, _, err := normalizeForceDisconnectTimes(req.NetworkCheck.ForceDisconnectTimes)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		actionsIn := make([]networkActionConfigFile, 0, len(req.NetworkCheck.Actions))
		for _, action := range req.NetworkCheck.Actions {
			actionsIn = append(actionsIn, networkActionConfigFile{
				Type:   action.Type,
				Script: action.Script,
			})
		}
		actions, err := normalizeNetworkActions(actionsIn)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		enabled := req.NetworkCheck.Enabled
		cfg := configFile{
			Message:     req.Message,
			Speed:       req.Speed,
			Opacity:     req.Opacity,
			LockPeriods: req.LockPeriods,
			NetworkCheck: networkCheckConfigFile{
				Enabled:              &enabled,
				AllowedCountries:     allowedCountries,
				Providers:            providers,
				Actions:              actionsToConfigFile(actions),
				ForceDisconnectTimes: forceTimes,
			},
		}
		if cfg.Message == "" {
			cfg.Message = "不熬夜！早点休息！"
		}
		if cfg.Speed < 1 {
			cfg.Speed = 2
		}
		if cfg.Opacity < 1 || cfg.Opacity > 255 {
			cfg.Opacity = 240
		}
		if len(cfg.LockPeriods) == 0 {
			http.Error(w, "至少需要一个锁定时段", 400)
			return
		}
		for i, p := range cfg.LockPeriods {
			if p.Start == "" || p.End == "" {
				http.Error(w, fmt.Sprintf("时段 %d 的开始/结束时间不能为空", i+1), 400)
				return
			}
			if _, err := parseTimeOfDay(p.Start); err != nil {
				http.Error(w, fmt.Sprintf("时段 %d 开始时间格式错误: %s", i+1, p.Start), 400)
				return
			}
			if _, err := parseTimeOfDay(p.End); err != nil {
				http.Error(w, fmt.Sprintf("时段 %d 结束时间格式错误: %s", i+1, p.End), 400)
				return
			}
		}
		data, err := yaml.Marshal(&cfg)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		content := []byte("# SleepHook-Go configuration\n# https://github.com/jtuki/sleephook-go\n" +
			"#\n# message: \u9501\u5c4f\u63d0\u793a\u8bed\n# speed: \u6587\u5b57\u79fb\u52a8\u901f\u5ea6 1-10 (\u9ed8\u8ba4 2)\n" +
			"# opacity: \u906e\u7f69\u900f\u660e\u5ea6 1-255 (\u9ed8\u8ba4 240\uff0c\u8d8a\u5c0f\u8d8a\u900f\u660e)\n" +
			"# lock_periods: \u9501\u5b9a\u65f6\u6bb5\u5217\u8868\uff0cstart/end: hh:mm \u6216 hh:mm:ss\n" +
			"# network_check.enabled: \u662f\u5426\u542f\u7528\u516c\u7f51 IP \u5730\u533a\u68c0\u67e5\n" +
			"# network_check.allowed_countries: \u5141\u8bb8\u7684 ISO 3166-1 alpha-2 \u56fd\u5bb6/\u5730\u533a\u7801\uff0c\u5982 SG/US/HK\n" +
			"# network_check.providers: \u516c\u7f51 IP \u68c0\u67e5\u7ad9\u70b9\uff0c\u9ed8\u8ba4\u5168\u90e8\u542f\u7528\uff0c\u68c0\u6d4b\u65f6\u8f6e\u8be2\u4f7f\u7528\uff1b\u4efb\u4e00\u6210\u529f\u8fd4\u56de\u547d\u4e2d\u5141\u8bb8\u56fd\u5bb6/\u5730\u533a\u5373\u901a\u8fc7\n" +
			"# network_check.actions: \u516c\u7f51 IP \u4e0d\u5728\u5141\u8bb8\u5217\u8868\u65f6\u6267\u884c\u7684\u52a8\u4f5c\uff1b\u9ed8\u8ba4 disconnect\uff0c\u53ef\u9009 powershell script\n" +
			"# network_check.force_disconnect_times: \u5b9a\u70b9\u6267\u884c\u7f51\u7edc\u5904\u7f6e\u52a8\u4f5c\u7684\u65f6\u523b\uff0c\u786e\u8ba4\u6216\u8d85\u65f6\u540e 120 \u5206\u949f\u5185\u6301\u7eed\u6267\u884c\n" +
			"#   \u8de8\u5348\u591c\u65f6\u6bb5\u603b\u65f6\u957f\u4e0d\u5f97\u8d85\u8fc71\u5c0f\u65f6\uff0c\u4fee\u6539\u540e1\u5206\u949f\u5185\u81ea\u52a8\u751f\u6548\n\n" +
			string(data))
		tmpPath := configPath() + ".tmp"
		if err := os.WriteFile(tmpPath, content, 0644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := os.Rename(tmpPath, configPath()); err != nil {
			os.Remove(tmpPath)
			if err2 := os.WriteFile(configPath(), content, 0644); err2 != nil {
				http.Error(w, err2.Error(), 500)
				return
			}
			logMsg("config saved via fallback (rename failed: %v)", err)
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	}
}

func validAPIRequest(w http.ResponseWriter, r *http.Request) bool {
	if webHost == "" || r.Host != webHost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-SleepHook-Token")
	}
	if webToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(webToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	if r.Method != http.MethodGet {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+webHost {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
	}
	return true
}

func actionsToConfigFile(actions []networkActionConfig) []networkActionConfigFile {
	out := make([]networkActionConfigFile, 0, len(actions))
	for _, action := range actions {
		out = append(out, networkActionConfigFile{
			Type:   action.Type,
			Script: action.Script,
		})
	}
	return out
}

func handleAutostart(w http.ResponseWriter, r *http.Request) {
	if !validAPIRequest(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	body, _ := io.ReadAll(r.Body)
	enable, _ := strconv.ParseBool(strings.TrimSpace(string(body)))
	if err := setAutoStart(enable); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(200)
	fmt.Fprint(w, "OK")
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>SleepHook 配置</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{
  font-family:"Segoe UI","Microsoft YaHei","PingFang SC",system-ui,sans-serif;
  min-height:100vh;
  background:linear-gradient(135deg,#0f0c29,#302b63,#24243e);
  color:#e2e8f0;
  display:flex;flex-direction:column;align-items:center;
  padding:40px 20px;
}
.stars{position:fixed;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:0;overflow:hidden}
.star{position:absolute;background:#fff;border-radius:50%;animation:twinkle var(--d) ease-in-out infinite}
@keyframes twinkle{0%,100%{opacity:.2}50%{opacity:1}}
.container{position:relative;z-index:1;width:100%;max-width:520px}
.header{text-align:center;margin-bottom:36px}
.moon{font-size:64px;display:block;margin-bottom:8px;animation:float 4s ease-in-out infinite}
@keyframes float{0%,100%{transform:translateY(0)}50%{transform:translateY(-10px)}}
.header h1{font-size:28px;font-weight:700;background:linear-gradient(135deg,#a78bfa,#f0abfc);-webkit-background-clip:text;-webkit-text-fill-color:transparent;margin-bottom:4px}
.header p{font-size:14px;color:#94a3b8}
.card{background:rgba(255,255,255,0.06);backdrop-filter:blur(20px);border:1px solid rgba(255,255,255,0.1);border-radius:16px;padding:24px;margin-bottom:20px}
.card-title{font-size:15px;font-weight:600;color:#c4b5fd;margin-bottom:16px;display:flex;align-items:center;gap:8px}
.card-title span{font-size:18px}
label{display:block;font-size:13px;color:#94a3b8;margin-bottom:6px}
input[type=text],input[type=time],textarea{
  width:100%;padding:10px 14px;background:rgba(255,255,255,0.08);border:1px solid rgba(255,255,255,0.12);
  border-radius:10px;color:#e2e8f0;font-size:14px;outline:none;transition:border .2s;
}
textarea{min-height:86px;resize:vertical;font-family:"Cascadia Code","Consolas",monospace;line-height:1.4}
input:focus{border-color:#a78bfa}
.range-row{display:flex;align-items:center;gap:12px}
.range-row input[type=range]{flex:1;-webkit-appearance:none;height:6px;background:rgba(255,255,255,0.15);border-radius:3px;outline:none}
.range-row input[type=range]::-webkit-slider-thumb{-webkit-appearance:none;width:20px;height:20px;border-radius:50%;background:linear-gradient(135deg,#a78bfa,#f0abfc);cursor:pointer;box-shadow:0 2px 8px rgba(167,139,250,0.4)}
.range-val{min-width:32px;text-align:center;font-size:14px;font-weight:600;color:#c4b5fd}
.periods{display:flex;flex-direction:column;gap:10px}
.period-row{display:flex;align-items:center;gap:10px}
.period-row input[type=time]{flex:1}
.period-row .arrow{color:#a78bfa;font-size:16px}
.btn-remove{width:32px;height:32px;border-radius:8px;border:none;background:rgba(239,68,68,0.2);color:#fca5a5;font-size:18px;cursor:pointer;display:flex;align-items:center;justify-content:center;transition:background .2s}
.btn-remove:hover{background:rgba(239,68,68,0.4)}
.btn-add{width:100%;padding:10px;border:2px dashed rgba(255,255,255,0.15);border-radius:10px;background:transparent;color:#94a3b8;font-size:14px;cursor:pointer;transition:all .2s;margin-top:4px}
.btn-add:hover{border-color:#a78bfa;color:#c4b5fd}
.toggle-row{display:flex;align-items:center;justify-content:space-between}
.toggle{position:relative;width:48px;height:26px;cursor:pointer}
.toggle input{opacity:0;width:0;height:0}
.toggle .slider{position:absolute;top:0;left:0;right:0;bottom:0;background:rgba(255,255,255,0.15);border-radius:13px;transition:.3s}
.toggle .slider:before{content:'';position:absolute;height:20px;width:20px;left:3px;bottom:3px;background:#fff;border-radius:50%;transition:.3s}
.toggle input:checked+.slider{background:linear-gradient(135deg,#a78bfa,#f0abfc)}
.toggle input:checked+.slider:before{transform:translateX(22px)}
.country-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:14px}
.provider-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:8px;margin-top:14px}
.force-times{display:flex;flex-direction:column;gap:10px;margin-top:14px}
.country-option{display:flex;align-items:center;gap:8px;padding:9px 10px;background:rgba(255,255,255,0.06);border:1px solid rgba(255,255,255,0.1);border-radius:8px;color:#cbd5e1;font-size:13px;cursor:pointer}
.provider-option{display:flex;align-items:center;gap:8px;padding:9px 10px;background:rgba(255,255,255,0.06);border:1px solid rgba(255,255,255,0.1);border-radius:8px;color:#cbd5e1;font-size:13px;cursor:pointer}
.action-option{display:flex;align-items:center;gap:8px;padding:9px 10px;background:rgba(255,255,255,0.06);border:1px solid rgba(255,255,255,0.1);border-radius:8px;color:#cbd5e1;font-size:13px;cursor:pointer;margin-top:14px}
.country-option input{accent-color:#a78bfa}
.provider-option input{accent-color:#a78bfa}
.action-option input{accent-color:#a78bfa}
.country-option:has(input:checked){border-color:#a78bfa;background:rgba(167,139,250,0.16);color:#f5f3ff}
.provider-option:has(input:checked){border-color:#a78bfa;background:rgba(167,139,250,0.16);color:#f5f3ff}
.action-option:has(input:checked){border-color:#a78bfa;background:rgba(167,139,250,0.16);color:#f5f3ff}
.country-option.disabled{opacity:.45;cursor:not-allowed}
.provider-option.disabled{opacity:.45;cursor:not-allowed}
.btn-save{width:100%;padding:14px;border:none;border-radius:12px;background:linear-gradient(135deg,#a78bfa,#f0abfc);color:#1e1b4b;font-size:16px;font-weight:700;cursor:pointer;transition:transform .15s,box-shadow .15s;box-shadow:0 4px 20px rgba(167,139,250,0.3)}
.btn-save:hover{transform:translateY(-2px);box-shadow:0 6px 28px rgba(167,139,250,0.5)}
.btn-save:active{transform:translateY(0)}
.toast{position:fixed;top:24px;left:50%;transform:translateX(-50%) translateY(-100px);padding:12px 24px;border-radius:12px;font-size:14px;font-weight:600;z-index:999;transition:transform .4s cubic-bezier(.175,.885,.32,1.275)}
.toast.show{transform:translateX(-50%) translateY(0)}
.toast.ok{background:#10b981;color:#fff}
.toast.err{background:#ef4444;color:#fff}
.footer{text-align:center;margin-top:12px;font-size:12px;color:#64748b}
</style>
</head>
<body>
<div class="stars" id="stars"></div>
<div class="container">
  <div class="header">
    <span class="moon">🌙</span>
    <h1>SleepHook</h1>
    <p>不熬夜，早点休息，爱惜身体 💤</p>
  </div>
  <div class="card">
    <div class="card-title"><span>💬</span> 提示语</div>
    <input type="text" id="message" placeholder="不熬夜！早点休息！">
  </div>
  <div class="card">
    <div class="card-title"><span>🎮</span> 动画速度</div>
    <div class="range-row">
      <span style="font-size:12px;color:#64748b">慢</span>
      <input type="range" id="speed" min="1" max="10" value="2">
      <span style="font-size:12px;color:#64748b">快</span>
      <span class="range-val" id="speedVal">2</span>
    </div>
  </div>
  <div class="card">
    <div class="card-title"><span>🌫️</span> 遮罩透明度</div>
    <div class="range-row">
      <span style="font-size:12px;color:#64748b">透</span>
      <input type="range" id="opacity" min="50" max="255" value="240">
      <span style="font-size:12px;color:#64748b">实</span>
      <span class="range-val" id="opacityVal">240</span>
    </div>
  </div>
  <div class="card">
    <div class="card-title"><span>⏰</span> 锁定时段</div>
    <div class="periods" id="periods"></div>
    <button class="btn-add" onclick="addPeriod()">+ 添加时段</button>
  </div>
  <div class="card">
    <div class="card-title"><span>🚀</span> 开机启动</div>
    <div class="toggle-row">
      <span style="font-size:13px;color:#94a3b8">登录时自动运行 SleepHook</span>
      <label class="toggle">
        <input type="checkbox" id="autostart">
        <span class="slider"></span>
      </label>
    </div>
  </div>
  <div class="card">
    <div class="card-title"><span>🌐</span> 网络检查</div>
    <div class="toggle-row">
      <span style="font-size:13px;color:#94a3b8">公网 IP 地区不在允许列表时执行处置动作</span>
      <label class="toggle">
        <input type="checkbox" id="networkEnabled" onchange="renderNetworkOptions()">
        <span class="slider"></span>
      </label>
    </div>
    <label style="margin-top:16px">允许国家/地区（多选，英文缩写 + 中文）</label>
    <div class="country-grid" id="countries"></div>
    <label style="margin-top:16px">IP 检查站点（多选，默认全部）</label>
    <div class="provider-grid" id="providers"></div>
    <label style="margin-top:16px">网络处置动作</label>
    <label class="action-option">
      <input type="checkbox" id="actionDisconnect">
      <span>断开 Windows 网络</span>
    </label>
    <label style="margin-top:14px">PowerShell 脚本（留空则不执行）</label>
    <textarea id="actionPowerShell" placeholder="wsl --shutdown"></textarea>
    <label style="margin-top:16px">定点网络处置提醒时刻</label>
    <div class="force-times" id="forceTimes"></div>
    <button class="btn-add" onclick="addForceTime()">+ 添加处置时刻</button>
  </div>
  <button class="btn-save" onclick="saveConfig()">保存配置</button>
  <div class="footer">修改后无需重启，1 分钟内自动生效</div>
</div>
<div class="toast" id="toast"></div>
<script>
function makeStars(){
  const c=document.getElementById('stars');
  for(let i=0;i<80;i++){
    const s=document.createElement('div');s.className='star';
    const size=Math.random()*2+1;
    s.style.cssText='width:'+size+'px;height:'+size+'px;left:'+Math.random()*100+'%;top:'+Math.random()*100+'%;--d:'+(Math.random()*3+2)+'s;animation-delay:'+(Math.random()*3)+'s';
    c.appendChild(s);
  }
}
makeStars();

const $=id=>document.getElementById(id);
const apiToken=new URLSearchParams(location.search).get('token')||'';
const apiPath=p=>p+(p.includes('?')?'&':'?')+'token='+encodeURIComponent(apiToken);
let periods=[];
let selectedCountries=['SG'];
let selectedProviders=[];
let forceTimes=[];
let networkActions=[{type:'disconnect'}];
const countryOptions=[
  ['SG','新加坡'],['US','美国'],['HK','香港'],
  ['JP','日本'],['TW','台湾'],['KR','韩国'],
  ['GB','英国'],['DE','德国'],['NL','荷兰'],
  ['CA','加拿大'],['AU','澳大利亚'],['FR','法国']
];
const providerOptions=[
  ['ipinfo','ipinfo.io'],['ifconfig','ifconfig.co'],
  ['ip-api','ip-api.com'],['ipapi','ipapi.co'],
  ['ipwhois','ipwho.is']
];

function renderPeriods(){
  const c=$('periods');c.innerHTML='';
  periods.forEach((p,i)=>{
    const d=document.createElement('div');d.className='period-row';
    d.innerHTML='<input type="time" value="'+p.start+'" onchange="periods['+i+'].start=this.value">\
<span class="arrow">→</span>\
<input type="time" value="'+p.end+'" onchange="periods['+i+'].end=this.value">\
<button class="btn-remove" onclick="removePeriod('+i+')">×</button>';
    c.appendChild(d);
  });
}
function addPeriod(start,end){
  periods.push({start:start||'23:00',end:end||'07:00'});renderPeriods();
}
function removePeriod(i){periods.splice(i,1);renderPeriods()}

function renderForceTimes(){
  const c=$('forceTimes');c.innerHTML='';
  forceTimes.forEach((t,i)=>{
    const d=document.createElement('div');d.className='period-row';
    d.innerHTML='<input type="time" value="'+t+'" onchange="forceTimes['+i+']=this.value">\
<button class="btn-remove" onclick="removeForceTime('+i+')">×</button>';
    c.appendChild(d);
  });
}
function addForceTime(t){forceTimes.push(t||'23:30');renderForceTimes()}
function removeForceTime(i){forceTimes.splice(i,1);renderForceTimes()}

function renderNetworkOptions(){
  renderCountries();
  renderProviders();
}

function renderCountries(){
  const c=$('countries');c.innerHTML='';
  const enabled=$('networkEnabled').checked;
  countryOptions.forEach(([code,name])=>{
    const checked=selectedCountries.includes(code)?'checked':'';
    const disabled=enabled?'':'disabled';
    const l=document.createElement('label');
    l.className='country-option '+(enabled?'':'disabled');
    l.innerHTML='<input type="checkbox" value="'+code+'" '+checked+' '+disabled+' onchange="toggleCountry(this)"> <span>'+code+' '+name+'</span>';
    c.appendChild(l);
  });
}

function toggleCountry(el){
  const code=el.value;
  if(el.checked&&!selectedCountries.includes(code))selectedCountries.push(code);
  if(!el.checked)selectedCountries=selectedCountries.filter(c=>c!==code);
}

function renderProviders(){
  const c=$('providers');c.innerHTML='';
  const enabled=$('networkEnabled').checked;
  providerOptions.forEach(([id,name])=>{
    const checked=selectedProviders.includes(id)?'checked':'';
    const disabled=enabled?'':'disabled';
    const l=document.createElement('label');
    l.className='provider-option '+(enabled?'':'disabled');
    l.innerHTML='<input type="checkbox" value="'+id+'" '+checked+' '+disabled+' onchange="toggleProvider(this)"> <span>'+id+' '+name+'</span>';
    c.appendChild(l);
  });
}

function toggleProvider(el){
  const id=el.value;
  if(el.checked&&!selectedProviders.includes(id))selectedProviders.push(id);
  if(!el.checked)selectedProviders=selectedProviders.filter(p=>p!==id);
}

function toast(msg,ok){
  const t=$('toast');t.textContent=msg;t.className='toast '+(ok?'ok':'err')+' show';
  setTimeout(()=>t.className='toast',2500);
}

$('speed').oninput=function(){$('speedVal').textContent=this.value};
$('opacity').oninput=function(){$('opacityVal').textContent=this.value};

async function loadConfig(){
  try{
    const r=await fetch(apiPath('/api/config'));const d=await r.json();
    $('message').value=d.message||'';
    $('speed').value=d.speed||2;$('speedVal').textContent=d.speed||2;
    $('opacity').value=d.opacity||240;$('opacityVal').textContent=d.opacity||240;
    $('autostart').checked=d.autostart||false;
    const net=d.network_check||{enabled:true,allowed_countries:['SG']};
    $('networkEnabled').checked=net.enabled!==false;
    selectedCountries=(net.allowed_countries&&net.allowed_countries.length?net.allowed_countries:['SG']).map(c=>String(c).toUpperCase());
    selectedProviders=(net.providers&&net.providers.length?net.providers:providerOptions.map(p=>p[0])).map(p=>String(p).toLowerCase());
    networkActions=(net.actions&&net.actions.length?net.actions:[{type:'disconnect'}]);
    $('actionDisconnect').checked=networkActions.some(a=>String(a.type).toLowerCase()==='disconnect'||String(a.type).toLowerCase()==='disconnect_windows_network');
    const ps=networkActions.find(a=>String(a.type).toLowerCase()==='powershell');
    $('actionPowerShell').value=ps&&ps.script?ps.script:'';
    forceTimes=(net.force_disconnect_times||[]).map(t=>String(t));
    renderNetworkOptions();
    renderForceTimes();
    periods=d.lock_periods||[];renderPeriods();
  }catch(e){toast('加载配置失败',false)}
}

async function saveConfig(){
  if($('networkEnabled').checked&&selectedCountries.length===0){toast('请选择至少一个允许国家/地区',false);return}
  if($('networkEnabled').checked&&selectedProviders.length===0){toast('请选择至少一个 IP 检查站点',false);return}
  const actions=[];
  if($('actionDisconnect').checked)actions.push({type:'disconnect'});
  const ps=$('actionPowerShell').value.trim();
  if(ps)actions.push({type:'powershell',script:ps});
  if($('networkEnabled').checked&&actions.length===0){toast('请至少选择一个网络处置动作',false);return}
  const cfg={
    message:$('message').value,
    speed:parseInt($('speed').value),
    opacity:parseInt($('opacity').value),
    lock_periods:periods.filter(p=>p.start&&p.end),
    network_check:{
      enabled:$('networkEnabled').checked,
      allowed_countries:selectedCountries,
      providers:selectedProviders,
      actions:actions,
      force_disconnect_times:forceTimes.filter(t=>t)
    }
  };
  try{
    const r=await fetch(apiPath('/api/config'),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)});
    if(!r.ok){const t=await r.text();toast(t,false);return}
    const as=$('autostart').checked;
    await fetch(apiPath('/api/autostart'),{method:'POST',body:as?'true':'false'});
    toast('已保存 ✓',true);
  }catch(e){toast('保存失败',false)}
}

loadConfig();
</script>
</body>
</html>`
