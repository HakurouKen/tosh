# 版本与发布

`VERSION` 是唯一版本来源，初始版本为 `0.1.0`。使用 `make build` 时注入版本；直接 `go build` 的版本为 `dev`。Git tag 使用带 `v` 前缀的版本，例如 `v0.1.0`。

## 本地操作

```sh
make version             # 查看 VERSION
make build               # 构建 bin/tosh，注入版本
make bump PART=patch     # 0.1.0 → 0.1.1；默认 patch
make bump PART=minor     # 0.1.0 → 0.2.0
make bump PART=major     # 0.1.0 → 1.0.0
make dist                # 仅打包，不打 tag 或发布
```

`bump` 只修改 `VERSION`，不提交、不打 tag、不发布。当前支持正式版本 `major.minor.patch`，不支持预发布后缀。

## 发布流程

首次发布可直接使用 `0.1.0`；后续版本先 bump，再审阅并提交所有待发布修改：

```sh
make bump PART=patch     # 首次发布 0.1.0 时跳过
git add VERSION          # 其他修改也需审阅并提交
git commit -m "chore: bump version"
make tag
make release
```

- `make tag` 要求工作区干净，创建指向当前提交的 annotated tag；同名 tag 已存在时拒绝覆盖。
- `make release` 要求工作区干净，当前版本的 annotated tag 指向 HEAD，且 origin 指向 `HakurouKen/tosh`。本机需安装 Go、Git、GitHub CLI，并完成 `gh auth login`。
- 发布命令先运行测试（含 race、PTY e2e）和 `go vet`，再构建安装包，重新检查工作区与 tag，推送版本 tag，最后通过 `gh release create --verify-tag` 创建 Release；不推送分支，不自动提交修改。
- 上传失败时，tag 可能已推送；检查 GitHub 状态后再重试。命令不会强推 tag，也不会覆盖已有 Release 或附件。

## 安装包

`make dist` 在 `dist/v<版本>/` 生成 macOS/Linux × arm64/amd64 四个 `.tar.gz` 和 `checksums.txt`。所有二进制关闭 CGO，注入相同版本；包内包含 `tosh`、README、MIT 许可证及所用 Go 模块根目录下的许可声明。

例如 `dist/v0.1.0/tosh_0.1.0_linux_amd64.tar.gz`。产物不会被 Git 跟踪。`dist` 可在工作区有修改时用于预览；正式 Release 只从干净且匹配 tag 的当前提交构建。

交叉编译不代表已在目标机器运行验证。发布前应检查目标平台交互行为。下载后校验 SHA-256：

```sh
sha256sum -c checksums.txt       # Linux，四个压缩包均在当前目录时
shasum -a 256 -c checksums.txt   # macOS，同上
```
