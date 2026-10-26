#!/usr/bin/env bash
# ============================================================================
# XrayR4u — Cloudflare WARP (wgcf + Xray WireGuard 出站) 独立安装脚本
#
# 不改动主安装脚本 install.sh；仅在已部署 XrayR 的机器上增量配置：
#   - wgcf 注册并生成 WireGuard 配置（非官方 warp-cli，资源占用极低）
#   - 写入 /etc/XrayR/config/custom_outbound.json 增加 warp 出站
#   - 写入 /etc/XrayR/config/route.json 将 AI / 流媒体走 warp
#   - 可选：systemctl restart xrayr
#
# 用法:
#   bash install-warp.sh              # 交互安装
#   bash install-warp.sh --install    # 非交互安装
#   bash install-warp.sh --uninstall  # 移除 warp 出站与路由规则
#   bash install-warp.sh --status     # 查看状态
#   bash install-warp.sh --regenerate # 重新 register/generate 后刷新配置
# ============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[✓]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[!]${NC}  $*"; }
log_error() { echo -e "${RED}[✗]${NC} $*"; exit 1; }
log_step()  { echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${BOLD}${CYAN}  $*${NC}"; echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"; }

XRAYR_DIR="${XRAYR_DIR:-/etc/XrayR}"
CONFIG_DIR="${XRAYR_DIR}/config"
WARP_DIR="${XRAYR_DIR}/warp"
OUTBOUND_FILE="${CONFIG_DIR}/custom_outbound.json"
ROUTE_FILE="${CONFIG_DIR}/route.json"
CONFIG_YML="${XRAYR_DIR}/config.yml"
WGCF_BIN="/usr/local/bin/wgcf"
WGCF_VERSION="${WGCF_VERSION:-v2.2.26}"
MARKER="xrayr4u-warp"   # 用于识别本脚本管理的 outbound tag / 规则

# AI + 流媒体（Loyalsoldier geosite 常见标签；不存在的规则会被 Xray 忽略或需自备 dat）
WARP_DOMAINS_DEFAULT=(
  "geosite:openai"
  "geosite:anthropic"
  "geosite:netflix"
  "geosite:disney"
  "geosite:hulu"
  "geosite:hbo"
  "geosite:primevideo"
  "geosite:youtube"
  "geosite:spotify"
  "geosite:tiktok"
  "geosite:twitter"
  "geosite:facebook"
  "geosite:instagram"
  "geosite:category-ai-chat-!cn"
  "geosite:category-ai-!cn"
  "domain:chatgpt.com"
  "domain:openai.com"
  "domain:claude.ai"
  "domain:anthropic.com"
  "domain:gemini.google.com"
  "domain:googleapis.com"
  "domain:gstatic.com"
  "domain:netflix.com"
  "domain:nflxvideo.net"
  "domain:disneyplus.com"
  "domain:dssott.com"
)

need_root() {
    if [[ "$(id -u)" -ne 0 ]]; then
        log_error "请使用 root 运行（修改 /etc/XrayR 并可能安装依赖）"
    fi
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) log_error "不支持的架构: $(uname -m)" ;;
    esac
}

