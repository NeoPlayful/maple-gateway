#!/usr/bin/env bash
# Maple Gateway Linux 原生部署脚本（需 root）。
# 用法: ./install.sh /path/to/maple-gateway-linux-amd64
set -euo pipefail

BIN="${1:?usage: install.sh /path/to/maple-gateway-linux-amd64}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR=/usr/local/bin
CONFIG_DIR=/etc/maple-gateway
DATA_DIR=/var/lib/maple-gateway
LOG_DIR=/var/log/maple-gateway

id maple >/dev/null 2>&1 || useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin maple

mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
install -m 0755 "$BIN" "$INSTALL_DIR/maple-gateway"
install -m 0644 "$SCRIPT_DIR/../../configs/config.example.yaml" "$CONFIG_DIR/config.yaml"
install -m 0644 "$SCRIPT_DIR/../systemd/maple-gateway.service" /etc/systemd/system/maple-gateway.service

chown -R maple:maple "$DATA_DIR" "$LOG_DIR"

systemctl daemon-reload
systemctl enable maple-gateway
echo "installed. edit $CONFIG_DIR/config.yaml and run: systemctl start maple-gateway"
