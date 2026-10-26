# Cloudflare WARP 解锁（wgcf + Xray WireGuard 出站）

独立方案：在 **已用 `install.sh` 部署好的 XrayR** 上，增量配置 WARP，把 AI / 流媒体流量经 Cloudflare 解锁。

**不改动** 主安装脚本 `install.sh`，**不安装** 官方 `warp-cli`，**不修改** 系统默认路由（SSH 安全）。

## 原理

```
用户 → XrayR 入站 → 路由匹配 AI/流媒体域名
                         ↓
              custom_outbound 中 tag=warp
              （Xray 内嵌 WireGuard 客户端）
                         ↓
              Cloudflare WARP 出口 IP
```

| 组件 | 作用 |
|------|------|
| [wgcf](https://github.com/ViRb3/wgcf) | 注册免费 WARP 账号，生成 WireGuard 配置 |
| Xray `wireguard` 出站 | 以用户态 gVisor 栈拨 WARP（`noKernelTun: true`） |
| `route.json` | 将 ChatGPT / Claude / Netflix 等域名指向 `warp` |
| `custom_outbound.json` | 增加 `tag: warp` 的 WireGuard 出站 |

仅路由命中的流量走 WARP，其余仍走默认 `IPv4_out` / `IPv6_out`。

## 前置条件

1. 已用主脚本部署 XrayR，且存在：
   - `/etc/XrayR/config.yml`
   - `OutboundConfigPath` → `/etc/XrayR/config/custom_outbound.json`
   - `RouteConfigPath` → `/etc/XrayR/config/route.json`
2. 二进制需 **已编译注册 wireguard 出站**（本仓库 `main/distro/all` 已 `_ "…/proxy/wireguard"`）。  
   旧 Release 若启动后报 `unknown config id: wireguard`，请升级到包含本改动的版本。
3. 建议节点可访问 `engage.cloudflareclient.com:2408`（UDP）与 GitHub（下载 wgcf）。

## 一键安装

```bash
# 推荐：从仓库拉取后以 root 执行
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install-warp.sh) --install
```

或交互菜单：

```bash
bash install-warp.sh
```

| 参数 | 说明 |
|------|------|
| （无） | 交互菜单 |
| `--install` | 非交互安装 / 更新配置 |
| `--regenerate` | 强制重新 `wgcf register` 并写配置 |
| `--uninstall` | 从 route/outbound 移除 warp（可选删账号目录） |
| `--status` | 查看状态 |
| `--help` | 用法 |

环境变量：

| 变量 | 默认 | 说明 |
|------|------|------|
| `XRAYR_DIR` | `/etc/XrayR` | 安装根目录 |
| `WGCF_VERSION` | `v2.2.26` | wgcf 版本 |

## 会修改哪些文件

| 路径 | 变更 |
|------|------|
| `/usr/local/bin/wgcf` | 安装 CLI（若不存在） |
| `/etc/XrayR/warp/wgcf-account.toml` | WARP 账号（私钥，勿泄露） |
| `/etc/XrayR/warp/wgcf-profile.conf` | WireGuard 配置 |
| `/etc/XrayR/config/custom_outbound.json` | 合并 `tag: warp` 出站；写前 `.bak.时间戳` |
| `/etc/XrayR/config/route.json` | 插入 AI/流媒体 → `warp`；去掉与旧模板冲突的 `netflix→IPv6_out` |
| `systemctl restart xrayr` | 安装/卸载后可选或自动重启 |

**不会** 改写 `config.yml` 里的节点/面板配置，**不会** 动内核路由表。

## 出站示例（由脚本生成）

```json
{
    "tag": "warp",
    "protocol": "wireguard",
    "settings": {
        "secretKey": "<from wgcf PrivateKey>",
        "address": ["172.16.0.2/32", "2606:4700:110:…/128"],
        "peers": [{
            "publicKey": "<Cloudflare peer>",
            "endpoint": "engage.cloudflareclient.com:2408",
            "keepAlive": 25,
            "allowedIPs": ["0.0.0.0/0", "::/0"]
        }],
        "mtu": 1280,
        "reserved": [/* client_id 前 3 字节，若有 */],
        "domainStrategy": "ForceIPv4",
        "noKernelTun": true
    }
}
```

`noKernelTun: true`：不创建系统 TUN，不抢默认网关，适合多用户代理节点。

## 默认路由域名（可改脚本内 `WARP_DOMAINS_DEFAULT`）

- AI：`geosite:openai` / `anthropic` / `category-ai-!cn`，以及 `chatgpt.com`、`claude.ai` 等 domain 兜底  
- 流媒体：`netflix`、`disney`、`hulu`、`hbo`、`primevideo`、`youtube`、`spotify`、`tiktok` 等  

节点需带较新的 `geosite.dat`（主程序自带或可自备 Loyalsoldier）。未知标签会被忽略或导致规则失效，domain 兜底用于关键站点。

## 验证

```bash
bash install-warp.sh --status

# 配置是否写入
grep -n warp /etc/XrayR/config/custom_outbound.json
grep -n warp /etc/XrayR/config/route.json

# 服务
systemctl status xrayr
journalctl -u xrayr -n 50 --no-pager

# 客户端侧：连上本节点后访问
curl -sS https://www.cloudflare.com/cdn-cgi/trace | grep warp
# 期望 warp=on 或 warp=plus（表示经 WARP 出口）
```

节点本机直接 `curl` **不会** 走 Xray 路由，需经代理客户端再测。

## 卸载

```bash
bash install-warp.sh --uninstall
```

仅从 `route.json` / `custom_outbound.json` 去掉 warp；交互时可选删除 `/etc/XrayR/warp`。

## 故障排查

| 现象 | 处理 |
|------|------|
| `unknown config id: wireguard` | 二进制未注册 wireguard，升级到含本仓库 distro 改动的版本并重装二进制 |
| xrayr 启动失败 | `journalctl -u xrayr -n 80`；用 `*.bak.*` 还原 route/outbound |
| `wgcf register` 失败 | 检查出网、时钟；可手动 `cd /etc/XrayR/warp && wgcf register && wgcf generate` |
| AI 仍未解锁 | 确认路由规则在 block 之后、默认出站之前；更新 geosite；客户端 DNS/分流勿绕开节点 |
| 部分流媒体仍失败 | WARP 免费出口地区/风控限制，可尝试 `--regenerate` 换账号，或改用 WARP+ License（自行编辑 account 后 regenerate） |
| UDP 2408 被墙 | 节点所在网络需能访问 CF WARP 端点 |

## 与主安装的关系

```
deploy/install.sh          # 主程序：面板、节点、systemd、默认 route/outbound
deploy/install-warp.sh     # 可选：WARP 解锁（本方案）
```

先装主程序，再按需装 WARP。卸载 WARP 不影响主程序；卸载主程序时请自行清理 `/etc/XrayR/warp` 与 wgcf。

## 安全注意

- `wgcf-account.toml` 含账号密钥，权限建议 `600`，勿提交到 Git。  
- 本方案不提升面板鉴权；仅改变部分出站路径。  
- 合规使用 Cloudflare 服务条款与当地法律。
