#!/usr/bin/env bash
# Instala o pad2xbox como serviço systemd, executando como o usuário chamador.
# Uso: sudo ./install-systemd.sh
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "Execute com sudo: sudo ./install-systemd.sh" >&2
  exit 1
fi

SRC_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUN_USER="${SUDO_USER:-j}"
BIN=/usr/local/bin/pad2xbox
UNIT=/etc/systemd/system/pad2xbox.service
RULE=/etc/udev/rules.d/99-pad2xbox.rules
TMP_BIN="$(runuser -u "${RUN_USER}" -- mktemp /tmp/pad2xbox-build.XXXXXX)"

# Remove a instalação antiga (gamepad-emulator), se existir.
if [[ -f /etc/systemd/system/gamepad-emulator.service ]]; then
  echo "==> Removendo instalação legada (gamepad-emulator)..."
  systemctl disable --now gamepad-emulator.service || true
  rm -f /etc/systemd/system/gamepad-emulator.service \
        /usr/local/bin/gamepad-emulator \
        /etc/udev/rules.d/99-gamepad-emulator.rules
fi

echo "==> Compilando binário como ${RUN_USER}..."
runuser -u "${RUN_USER}" -- bash -c "cd '${SRC_DIR}' && go build -o '${TMP_BIN}' ."

echo "==> Instalando ${BIN}"
install -m 0755 "${TMP_BIN}" "${BIN}"
rm -f "${TMP_BIN}"

echo "==> Instalando regra udev (acesso a /dev/uinput)"
install -m 0644 "${SRC_DIR}/99-pad2xbox.rules" "${RULE}"

echo "==> Instalando ${UNIT} (User=${RUN_USER})"
cat > "${UNIT}" <<EOF
[Unit]
Description=pad2xbox: generic USB gamepad -> Xbox 360 virtual controller
Documentation=https://github.com/evandrojr/pad2xbox
After=local-fs.target

[Service]
Type=simple
User=${RUN_USER}
Group=$(id -gn "${RUN_USER}")
WorkingDirectory=${SRC_DIR}
ExecStart=${BIN} run
Restart=on-failure
RestartSec=2
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

udevadm control --reload
systemctl daemon-reload
systemctl enable --now pad2xbox.service
# enable --now não reinicia um serviço já ativo; força o carregamento do binário novo.
systemctl restart pad2xbox.service

echo
systemctl --no-pager status pad2xbox.service || true
echo
echo "Instalado. Comandos úteis:"
echo "  systemctl status pad2xbox"
echo "  journalctl -u pad2xbox -f"
echo "Para desinstalar:"
echo "  sudo systemctl disable --now pad2xbox.service"
echo "  sudo rm -f ${UNIT} ${BIN} ${RULE}"
