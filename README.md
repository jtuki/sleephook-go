# SleepHook-Go

定时锁屏工具 —— 强制自己按时睡觉，改掉熬夜坏习惯。

原版 [SleepHook](https://github.com/jtuki/SleepHook) 使用 C++/MFC (VS2010) 编写，本版本用 Go 重写，零外部运行时依赖，单文件部署。

## 特性

- 单文件部署 —— 编译为一个 exe，无需 DLL 或运行时
- 多时间段配置 —— YAML 格式，支持任意数量的锁定时段
- 动态生效 —— 修改配置后无需重启，每分钟自动加载
- 多显示器覆盖 —— 覆盖所有屏幕，而非仅主显示器
- 低级钩子 —— 使用 `WH_KEYBOARD_LL` / `WH_MOUSE_LL`，无需 DLL 注入

## 快速开始

1. 从 [Releases](https://github.com/jtuki/sleephook-go/releases) 下载最新版，或自行编译
2. 在 exe 同目录下编辑 `config.yaml`
3. 运行 `SleepHook.exe`

## 配置

编辑 `config.yaml`（与 exe 同目录），使用 YAML 格式定义锁定时段：

```yaml
lock_periods:
  - start: "23:00"
    end:   "07:00"
```

多段锁定：

```yaml
lock_periods:
  - start: "09:00"
    end:   "09:30"
  - start: "13:00"
    end:   "14:00"
  - start: "23:00"
    end:   "07:00"
```

格式说明：

| 字段 | 格式 | 说明 |
|------|------|------|
| `start` | `"hh:mm"` 或 `"hh:mm:ss"` | 锁定开始时间（24 小时制） |
| `end` | `"hh:mm"` 或 `"hh:mm:ss"` | 锁定结束时间 |

支持跨午夜时段：`23:00` → `07:00` 表示晚 11 点到次日早 7 点。

### 动态修改

程序每分钟重新读取 `config.yaml`，无需重启：

- **新增时段** —— 最多 1 分钟后生效，若当前时间在新范围内立即锁定
- **删除时段** —— 最多 1 分钟后生效，若当前不在任何范围内立即解锁
- **清空所有时段** —— 等同于永久解锁
- 编辑过程中文件暂时不可读时，程序继续使用上一次有效配置

## 编译

需要 [Go 1.21+](https://go.dev/dl/)。

```bash
# Windows
go build -ldflags="-H windowsgui" -o SleepHook.exe

# Linux / macOS 交叉编译
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o SleepHook.exe
```

`-H windowsgui` 防止弹出控制台窗口。

## 使用

1. 编辑 `config.yaml` 设置锁定时段
2. 将 `SleepHook.exe` 和 `config.yaml` 放在同一目录
3. 运行 `SleepHook.exe`（建议以管理员权限运行以阻止任务管理器）
4. 到了锁定时段，屏幕被半透明黑色遮罩覆盖，键盘鼠标输入被拦截
5. 锁定期间鼠标光标可移动但点击无效

## 紧急退出

锁定期间无法操作屏幕，可通过以下方式退出：

- **Ctrl+Alt+Delete** → 打开任务管理器 → 结束 SleepHook.exe
- **远程编辑** `config.yaml` → 清空所有 `lock_periods` → 等待最多 1 分钟自动解锁
- **命令行结束**：`taskkill /F /IM SleepHook.exe`

## 与原版区别

| | SleepHook (C++/MFC) | SleepHook-Go |
|---|---|---|
| 部署 | exe + dll | 单文件 exe |
| 配置格式 | INI（行对） | YAML |
| 动态修改 | 不支持，需重启 | 每分钟自动加载 |
| 显示器 | 仅主屏幕 | 覆盖所有显示器 |
| 钩子方式 | 全局钩子 (DLL 注入) | 低级钩子 (无注入) |
| 运行时 | 需要 MFC DLL | 无外部依赖 |

## License

MIT