ensure_deps() {
    local miss=()
    command -v curl >/dev/null || miss+=(curl)
    command -v python3 >/dev/null || miss+=(python3)
    if [[ ${#miss[@]} -gt 0 ]]; then
        log_info "安装依赖: ${miss[*]}"
        if command -v apt-get >/dev/null; then
            apt-get update -qq
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${miss[@]}"
        elif command -v yum >/dev/null; then
            yum install -y -q "${miss[@]}"
        else
            log_error "请手动安装: ${miss[*]}"
        fi
    fi
}

check_xrayr() {
    if [[ ! -f "$CONFIG_YML" ]]; then
        log_error "未找到 ${CONFIG_YML}。请先用 deploy/install.sh 完成 XrayR 主程序部署。"
    fi
    if ! grep -qE 'OutboundConfigPath:.*custom_outbound' "$CONFIG_YML" 2>/dev/null; then
        log_warn "config.yml 中可能未配置 OutboundConfigPath，请确认指向 ${OUTBOUND_FILE}"
    fi
    if ! grep -qE 'RouteConfigPath:.*route' "$CONFIG_YML" 2>/dev/null; then
        log_warn "config.yml 中可能未配置 RouteConfigPath，请确认指向 ${ROUTE_FILE}"
    fi
    mkdir -p "$CONFIG_DIR" "$WARP_DIR"
}

install_wgcf() {
    if [[ -x "$WGCF_BIN" ]]; then
        log_info "wgcf 已存在: $WGCF_BIN"
        return
    fi
    local arch
    arch="$(detect_arch)"
    local asset="wgcf_${WGCF_VERSION#v}_linux_${arch}"
    # release asset naming: wgcf_2.2.26_linux_amd64
    asset="wgcf_${WGCF_VERSION#v}_linux_${arch}"
    local url="https://github.com/ViRb3/wgcf/releases/download/${WGCF_VERSION}/${asset}"
    log_info "下载 wgcf ${WGCF_VERSION} (${arch})..."
    local tmp
    tmp="$(mktemp)"
    if ! curl -fsSL -o "$tmp" "$url"; then
        # try without v strip alternate
        url="https://github.com/ViRb3/wgcf/releases/download/${WGCF_VERSION}/wgcf_${WGCF_VERSION#v}_linux_${arch}"
        curl -fsSL -o "$tmp" "$url" || log_error "下载 wgcf 失败: $url"
    fi
    install -m 755 "$tmp" "$WGCF_BIN"
    rm -f "$tmp"
    log_info "已安装 $WGCF_BIN"
}

wgcf_register_generate() {
    cd "$WARP_DIR"
    if [[ ! -f wgcf-account.toml ]] || [[ "${1:-}" == "force" ]]; then
        log_info "wgcf register ..."
        # non-interactive accept TOS if prompted
        yes | "$WGCF_BIN" register 2>/dev/null || "$WGCF_BIN" register || true
        if [[ ! -f wgcf-account.toml ]]; then
            log_error "wgcf register 失败，未生成 wgcf-account.toml"
        fi
        log_info "已生成 wgcf-account.toml"
    else
        log_info "复用已有 wgcf-account.toml（使用 --regenerate 可强制重注册）"
    fi
    log_info "wgcf generate ..."
    "$WGCF_BIN" generate
    if [[ ! -f wgcf-profile.conf ]]; then
        log_error "wgcf generate 失败，未生成 wgcf-profile.conf"
    fi
    log_info "已生成 ${WARP_DIR}/wgcf-profile.conf"
}

# Parse wgcf-profile.conf (+ optional account client_id → reserved) → JSON
parse_wgcf_profile() {
    local conf="${WARP_DIR}/wgcf-profile.conf"
    local account="${WARP_DIR}/wgcf-account.toml"
    [[ -f "$conf" ]] || log_error "缺少 $conf"
    python3 - "$conf" "$account" <<'PY'
import sys, re, json, base64
from pathlib import Path
path, account = sys.argv[1], sys.argv[2]
text = open(path, encoding="utf-8", errors="ignore").read()

def grab(key, src=text):
    m = re.search(rf"^{re.escape(key)}\s*=\s*(.+)$", src, re.M)
    return m.group(1).strip().strip('"').strip("'") if m else ""

addrs = re.findall(r"^Address\s*=\s*(.+)$", text, re.M)
priv = grab("PrivateKey")
pub = grab("PublicKey")
endpoint = grab("Endpoint") or "engage.cloudflareclient.com:2408"
mtu = grab("MTU") or "1280"
if not priv or not pub:
    sys.exit("PrivateKey/PublicKey missing in profile")

# WARP client_id → 3-byte reserved (required by some CF edges)
reserved = []
if Path(account).is_file():
    acc = open(account, encoding="utf-8", errors="ignore").read()
    cid = grab("client_id", acc) or grab("device_id", acc)
    if cid:
        try:
            # client_id is often base64; first 3 decoded bytes = reserved
            raw = base64.b64decode(cid + "==")
            if len(raw) >= 3:
                reserved = list(raw[:3])
        except Exception:
            reserved = []

print(json.dumps({
    "secretKey": priv,
    "address": [a.strip() for a in addrs] or ["172.16.0.2/32"],
    "publicKey": pub,
    "endpoint": endpoint,
    "mtu": int(mtu) if mtu.isdigit() else 1280,
    "reserved": reserved,
}))
PY
}

backup_file() {
    local f=$1
    if [[ -f "$f" ]]; then
        local bak="${f}.bak.$(date +%Y%m%d%H%M%S)"
        cp -a "$f" "$bak"
        log_info "已备份: $bak"
    fi
}

write_outbound() {
    local meta
    meta="$(parse_wgcf_profile)"
    backup_file "$OUTBOUND_FILE"
    python3 - "$OUTBOUND_FILE" "$meta" "$MARKER" <<'PY'
import json, sys
path, meta_s, marker = sys.argv[1], sys.argv[2], sys.argv[3]
meta = json.loads(meta_s)
try:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
except FileNotFoundError:
    data = []
if not isinstance(data, list):
    sys.exit("custom_outbound.json must be a JSON array")

# drop previous warp outbound managed by us (tag warp or marker in email)
out = []
for item in data:
    tag = item.get("tag") or ""
    if tag == "warp" or tag == marker:
        continue
    out.append(item)

settings = {
    "secretKey": meta["secretKey"],
    "address": meta["address"],
    "peers": [{
        "publicKey": meta["publicKey"],
        "endpoint": meta["endpoint"],
        "keepAlive": 25,
        "allowedIPs": ["0.0.0.0/0", "::/0"],
    }],
    "mtu": meta["mtu"],
    "domainStrategy": "ForceIPv4",
    "noKernelTun": True,
}
if meta.get("reserved"):
    settings["reserved"] = meta["reserved"]
warp = {
    "tag": "warp",
    "protocol": "wireguard",
    "settings": settings,
}
# ensure baseline freedom/block if empty
if not out:
    out = [
        {"tag": "IPv4_out", "protocol": "freedom"},
        {"tag": "IPv6_out", "protocol": "freedom", "settings": {"domainStrategy": "UseIPv6"}},
        {"protocol": "blackhole", "tag": "block"},
    ]
out.append(warp)
with open(path, "w", encoding="utf-8") as f:
    json.dump(out, f, indent=4, ensure_ascii=False)
    f.write("\n")
print(path)
PY
    log_info "已更新出站: $OUTBOUND_FILE (tag=warp)"
}

write_route() {
    backup_file "$ROUTE_FILE"
    local domains_json
    domains_json="$(printf '%s\n' "${WARP_DOMAINS_DEFAULT[@]}" | python3 -c 'import json,sys; print(json.dumps([l.strip() for l in sys.stdin if l.strip()]))')"
    python3 - "$ROUTE_FILE" "$domains_json" <<'PY'
import json, sys
path, domains = sys.argv[1], json.loads(sys.argv[2])
try:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
except FileNotFoundError:
    data = {"domainStrategy": "AsIs", "rules": []}

if "rules" not in data or not isinstance(data["rules"], list):
    data["rules"] = []

# Remove previous WARP-managed rules (outboundTag warp)
data["rules"] = [
    r for r in data["rules"]
    if r.get("outboundTag") != "warp"
]

# Prefer warp over later freedom / IPv6_out rules; keep block rules first
warp_rule = {
    "type": "field",
    "outboundTag": "warp",
    "domain": domains,
}

# Drop conflicting geosite:netflix → IPv6_out from older templates
cleaned = []
for rule in data["rules"]:
    if rule.get("outboundTag") == "IPv6_out" and "domain" in rule:
        rule = dict(rule)
        rule["domain"] = [d for d in (rule.get("domain") or []) if d != "geosite:netflix"]
        if not rule["domain"] and not rule.get("ip") and not rule.get("protocol"):
            continue
    cleaned.append(rule)
data["rules"] = cleaned

# Insert after leading block rules (private / bittorrent)
insert_at = 0
for i, rule in enumerate(data["rules"]):
    if rule.get("outboundTag") == "block":
        insert_at = i + 1
    else:
        break
data["rules"].insert(insert_at, warp_rule)

with open(path, "w", encoding="utf-8") as f:
    json.dump(data, f, indent=4, ensure_ascii=False)
    f.write("\n")
print(path)
PY
    log_info "已更新路由: $ROUTE_FILE (AI/流媒体 → warp)"
}

remove_warp_from_configs() {
    backup_file "$OUTBOUND_FILE"
    backup_file "$ROUTE_FILE"
    python3 - "$OUTBOUND_FILE" <<'PY'
import json, sys
path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
except FileNotFoundError:
    sys.exit(0)
if isinstance(data, list):
    data = [x for x in data if x.get("tag") != "warp"]
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=4, ensure_ascii=False)
        f.write("\n")
PY
    python3 - "$ROUTE_FILE" <<'PY'
import json, sys
path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
except FileNotFoundError:
    sys.exit(0)
rules = data.get("rules") or []
data["rules"] = [r for r in rules if r.get("outboundTag") != "warp"]
with open(path, "w", encoding="utf-8") as f:
    json.dump(data, f, indent=4, ensure_ascii=False)
    f.write("\n")
PY
    log_info "已从 route/outbound 移除 warp"
}

restart_xrayr() {
    if systemctl is-active --quiet xrayr 2>/dev/null; then
        log_info "重启 xrayr 以加载新配置..."
        systemctl restart xrayr
        sleep 2
        if systemctl is-active --quiet xrayr; then
            log_info "xrayr 已运行"
        else
            log_warn "xrayr 重启失败，请检查: journalctl -u xrayr -n 50"
            log_warn "可还原备份: ls -lt ${CONFIG_DIR}/*.bak.*"
        fi
    else
        log_warn "xrayr 服务未运行，请手动: systemctl start xrayr"
    fi
}

show_status() {
    echo -e "${BOLD}WARP / WireGuard 配置状态${NC}"
    echo "  XrayR 目录:   $XRAYR_DIR"
    echo "  wgcf 配置:    $WARP_DIR"
    [[ -f "$WARP_DIR/wgcf-profile.conf" ]] && echo -e "  profile:      ${GREEN}存在${NC}" || echo -e "  profile:      ${RED}无${NC}"
    [[ -f "$OUTBOUND_FILE" ]] && grep -q '"tag": "warp"' "$OUTBOUND_FILE" 2>/dev/null && echo -e "  outbound:     ${GREEN}warp 已配置${NC}" || echo -e "  outbound:     ${YELLOW}无 warp 标签${NC}"
    [[ -f "$ROUTE_FILE" ]] && grep -q '"outboundTag": "warp"' "$ROUTE_FILE" 2>/dev/null && echo -e "  route:        ${GREEN}已有 warp 规则${NC}" || echo -e "  route:        ${YELLOW}无 warp 规则${NC}"
    if systemctl is-active --quiet xrayr 2>/dev/null; then
        echo -e "  xrayr:        ${GREEN}running${NC}"
    else
        echo -e "  xrayr:        ${RED}not running${NC}"
    fi
    echo ""
    echo "验证（在节点上）:"
    echo "  # 若本机默认路由未走 WARP，需通过 Xray 代理后访问:"
    echo "  curl -sS https://www.cloudflare.com/cdn-cgi/trace | grep warp"
    echo "  # 期望 warp=on 或 warp=plus"
}

do_install() {
    need_root
    log_step "Cloudflare WARP (wgcf) + Xray WireGuard 出站"
    ensure_deps
    check_xrayr
    install_wgcf
    wgcf_register_generate "${1:-}"
    write_outbound
    write_route
    echo ""
    log_info "配置完成。"
    echo -e "  ${YELLOW}说明:${NC}"
    echo -e "  • 使用 Xray 内嵌 WireGuard 出站（noKernelTun），不安装官方 warp-cli"
    echo -e "  • 仅「路由命中 AI/流媒体」的流量走 WARP，系统默认路由不变（SSH 安全）"
    echo -e "  • 需要已部署支持 wireguard 协议的 XrayR 版本（≥ 集成 wireguard 的构建）"
    echo ""
    if [[ "${NONINTERACTIVE:-0}" == "1" ]]; then
        restart_xrayr
    else
        echo -e "  ${BOLD}是否立即重启 xrayr？${NC} [Y/n]"
        read -r -n 1 -p "  → " ans
        echo ""
        case "${ans:-Y}" in
            n|N) log_warn "跳过重启，请稍后: systemctl restart xrayr" ;;
            *) restart_xrayr ;;
        esac
    fi
    show_status
}

