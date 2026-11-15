# 连接版更新与本机构建

## 先确认交付目标

`album-shelf` 保存连接补丁，`origin/main` 是原项目上游，个人推送目标为 `personal/album-shelf`。操作 Git 前报告；检查工作区后再 fetch、比较分叉及合并，不强推。Git 没有文本冲突不代表程序已经构建，也不代表运行行为通过验证。

用户通过外层 `启动开发版Cloudy.cmd` 打开的是 `%LOCALAPPDATA%/Programs/Afterglow-Development/Cloudy/Cloudy.exe`，不是源码。此入口不需要用户运行 Go。要求更新正在使用的开发版时，应完成合并、构建、备份和部署，不要只交付源码或让用户自行构建。日常版是另一个部署目标，见余音 `docs/DAILY.md`。

## 工具链、缓存与网络

1. `go` 不在 PATH 只代表当前 shell 找不到它，不等于机器没有 Go。先检查已有工具链；本机曾使用外层 `artifacts/toolchains/go/bin/go.exe`（Go 1.27.4），与 `go.mod` 的版本要求核对后使用，不重复安装。
2. 当前工具执行环境的默认 `%LOCALAPPDATA%/go-build`、`%TEMP%` 可能不可写。构建前同时设置 `GOCACHE`、`GOMODCACHE`、`GOTMPDIR`、`TEMP` 和 `TMP` 为项目内可写目录。只设置 `GOTMPDIR` 不够：测试里的 `os.MkdirTemp` 使用 `TEMP`/`TMP`。
3. 系统代理不一定传给 Go。用户已开启 Clash for Windows 且端口为 7890 时，在当前进程设置 `HTTP_PROXY`/`HTTPS_PROXY` 为 `http://127.0.0.1:7890`；先核对端口，不永久修改系统设置。2026-11-15 直连 `proxy.golang.org` 的 IPv6 请求超时，使用此代理后依赖成功下载。
4. 测试使用隔离的账号设置、历史和会话目录，不能使用真实用户库或登录做自动播放测试。`APPDATA` 也要隔离。首次依赖下载后保留缓存，不为排错清空依赖或用户文件。

在仓库根目录配置当前 PowerShell 会话的示例：

```powershell
$cloudyGo = (Resolve-Path '../artifacts/toolchains/go/bin/go.exe').Path
$cloudyBuildRoot = Join-Path (Get-Location) 'build'
foreach ($name in @('go-cache', 'go-mod-cache', 'go-tmp', 'test-profile')) {
    New-Item -ItemType Directory -Path (Join-Path $cloudyBuildRoot $name) -Force | Out-Null
}
$env:GOCACHE = Join-Path $cloudyBuildRoot 'go-cache'
$env:GOMODCACHE = Join-Path $cloudyBuildRoot 'go-mod-cache'
$env:GOTMPDIR = Join-Path $cloudyBuildRoot 'go-tmp'
$env:TEMP = $env:GOTMPDIR
$env:TMP = $env:GOTMPDIR
$env:APPDATA = Join-Path $cloudyBuildRoot 'test-profile'
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'
# 仅在已确认本机 Clash 端口时设置：
$env:HTTP_PROXY = 'http://127.0.0.1:7890'
$env:HTTPS_PROXY = $env:HTTP_PROXY
& $cloudyGo version
& $cloudyGo test ./internal/... -timeout 120s
```

测试期间没有输出不代表下载卡住。检查 Go 的子进程：`app.test.exe` 表示界面测试已开始；为测试设 timeout，并获取最终输出。工具会话暂未返回退出码时，不重复启动同一测试。

## 构建与开发版部署

`scripts/build-shelf.ps1` 包含 Windows 资源复制、`mygo_noinspector` 标签、生产标志、程序身份和开发目录复制步骤，使用 `-Go <已有 go.exe 路径>`。注意该脚本目前硬编码 `1.2.12-shelf`：更新时必须依据 `mygo.json` 使用新的 `packageVersion`（本轮为 `1.2.17-shelf`），不能让新程序仍报告旧版本。可按脚本步骤构建候选；不得遗漏资源、标签和身份。原版 EXE 仅只读提供 Windows 资源，不启动或改写它；资源来源是否同版需另核对，不能把复制资源说成视觉验收。

候选放在 `build/`，生成的 `.syso` 在构建后移入 `build/` 保存，不提交二进制或缓存。开发版必须复制到正常用户应用目录：工作区构建产物可能继承低完整性标签，直接运行会导致托盘注册失败。

部署前即时查询 Cloudy 进程的路径，不以此前查询结果断言它仍在运行。不要强杀播放器；未运行时先把目标 EXE 复制到带时间戳的 `backups/`，再复制候选并核对 SHA256。保留启动脚本原有 `CLOUDY_BRIDGE_FILE` 和 `CLOUDY_PORTABLE_DIR` 设置，不改账号、收藏、历史和会话。部署后明确区分“程序文件已更新”和“已启动实机验收”。

## 2026-11-15 验证记录

已把上游 1.2.17 合入连接分支，构建并部署 `1.2.17-shelf` 到开发入口；旧 EXE 留在开发安装目录 `backups/`。构建成功，候选与部署文件 SHA256 一致；没有代用户启动新版或进行试听。

全量 `go test ./internal/...` 未全部通过：字幕下载/点击/状态三个测试失败，另有默认系统临时目录导致的文件测试失败。将 `TEMP`/`TMP` 一并隔离后，`internal/controlbridge`、`internal/player` 及 app 的 Bridge、Managed、CloseHides、QuitKeeps、History、SessionKeeps、RestoredPaused 专项通过。没有修复字幕逻辑，也没有重新跑完整字幕套件；不得把此次记录写成全量测试通过。
