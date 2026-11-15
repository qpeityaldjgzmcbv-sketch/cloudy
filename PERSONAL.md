# Album Shelf 连接版的源码保存

本地当前开发分支为 `album-shelf`，对应个人仓库：

`https://github.com/qpeityaldjgzmcbv-sketch/cloudy/tree/album-shelf`

- `personal/album-shelf`：余音连接、本机播放历史与会话保存、日常版部署适配。
- `personal/main`：个人仓库保存的原播放器主线；本地 `main` 跟踪它。
- `origin`：原播放器上游 `https://github.com/nya-owo/cloudy.git`，保留供查询。

本机默认推送目标设为 `personal`。连接版改动只推送到 `album-shelf`，不强推或覆盖任何主线。需要升级播放器主线时另做兼容工作，不在整理文件时直接合并新功能。

余音的 `scripts/build_daily.py` 使用本目录构建连接版，因此构建前应保持在 `album-shelf`。构建会设置 `managedBuild=1`，阻止上游自动更新覆盖连接功能；`CLOUDY_PORTABLE_DIR` 让备用历史／会话跨日常程序版本保留。

正式代码、测试和本说明应提交。`build/`、`.mygo/`、`node_modules/`、意外生成的 `pkg/mod/` 依赖缓存、Python `__pycache__/` 不提交。缓存先保留，整理源码不代表已释放磁盘空间。

日常程序、账号、播放历史和资料库不跟着 Git 上传；本次整理不重建或替换已安装的 Cloudy。日常版部署、验证和备份说明在旁边余音仓库的 `docs/DAILY.md`。

## 本轮验证边界（2026-11-14）

部署路径、会话、整专桥接和前后导航专项连续五次通过，`go vet ./internal/...` 通过；连接版候选成功编译并核对复制后哈希。完整 `go test ./internal/... -timeout 90s` 未通过：三个字幕测试、资料夹测试、播放器临时文件占用失败，app 包累计超时。没有为整理源码修改字幕或播放器逻辑，未替换已安装程序。日志及详细记录在旁边余音仓库的 `docs/VALIDATION.md` 与本机 `artifacts/tidy-cloudy-*.log`。
