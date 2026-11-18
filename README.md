# PPanel-node

A PPanel node server based on multi core, modified from V2bX.  
一个基于多种内核的PPanel节点服务端，修改自V2bX，支持V2ay,Trojan,Shadowsocks,Tuic,Hysteria协议。

## 软件安装

### 一键安装

```
wget -N https://raw.githubusercontent.com/wyx2685/ppanel-node/master/Scripts/install.sh && bash install.sh
```

## 构建

使用 Go 1.27.4（最低 Go 1.27），并启用项目使用的 `jsonv2` 实验。

``` bash
# 通过-tags选项指定要编译的内核， 可选 xray，sing
GOEXPERIMENT=jsonv2 go build -v -o ./node -tags "xray sing with_quic with_grpc with_utls with_wireguard with_acme http2legacy" -trimpath -ldflags "-s -w -buildid="
```

双内核构建需要 `http2legacy` 标签，以兼容现有 sing-box 的 HTTP/2 连接池实现。Xray 26.11.9 的依赖版本、配置迁移和验证记录见 [升级说明](docs/xray-26.11.9-upgrade.md)。

