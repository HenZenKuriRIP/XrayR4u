# XrayR4u

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MPL%202.0-blue.svg)](LICENSE)
[![Xray-core](https://img.shields.io/badge/Xray--core-v26.7.11-red.svg)](https://github.com/XTLS/Xray-core)
[![Release](https://img.shields.io/badge/release-v0.9.14-brightgreen.svg)](https://github.com/HenZenKuriRIP/XrayR4u/releases)

XrayR4u 是 [XrayR](https://github.com/XrayR-project/XrayR) 的社区维护分支：Go 语言后端代理管理框架，作为 **K2Board 面板** 与 **Xray-core** 之间的桥梁。

> **XrayR4u = XrayR for you** — 开箱即用，持续更新。

## 免责声明

本项目仅供个人学习研究使用，不保证任何可用性，也不对使用本软件造成的任何后果负责。

## 核心特性

| 特性 | VLESS + REALITY | VLESS + TLS + XHTTP (CDN) | AnyTLS + TLS |
|------|:---:|:---:|:---:|
| 节点 / 用户 / 流量同步 | ✅ | ✅ | ✅ |
| 在线人数 & IP 限制 | ✅ | ✅ | ✅ |
| 审计规则 / 限速 | ✅ | ✅ | ✅ |
| REALITY 无证书防探测 | ✅ | — | — |
| XTLS Vision | ✅ | 可选 | — |
| 过 CDN（XHTTP） | — | ✅ | — |
| 后量子（ML-DSA / VLESS Encryption / TLS 曲线） | ✅ 可选 | ✅ 可选 | — |
| 域名 TLS 证书 | — | ✅ | ✅ |
| 內建多路复用 / padding | — | — | ✅ |
| 抗主动探测 Fallback | — | — | ✅ |

- 內嵌 **Xray-core v26.7.11**，无需单独安装核心
- 单实例多面板 / 多节点
- 配置热重载
- 对接 **K2Board UniProxy**（详见 [deploy/K2BOARD_INTEGRATION.md](deploy/K2BOARD_INTEGRATION.md)）

## 支持的前端面板

| 面板 | 协议 | 接口 |
|------|------|------|
| [K2Board](https://github.com/RIPHZK1998/K2board) | VLESS REALITY / TLS+XHTTP / AnyTLS | UniProxy |

## 版本信息

| 项目 | 版本 |
|------|------|
| XrayR4u | **v0.9.14** |
| Xray-core | **v26.7.11** |
| Go | 1.26+ |

## 密钥 / 后量子材料生成（节点自带）

无需单独安装 `xray`，使用与内嵌 core **同版本** 的 tools：

```bash
XrayR tools x25519      # REALITY 密钥对
XrayR tools mldsa65     # ML-DSA：Seed → 服务端，Verify → 订阅
XrayR tools vlessenc    # Encryption：成对 decryption + encryption
XrayR tools help
```

短写：`XrayR x25519` / `XrayR mldsa65` / `XrayR vlessenc` 亦可。  
一键安装脚本在部署末尾可选择生成并保存到 `/etc/XrayR/keys/`。

## 快速安装

### 一键脚本（推荐）

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

交互输入面板地址、Token、节点 ID，选择协议后自动安装。

### 从 Release 安装二进制

```bash
# 示例：linux amd64
curl -sSL -o XrayR https://github.com/HenZenKuriRIP/XrayR4u/releases/download/v0.9.14/XrayR-linux-amd64
chmod +x XrayR
# 与 install.sh 同目录后执行 bash install.sh（脚本优先用本地二进制）
```

### 源码编译

```bash
git clone https://github.com/HenZenKuriRIP/XrayR4u.git
cd XrayR4u
go mod download
CGO_ENABLED=0 go build -v -o XrayR -trimpath -ldflags "-s -w -buildid=" ./main
```

## 文档

| 文档 | 说明 |
|------|------|
| [INSTALL.md](INSTALL.md) | 安装与配置详解 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 代码架构 |
| [deploy/README.md](deploy/README.md) | VPS 部署与脚本 |
| [deploy/K2BOARD_INTEGRATION.md](deploy/K2BOARD_INTEGRATION.md) | **K2Board 对接参考** |
| [deploy/TLS_XHTTP_CDN_PQ.md](deploy/TLS_XHTTP_CDN_PQ.md) | CDN / XHTTP / 后量子节点运维 |

## 项目结构

```
XrayR4u/
├── main/                 # 入口与示例配置
├── panel/                # 面板生命周期
├── api/v2board/          # UniProxy 适配
├── service/controller/   # 入站构建 / 用户同步 / REALITY·XHTTP·PQ
├── app/mydispatcher/     # 限速 / 统计 / 审计
├── proxy/anytls/         # AnyTLS 协议
├── deploy/               # 安装脚本与对接文档
└── vendor/               # 依赖（含 xray-core）
```

## 管理命令

```bash
systemctl status xrayr
journalctl -u xrayr -f
vi /etc/XrayR/config.yml
systemctl restart xrayr
```

## License

[Mozilla Public License Version 2.0](LICENSE)