do_uninstall() {
    need_root
    log_step "卸载 WARP 出站与路由规则"
    remove_warp_from_configs
    echo -e "  ${BOLD}是否删除 ${WARP_DIR} 下 wgcf 账号/配置？${NC} [y/N]"
    if [[ "${NONINTERACTIVE:-0}" == "1" ]]; then
        log_info "保留 ${WARP_DIR}"
    else
        read -r -n 1 -p "  → " ans
        echo ""
        case "${ans:-N}" in
            y|Y) rm -rf "$WARP_DIR"; log_info "已删除 $WARP_DIR" ;;
            *) log_info "保留 $WARP_DIR" ;;
        esac
    fi
    restart_xrayr
    log_info "卸载完成"
}

# ---- main ----
ACTION="${1:-}"
case "$ACTION" in
    --install)
        NONINTERACTIVE=1
        do_install
        ;;
    --regenerate)
        NONINTERACTIVE=1
        do_install force
        ;;
    --uninstall)
        NONINTERACTIVE=1
        do_uninstall
        ;;
    --status)
        show_status
        ;;
    --help|-h)
        sed -n '2,17p' "$0" | sed 's/^# \?//'
        exit 0
        ;;
    "")
        # interactive
        clear
        echo -e "${CYAN}${BOLD}XrayR4u — WARP (wgcf + WireGuard 出站) 配置${NC}"
        echo ""
        echo -e "  [1] 安装 / 更新（注册 wgcf + 写 route/outbound）"
        echo -e "  [2] 重新生成密钥并更新配置"
        echo -e "  [3] 卸载 WARP 出站与路由"
        echo -e "  [4] 查看状态"
        echo -e "  [0] 退出"
        echo ""
        read -r -n 1 -p "  → " c
        echo ""
        case "$c" in
            1) do_install ;;
            2) do_install force ;;
            3) do_uninstall ;;
            4) show_status ;;
            *) exit 0 ;;
        esac
        ;;
    *)
        log_error "未知参数: $ACTION （使用 --help）"
        ;;
esac
