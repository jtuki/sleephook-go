# SleepHook-Go

定时锁屏工具 —— 强制自己按时睡觉，改掉熬夜坏习惯。

Go 重写版，单 exe 部署，零外部依赖。原版 [SleepHook](https://github.com/jtuki/SleepHook)（C++/MFC）。

## 特性

- 单 exe 部署，无需 DLL 或运行时
- YAML 多时段配置，修改后每分钟自动生效，无需重启
- 多显示器覆盖，支持不同分辨率/DPI 的扩展屏幕
- 低级钩子拦截键盘鼠标，无需 DLL 注入
- 锁屏文字动画（DVD 屏保风格），速度可配置

## 使用

1. 编辑 `config.yaml` 设置锁定时段
2. 将 `SleepHook.exe` 和 `config.yaml` 放同一目录
3. 运行（建议管理员权限，可阻止任务管理器）

## 配置

```yaml
message: "不熬夜！早点休息！"
speed: 2
opacity: 240
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
| `lock_periods` | 锁定时段列表，`start`/`end` 格式 `hh:mm` 或 `hh:mm:ss`，支持跨午夜 |

跨午夜时段（如 `23:50` → `00:20`）总时长不得超过 1 小时。

## 编译

```bash
# WSL / Linux / macOS 交叉编译
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui -s -w" -o SleepHook.exe
```

或使用 `build.sh` 输出到 `builds/` 目录。

## 紧急退出

- **Ctrl+Alt+Delete** → 任务管理器 → 结束 SleepHook.exe
- 远程编辑 `config.yaml` 清空 `lock_periods`，最多 1 分钟自动解锁
- `taskkill /F /IM SleepHook.exe`

## License

MIT
