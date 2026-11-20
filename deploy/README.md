# XrayR4u v0.9.17 VPS 部署

## 适用场景

| 项目 | 要求 |
|------|------|
| 系统 | Debian 12/13, Ubuntu 20.04+/22.04+/24.04+ |
| 架构 | amd64 / arm64 |
| 面板 | K2Board (UniProxy) |
| 协议 | VLESS + REALITY / TLS + XHTTP + CDN / AnyTLS + TLS / Hysteria2 + TLS |
| 核心 | Xray-core **v26.11.9**。v0.9.17 发布二进制与源码一致。v0.9.16 二进制仍是 v26.7.11 |

## 一行命令

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

1. 面板地址  
2. API Token  
3. 节点 ID  
4. 协议：`[1]` VLESS + REALITY / `[2]` AnyTLS + TLS / `[3]` VLESS + AnyTLS / `[4]` Hysteria2 + TLS / `[5]` VLESS + Hysteria2

`[1]` 到 `[4]` 的配置和以前一样。五个选项都可以下载 Release **v0.9.17**。

`[4]` 和 `[5]` 只适合新机器。`[5]` 同时写入一条 VLESS 和一条 Hysteria2，面板上要先建好两个 NodeID。已经在跑 VLESS 或 AnyTLS 的机器不要选这两项，安装会整份覆盖 `config.yml`，脚本发现旧节点后会退出。追加方式见 [HYSTERIA2.md](HYSTERIA2.md)。Hysteria2 听 UDP，脚本会放行 UDP 443；面板端口不是 443 时要另开。

> 离线：下载 Release 的 `XrayR-linux-amd64` 重命名为 `XrayR`，与 `install.sh` 同目录后执行。不要用 v0.9.16 的 Release 装 Hysteria2。

### 本机交叉编译（linux amd64）

```bash
cd /path/to/XrayR4u
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -buildid=" -o XrayR-linux-amd64 ./main
```

### 面板升级后只更新节点

1. 确认 **K2Board 面板**已支持 Header 鉴权与（如需审计）`/rule`、`/illegal`  
2. 上传新二进制并重启 `xrayr`  
3. **不必**改订阅链接；`config.yml` 的 `ApiKey` 字段名不变（运行时改走 Header）  

对接细节：[K2BOARD_INTEGRATION.md](K2BOARD_INTEGRATION.md) §2。

## 目录

```
deploy/
├── install.sh                 # 一键安装（主程序）
├── install-warp.sh            # 可选：WARP 解锁 AI/流媒体（独立）
├── WARP_README.md             # WARP (wgcf + WireGuard 出站) 说明
├── README.md                  # 本文件
├── K2BOARD_INTEGRATION.md     # K2Board 对接参考（含 rule/illegal）
├── HYSTERIA2.md               # Hysteria2 节点：配置、安装脚本、上线与回滚
└── TLS_XHTTP_CDN_PQ.md        # CDN / XHTTP / 后量子运维
```

## 安装后默认

VLESS 节点配置会写入：

- `EnableReality: true`（面板下发 REALITY）
- `RealityMinClientVer: "1.8.0"`（兼容 Mihomo 等；xray-core ≥26.7.11）

CDN / 后量子等进阶项见 [TLS_XHTTP_CDN_PQ.md](TLS_XHTTP_CDN_PQ.md) 与 [K2BOARD_INTEGRATION.md](K2BOARD_INTEGRATION.md)。

选 `[4]` 时，Hysteria2 节点会写入：

- `NodeType: hysteria2`
- `EnableReality: false`
- `CertConfig.ALPN: ["h3"]`
- 证书为 `file` 或 `http`

监听的是面板端口上的 **UDP**。说明见 [HYSTERIA2.md](HYSTERIA2.md)。

## 可选：WARP 解锁 AI / 流媒体

主程序装好后，可**单独**配置 Cloudflare WARP（wgcf + Xray WireGuard 出站），不改系统路由、不装官方 warp-cli：

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install-warp.sh) --install
```

脚本会备份并合并 `/etc/XrayR/config/custom_outbound.json` 与 `route.json`，将 ChatGPT / Claude / Netflix 等走 `warp` 出站。详见 [WARP_README.md](WARP_README.md)。

## 密钥生成（安装脚本可选 · 节点自带 tools）

安装过程中可选择生成；也可随时：

```bash
XrayR tools x25519       # REALITY PrivateKey / PublicKey
XrayR tools mldsa65      # ML-DSA Seed + Verify
XrayR tools vlessenc     # VLESS Encryption 成对串
XrayR tools help
```

结果可保存到 `/etc/XrayR/keys/`（脚本选「全部生成」时自动写入）。

## 管理

```bash
systemctl status xrayr
journalctl -u xrayr -f
ss -tlnp | grep XrayR
vi /etc/XrayR/config.yml && systemctl restart xrayr
```

卸载：`bash install.sh --uninstall`

## 故障速查

| 现象 | 处理 |
|------|------|
| REALITY 第三方客户端全挂 | 确认 `RealityMinClientVer` / 面板 `min_client_ver` 为 `1.8.0` |
| 启动失败 | `journalctl -u xrayr -n 50` |
| 面板 API | `curl -sS -H "Authorization: Bearer TOKEN" -H "X-Api-Key: TOKEN" "https://面板/api/v1/server/UniProxy/config?node_id=1&node_type=vless&local_port=1"` |
| 审计规则 | `curl -sS -H "Authorization: Bearer TOKEN" -H "X-Api-Key: TOKEN" "https://面板/api/v1/server/UniProxy/rule?node_id=1&node_type=vless"` |

## 链接

- 仓库：https://github.com/HenZenKuriRIP/XrayR4u  
- 安装详解：[../INSTALL.md](../INSTALL.md)  
- 架构：[../ARCHITECTURE.md](../ARCHITECTURE.md)  
