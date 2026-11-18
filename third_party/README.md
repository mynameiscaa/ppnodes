# 双内核共享依赖兼容补丁

本目录保留两个完整 Go 模块的源码快照和原始许可证，通过根目录 `go.mod` 的版本限定 `replace` 使用，确保普通 `go build`、Docker 和 CI 都能复现。没有改写 Go 模块缓存，也不需要构建前下载未固定的分支或执行补丁脚本。

| 目录 | 原始来源 | 本地修改 |
| --- | --- | --- |
| `sing-box` | [`github.com/wyx2685/sing-box_mod v1.13.0-alpha.5`](https://github.com/wyx2685/sing-box_mod/tree/v1.13.0-alpha.5)，提交 `cbf07b5800ae913f87c701e89abd4297854dfe0c` | `common/tls/reality_server.go`：将解码后的 X25519 私钥通过 `crypto/ecdh` 转换为新 REALITY 库要求的 `*ecdh.PrivateKey`；新增 `common/tls/reality_compat_test.go`。 |
| `qpack` | [`github.com/quic-go/qpack v0.6.0`](https://github.com/quic-go/qpack/tree/v0.6.0) | `decoder.go` 增加旧构造调用和 `DecodeFull` 的兼容入口，内部仍使用 v0.6 的解析器；新增 `compat_test.go`。 |

为什么需要：最新 Xray 引入的 `github.com/xtls/reality` 和 QPACK v0.6 会被 Go 的最小版本选择用于整个程序。当前 sing-box fork 仍使用旧的 REALITY 私钥字段和 QPACK v0.5 解码接口，直接升级 Xray 会导致完整双内核构建失败。这里保留 sing-box 原有行为和面板扩展，仅适配共享接口。

QPACK 兼容范围为本项目旧 QUIC 实际使用的 `NewDecoder(func(HeaderField))` 和 `DecodeFull`；旧构造器提供的回调不会由 `DecodeFull` 调用，与旧版该方法的行为一致。本补丁不恢复已经删除的流式 `Write`/`Close` API。

许可证：sing-box 使用其原始 `LICENSE`；QPACK 使用其原始 `LICENSE.md`。上游其他文件保持原样。

升级或移除补丁时：

1. 对照上述版本，检查本地修改与上游差异。
2. 如果新的 sing-box/QUIC 依赖已同时兼容新版 REALITY 和 QPACK，则恢复远程版本依赖，移除对应本地替换。
3. 运行根模块的完整标签测试与构建，并单独运行 `go test github.com/quic-go/qpack` 和 `go test -tags "with_utls http2legacy" github.com/sagernet/sing-box/common/tls`。根模块的 `./...` 不会自动遍历这两个嵌套模块。

Go 1.27 的完整双内核构建还需要 `http2legacy` 标签，以保留当前 sing-box 使用的 HTTP/2 连接池实现。移除该标签前需同时适配 sing-box 的连接池关闭逻辑。

完整升级记录见 [Xray 26.11.9 升级说明](../docs/xray-26.11.9-upgrade.md)。
