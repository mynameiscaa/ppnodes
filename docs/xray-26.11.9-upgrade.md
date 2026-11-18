# Xray 26.11.9 升级记录

升级日期：2026-11-19。

## 版本与依赖来源

| 项目 | 升级前 | 升级后 |
| --- | --- | --- |
| Xray 内核 | 25.9.11，wyx2685 定制分支 | 官方 XTLS/Xray-core 26.11.9 |
| 实际提交 | `a74bf884128ded1c3a4da3b7e29c23924a149dac` | `676eaf66f8f05d4d558ee7a74b4736623d3feeb9` |
| Go 最低版本 | 1.25 | 1.27 |
| 推荐/发布工具链 | 1.25.0 | 1.27.4 |

官方发布：[v26.11.9](https://github.com/XTLS/Xray-core/releases/tag/v26.11.9)。
最低 Go 版本来自该 release 的 [go.mod](https://github.com/XTLS/Xray-core/blob/v26.11.9/go.mod)。

本项目把 Xray 作为 Go 库编译进 `ppnode`，升级需要重新构建并替换 `ppnode` 可执行文件。

`go.mod` 锁定的 Go 模块版本是：

```text
github.com/xtls/xray-core v1.260327.1-0.20261109052709-676eaf66f8f0
```

这个伪版本对应 **26.11.9 的 release 提交**，不是 2026 年 3 月的旧内核。官方 release 标签 `v26.11.9` 与模块路径的主版本不匹配，直接 `go get ...@v26.11.9` 会报错；使用完整提交号让 Go 解析版本：

```bash
go get github.com/xtls/xray-core@676eaf66f8f05d4d558ee7a74b4736623d3feeb9
```

已移除指向 `github.com/wyx2685/xray-core` 的旧 `replace`。sing-box 保留原先 `github.com/wyx2685/sing-box_mod v1.13.0-alpha.5` 的源码版本，改用仓库内的兼容补丁快照；共享间接依赖按新 Xray 的要求由 Go 解析。

## 代码适配

### 官方内核与自定义 dispatcher

旧 fork 相比它的官方基线只有一个补丁，涉及 `proxy/vless/inbound/inbound.go`、`common/mux/server.go` 和 `app/reverse/bridge.go`，用于避免把项目的自定义 dispatcher 强制转换为官方 `DefaultDispatcher`。

官方 26.11.9 已调整相关接口。项目保留自己的 dispatcher、用户流量计数、限速和连接管理，通过真实回环代理测试验证与官方内核的组合。

同时适配以下编译接口变化：

- `SniffingConfig.DestOverride` 改为直接使用 `StringList`，不再传指针。
- `session.SniffingRequest.ExcludeForDomain` 改用官方预编译匹配器的 `MatchAny`，并支持新的 `ExcludeForIP` 匹配器。
- FakeDNS 错误构造移除上游已删除的 `AtError()` 方法。
- 删除上游已移除的 `headers/srtp`、`headers/tls`、`headers/utp`、`headers/wechat`、`headers/wireguard` 包导入。使用自定义 Xray JSON 的用户需要检查旧传输伪装配置与新版内核的兼容性。

回归测试还发现并修复了原有连接管理遗漏：`DispatchLink` 路径没有登记活动连接，且只关闭 reader/writer 包装器不能保证断开底层 TCP 连接。现在两条分发路径都登记 TCP 连接，删除用户时关闭实际连接；批量关闭先在锁内获取快照，再在锁外关闭，避免与并发移除冲突。正常结束一个流时不会主动关闭其共享 TCP 连接。

### 双内核共享依赖

完整双内核构建额外暴露了三个兼容点：

1. 新 REALITY 库的 `Config.PrivateKey` 从 `[]byte` 改为 `*ecdh.PrivateKey`。在原 sing-box fork 快照中用 `ecdh.X25519().NewPrivateKey` 转换，保留原有的密钥格式和验证。
2. QPACK v0.6 的 `NewDecoder` / `Decode` API 与 sing-box 所用 QUIC 库不兼容。在 QPACK v0.6 快照上增加旧构造调用及 `DecodeFull` 入口，实际解码仍使用 v0.6 解析器。兼容测试覆盖重复解码和损坏输入。
3. Go 1.27 默认切换了 `golang.org/x/net/http2` 的底层实现，旧 sing-box 依赖的连接池内部符号不再存在。双内核构建加入上游支持的 `http2legacy` 标签，保留原 HTTP/2 实现及连接池关闭行为。

这两个本地模块放在 `third_party/`，保留完整来源快照及许可证，通过 `go.mod` 的版本限定替换构建。来源、精确补丁和未来移除步骤见 [third_party/README.md](../third_party/README.md)。没有修改本机 Go 模块缓存；构建不依赖临时 overlay 或构建前补丁脚本。

### Shadowsocks

- 删除对已移除的 `ShadowsocksServerConfig.IVCheck` 字段的赋值。
- 保留配置解析字段 `DisableIVCheck`，便于给旧配置返回明确错误；Shadowsocks 节点设置为 `true` 时拒绝启动，需删除该项或设为 `false`。
- 移除对已删除的 `CipherType_NONE` 枚举的引用。Shadowsocks 的 `none` / `plain` 配置返回迁移错误，需改用 AEAD 加密。这不影响 VLESS 的 `decryption: "none"`。
- 对齐官方 XChaCha20-Poly1305 的枚举映射。
- 修复 SS2022 占位用户的随机密钥：AES-128 生成 16 字节、AES-256 生成 32 字节，然后直接 Base64 编码。旧实现先转十六进制再 Base64，解码后是 64 字节，无法通过新内核的密钥长度校验。
- SS2022 多用户节点必须提供 `server_key`，支持 `2022-blake3-aes-128-gcm` 和 `2022-blake3-aes-256-gcm`。不支持多用户 ChaCha20 的组合，配置会给出明确错误。
- 保持真实用户基于 UUID 前 16/32 字节生成密钥的现有规则，避免改变面板下发的客户端凭据。

### 构建环境

- `go.mod`：最低 Go 1.27，推荐工具链 Go 1.27.4。
- `Dockerfile`：构建镜像升级为 `golang:1.27.4-alpine`。
- 发布工作流使用 `go-version-file: go.mod`，避免单独维护 Go 版本。
- 发布前新增双内核测试与 QPACK 兼容测试，测试成功后才执行发布构建。
- 保留 `GOEXPERIMENT=jsonv2`，项目的面板用户列表解析仍使用 `encoding/json/v2`。
- Docker、发布工作流和 README 统一加入双内核所需的 `http2legacy` 构建标签。
- 新增 `.dockerignore`，排除 `.git` 与本机 `output` 工具链/缓存目录，保留构建必需的 `third_party`。
- 修正 `common/serverstatus` 的动态 `fmt.Errorf(errorString)` 为 `errors.New(errorString)`，通过新工具链随 `go test` 执行的 vet 检查。

## 构建与验证方法

Linux / macOS：

```bash
export GOEXPERIMENT=jsonv2
export CGO_ENABLED=0
go mod download
go mod verify
go test -count=1 -timeout=5m -tags "xray sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor http2legacy" ./...
go test -count=1 github.com/quic-go/qpack
go test -count=1 -tags "with_utls http2legacy" github.com/sagernet/sing-box/common/tls
go build -trimpath -tags "xray sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor http2legacy" -o ppnode .
```

Windows PowerShell：

```powershell
$env:GOEXPERIMENT = 'jsonv2'
$env:CGO_ENABLED = '0'
go mod download
go mod verify
go test -count=1 -timeout=5m -tags 'xray sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor http2legacy' ./...
go test -count=1 github.com/quic-go/qpack
go test -count=1 -tags 'with_utls http2legacy' github.com/sagernet/sing-box/common/tls
go build -trimpath -tags 'xray sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor http2legacy' -o ppnode.exe .
```

需要本机代理时，可在运行 Go 命令前设置：

```powershell
$env:HTTP_PROXY = 'http://127.0.0.1:10808'
$env:HTTPS_PROXY = 'http://127.0.0.1:10808'
$env:GOPROXY = 'https://goproxy.cn,https://proxy.golang.org,direct'
```

本次 Windows 工具链通过该代理下载。官方源的部分数据段超时后使用镜像补齐，并以 Go 官方元数据中的 SHA256 校验完整压缩包：

```text
go1.27.4.windows-amd64.zip
178ab98950ac69ab25069307dc6ade79f3816e414cce95b4da166a115cde1e7e
```

工具链及下载缓存放在 Git 忽略的 `output/` 中；发布和 CI 不依赖这些本机文件。

如果把工具链或 Go 模块缓存放在仓库内部，需放入隐藏目录或具有独立 `go.mod` 的目录，防止 `go mod tidy` / `go test ./...` 将缓存和工具链自身的测试代码当作项目代码。本次 `output/go.mod` 仅用于隔离本机缓存，不参与项目依赖。

## 本次验证结果

验证工具链为 Go 1.27.4，运行主机为 Windows amd64。

| 检查 | 结果 |
| --- | --- |
| `go mod tidy` | 成功完成，更新 `go.mod` / `go.sum` |
| `go mod verify` | 全部远程模块校验通过 |
| 完整双内核标签的 `go test ./...`（包含 vet） | 通过；Xray 测试约 7 秒，既有 limiter 测试约 60 秒 |
| Xray 13 组真实 TCP 回环代理测试 | 通过：VLESS、Mux、WS、gRPC、HTTPUpgrade、XHTTP、REALITY/Vision、1 Mbps 限速、VMess、Trojan、SS AEAD、SS2022 AES-128 / AES-256 |
| 每组代理的用户生命周期与统计 | 通过：动态添加用户、数据往返、按用户统计上传/下载、统计清零、删除用户、关闭活动 TCP 连接、删除节点 |
| 5 组旧 Shadowsocks 配置迁移错误 | 通过：`none`、`plain`、禁用 IV 检查、多用户 ChaCha20、缺少服务器密钥 |
| QPACK 上游测试与新增兼容测试 | 通过 |
| sing-box TLS 包测试与新增 REALITY 私钥兼容测试 | 通过；验证现有 Base64 私钥保持一致，非法密钥被拒绝 |
| Windows amd64 完整双内核构建 | 通过；`version` 命令可执行 |
| Linux amd64 / arm64 完整双内核交叉编译 | 通过 |
| 两个依赖快照与来源文件的逐文件对照 | 仅存在声明的 2 个源码修改和 2 个新增兼容测试文件，没有缺失来源文件 |

构建产物位于 Git 忽略的目录：

```text
output/ppnode-windows-amd64.exe
output/ppnode-linux-amd64
output/ppnode-linux-arm64
```

这些是本地验证产物，PPanel 应用版本仍为默认 `TempVersion`；嵌入的 Xray 为 **26.11.9**，已在运行测试的启动日志中确认。正式发布工作流继续使用原有 `-ldflags` 注入 PPanel 发布版本。

本机因为 Git 沙箱读取限制，构建时额外使用了 `-buildvcs=false`，并使用 `-ldflags "-s -w -buildid="` 生成精简产物。该标志只关闭本地 VCS 信息写入，不改变依赖版本或内核功能。

验证范围：Linux 产物完成交叉编译，但没有在 Linux 主机上运行；Docker 镜像构建、其余发布矩阵架构、真实面板同步和公网客户端连接未在本机执行。CI 已增加测试步骤，但本次没有触发远程发布。

## 部署注意事项

升级内核不自动扩展面板的协议字段或新增传输配置界面。例如，面板生成的 VLESS 入站仍使用现有 `decryption: "none"`，本次没有新增 VLESS Encryption 或 XDRIVE 的面板接入。

新版 Freedom 对来自 VLESS、VMess、Trojan、Shadowsocks 等入站的流量默认阻止私网目的地址。回环测试通过临时自定义出站，仅放行 `127.0.0.1/32`；生产默认行为保持上游设置。如果节点需要访问内部服务，应在相应自定义 Freedom 出站的 `settings.finalRules` 中显式允许目标 IP/CIDR，并确认路由选择了该出站。

部署前保留原 `ppnode` 二进制与节点配置，先在测试节点检查真实客户端连接、面板用户同步和流量回报，再替换生产服务。回退时恢复旧二进制以及与之匹配的配置。
