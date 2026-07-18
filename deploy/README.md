# XrayR4u v0.9.14 VPS 部署

## 适用场景

| 项目 | 要求 |
|------|------|
| 系统 | Debian 12/13, Ubuntu 20.04+/22.04+/24.04+ |
| 架构 | amd64 / arm64 |
| 面板 | K2Board (UniProxy) |
| 协议 | VLESS + REALITY / TLS + XHTTP + CDN / AnyTLS + TLS |
| 核心 | Xray-core **v26.7.11**（內嵌） |

## 一行命令

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

1. 面板地址  
2. API Token  
3. 节点 ID  
4. 协议：VLESS + REALITY / AnyTLS + TLS / 双协议  

> 离线：本机编译或下载 Release 的 `XrayR-linux-amd64` 重命名为 `XrayR`，与 `install.sh` 同目录后执行。

## 目录

```
deploy/
├── install.sh                 # 一键安装
├── README.md                  # 本文件
├── K2BOARD_INTEGRATION.md     # K2Board 对接参考
└── TLS_XHTTP_CDN_PQ.md        # CDN / XHTTP / 后量子运维
```

## 安装后默认

VLESS 节点配置会写入：

- `EnableReality: true`（面板下发 REALITY）
- `RealityMinClientVer: "1.8.0"`（兼容 Mihomo 等；xray-core ≥26.7.11）

CDN / 后量子等进阶项见 [TLS_XHTTP_CDN_PQ.md](TLS_XHTTP_CDN_PQ.md) 与 [K2BOARD_INTEGRATION.md](K2BOARD_INTEGRATION.md)。

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
| 面板 API | `curl -s "https://面板/api/v1/server/UniProxy/config?node_id=1&token=TOKEN&node_type=vless&local_port=1"` |

## 链接

- 仓库：https://github.com/HenZenKuriRIP/XrayR4u  
- 安装详解：[../INSTALL.md](../INSTALL.md)  
- 架构：[../ARCHITECTURE.md](../ARCHITECTURE.md)  
