package main

import (
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
		webURL = fmt.Sprintf("http://%s", listener.Addr())

		mux := http.NewServeMux()
		mux.HandleFunc("/", serveIndex)
		mux.HandleFunc("/api/config", handleConfig)
		mux.HandleFunc("/api/autostart", handleAutostart)

		webSrv = &http.Server{Handler: mux}
		go webSrv.Serve(listener)
		logMsg("webui: listening on %s", webURL)
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

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, indexHTML)
}

type configResponse struct {
	Message     string       `json:"message"`
	Speed       int          `json:"speed"`
	Opacity     int          `json:"opacity"`
	LockPeriods []lockPeriod `json:"lock_periods"`
	AutoStart   bool         `json:"autostart"`
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
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
		resp := configResponse{
			Message:     cfg.Message,
			Speed:       speed,
			Opacity:     opacity,
			LockPeriods: cfg.LockPeriods,
			AutoStart:   isAutoStartEnabled(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodPost {
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
		cfg := configFile{
			Message:     req.Message,
			Speed:       req.Speed,
			Opacity:     req.Opacity,
			LockPeriods: req.LockPeriods,
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
		header := "# SleepHook-Go configuration\n# https://github.com/jtuki/sleephook-go\n\n"
		if err := os.WriteFile(configPath()+".tmp", []byte(header+string(data)), 0644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	}
}

func handleAutostart(w http.ResponseWriter, r *http.Request) {
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
input[type=text],input[type=time]{
  width:100%;padding:10px 14px;background:rgba(255,255,255,0.08);border:1px solid rgba(255,255,255,0.12);
  border-radius:10px;color:#e2e8f0;font-size:14px;outline:none;transition:border .2s;
}
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
let periods=[];

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

function toast(msg,ok){
  const t=$('toast');t.textContent=msg;t.className='toast '+(ok?'ok':'err')+' show';
  setTimeout(()=>t.className='toast',2500);
}

$('speed').oninput=function(){$('speedVal').textContent=this.value};
$('opacity').oninput=function(){$('opacityVal').textContent=this.value};

async function loadConfig(){
  try{
    const r=await fetch('/api/config');const d=await r.json();
    $('message').value=d.message||'';
    $('speed').value=d.speed||2;$('speedVal').textContent=d.speed||2;
    $('opacity').value=d.opacity||240;$('opacityVal').textContent=d.opacity||240;
    $('autostart').checked=d.autostart||false;
    periods=d.lock_periods||[];renderPeriods();
  }catch(e){toast('加载配置失败',false)}
}

async function saveConfig(){
  const cfg={
    message:$('message').value,
    speed:parseInt($('speed').value),
    opacity:parseInt($('opacity').value),
    lock_periods:periods.filter(p=>p.start&&p.end)
  };
  try{
    const r=await fetch('/api/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)});
    if(!r.ok){const t=await r.text();toast(t,false);return}
    const as=$('autostart').checked;
    await fetch('/api/autostart',{method:'POST',body:as?'true':'false'});
    toast('已保存 ✓',true);
  }catch(e){toast('保存失败',false)}
}

loadConfig();
</script>
</body>
</html>`
