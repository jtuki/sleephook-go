# SleepHook-Go

![SleepHook-Go](resource/readme-slogan.jpg)

定时锁屏工具 —— 强制自己按时睡觉，改掉熬夜坏习惯。

Go 重写版，Windows 单 exe 部署，零外部依赖。原版 [SleepHook](https://github.com/jtuki/SleepHook)（C++/MFC）。

## 平台支持

目前仅支持 Windows。程序依赖 Windows API、系统托盘、低级键盘/鼠标钩子、任务管理器限制、网卡禁用和 Windows 消息框等能力，不能在 Linux/macOS 上直接运行。

可以在 WSL、Linux 或 macOS 上交叉编译出 Windows 可执行文件，但生成的 `SleepHook.exe` 仍然只能在 Windows 上使用。

## 特性

- 单 exe 部署，无需 DLL 或运行时
- YAML 多时段配置，修改后每分钟自动生效，无需重启
- 多显示器覆盖，支持不同分辨率/DPI 的扩展屏幕
- 低级钩子拦截键盘鼠标，无需 DLL 注入
- 锁屏文字动画（DVD 屏保风格），速度可配置
- 每 200ms 扫描本地网卡 IP 变化；每次公网 IP 检查完成 3 秒后再次检查，IP 变化后立即确认是否仍在允许国家/地区列表；至少一个检查站点成功返回且成功结果均不命中允许列表时，执行可配置网络处置动作
- 托盘菜单可临时屏蔽网络检查 3/5/10 分钟

## 使用

1. 编辑 `config.yaml` 设置锁定时段
2. 将 `SleepHook.exe` 和 `config.yaml` 放同一目录
3. 运行（建议管理员权限，可阻止任务管理器，并允许执行网络处置动作）

右键托盘图标可选择“屏蔽网络检查 3/5/10 分钟”。未屏蔽且网络检查开启时，程序会每 200ms 扫描本地网卡 IP 指纹；本地 IP 变化会立即触发公网校验。公网校验通过多个 IP 定位服务确认当前 IP，只要任一成功返回的检查站点命中允许国家/地区就继续联网；超时或调用失败的站点不算命中，且如果本轮没有任何站点成功返回，则只记录日志、不主动执行处置动作。检查站点按 round-robin 轮换，降低单个站点访问频率。

## 配置

```yaml
message: "不熬夜！早点休息！"
speed: 2
opacity: 240
network_check:
  enabled: true
  allowed_countries:
    - "SG"
    - "US"
    - "HK"
  providers:
    - "ipinfo"
    - "ifconfig"
    - "ip-api"
    - "ipapi"
    - "ipwhois"
  actions:
    - type: "powershell"
      script: "wsl --shutdown"
  force_disconnect_times:
    - "23:30"
lock_periods:
  - start: "00:40"
    end:   "07:00"
  - start: "13:00"
    end:   "14:00"
```

| 字段 | 说明 |
|------|------|
| `message` | 锁屏显示文字（可选，有默认值） |
| `speed` | 文字移动速度 1-10（可选，默认 2） |
| `opacity` | 遮罩透明度 1-255（可选，默认 240，越小越透明） |
| `network_check.enabled` | 是否开启公网 IP 地区检查（旧配置未设置时默认开启） |
| `network_check.allowed_countries` | 允许联网的 ISO 3166-1 alpha-2 国家/地区码列表，如 `SG`、`US`、`HK` |
| `network_check.providers` | 公网 IP 检查站点列表，默认全部：`ipinfo`、`ifconfig`、`ip-api`、`ipapi`、`ipwhois`；任一成功结果命中允许国家/地区即通过 |
| `network_check.actions` | 公网 IP 不在允许列表时执行的动作列表；旧配置未设置时默认 `disconnect`；可选 `disconnect`（断开 Windows 网络）或单个 `powershell`（执行 `script` 指定的 PowerShell 脚本，如 `wsl --shutdown`） |
| `network_check.force_disconnect_times` | 定点网络处置提醒时刻列表，格式 `hh:mm` 或 `hh:mm:ss`；触发时先弹框，确认或 30 秒无操作会执行 `network_check.actions`，并在 120 分钟内持续阻止恢复；选择手动处理则本次不强制；右键托盘可取消网络处置 |
| `lock_periods` | 锁定时段列表，`start`/`end` 格式 `hh:mm` 或 `hh:mm:ss`，支持跨午夜 |

跨午夜时段（如 `23:50` → `00:20`）总时长不得超过 1 小时。

## 编译 Windows 版本

```bash
# WSL / Linux / macOS 上交叉编译 Windows exe
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui -s -w" -o SleepHook.exe
```

或使用 `build.sh` 输出到 `builds/` 目录。

## 紧急退出

- **Ctrl+Alt+Delete** → 任务管理器 → 结束 SleepHook.exe
- 远程编辑 `config.yaml` 清空 `lock_periods`，最多 1 分钟自动解锁
- `taskkill /F /IM SleepHook.exe`

## License

MIT
