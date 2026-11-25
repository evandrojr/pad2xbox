# pad2xbox

Transforma um **gamepad USB genérico com layout PlayStation/DirectInput** em um
**controle Xbox 360 virtual** no Linux, usando **Go puro** (biblioteca padrão,
sem dependências externas).

Feito para o `DragonRise PC TWIN SHOCK` (`0079:0006`), que o kernel expõe como
`Microntek USB Joystick` com botões `0x120`–`0x12b` em vez de `BTN_GAMEPAD`.
O pad virtual se apresenta como `045e:028e` / `Microsoft X-Box 360 pad`, então
SDL2/SDL3, Steam Input, Wine/Proton e jogos com XInput aplicam o mapeamento
corretamente automaticamente.

> Não emula um DualShock/DualSense completo (touchpad, giroscópio, gatilhos
> adaptativos). Para jogos, o layout Xbox atende praticamente todos os casos.

## Instalação rápida (systemd)

```bash
git clone https://github.com/evandrojr/pad2xbox.git
cd pad2xbox
sudo ./install-systemd.sh
```

O script compila o binário para `/usr/local/bin/pad2xbox`, instala a regra
udev, cria `/etc/systemd/system/pad2xbox.service` (rodando como o usuário que
chamou o `sudo`) e já habilita/inicia o serviço.

Verificar:

```bash
systemctl status pad2xbox
journalctl -u pad2xbox -f
grep -A8 -i 'X-Box 360 pad' /proc/bus/input/devices
```

Desinstalar:

```bash
sudo systemctl disable --now pad2xbox.service
sudo rm -f /etc/systemd/system/pad2xbox.service \
          /usr/local/bin/pad2xbox \
          /etc/udev/rules.d/99-pad2xbox.rules
```

## Uso manual (sem systemd)

```bash
go run . detect                          # lista gamepads e suas capacidades
go run . listen                          # mostra eventos brutos (aperte botões)
go run . run                             # inicia o emulador (até 2 pads)
go run . run -max-pads 4                 # permite até 4 pads
go run . run -source /dev/input/event30,/dev/input/event34
go run . run -deadzone 0.12 -verbose
```

`Ctrl+C` encerra e remove os dispositivos virtuais.

### Dois ou mais controles

Por padrão o `run` aceita **até 2 gamepads**, criando um pad Xbox 360 virtual
independente para cada um (J1/J2). A detecção é automática e inclui hot-plug:
conectar ou desconectar um controle atualiza os pads virtuais em tempo real.
Use `-max-pads N` para permitir mais, ou `-source caminho1,caminho2` para fixar
quais dispositivos usar. Dispositivos uinput virtuais são ignorados na detecção
para não gerar loop com o próprio emulador.

## Mapeamento

O `hid-dragonrise` entrega botões legacy (`0x120`..`0x12b`). Ordem física do
PC TWIN SHOCK:

| Botão físico | Origem | Xbox virtual |
|---|---|---|
| Triângulo | `0x120` | `BTN_NORTH` (Y) |
| Círculo | `0x121` | `BTN_EAST` (B) |
| X / Cross | `0x122` | `BTN_SOUTH` (A) |
| Quadrado | `0x123` | `BTN_WEST` (X) |
| L2 | `0x124` | `ABS_Z` (0..255) |
| R2 | `0x125` | `ABS_RZ` (0..255) |
| L1 | `0x126` | `BTN_TL` (LB) |
| R1 | `0x127` | `BTN_TR` (RB) |
| Select | `0x128` | `BTN_SELECT` (Back) |
| Start | `0x129` | `BTN_START` |
| L3 | `0x12a` | `BTN_THUMBL` |
| R3 | `0x12b` | `BTN_THUMBR` |
| Stick esquerdo | `ABS_X/Y` [0..255] | `ABS_X/Y` [-32768..32767] |
| Stick direito | `ABS_Z/RZ` [0..255] | `ABS_RX/RY` [-32768..32767] |
| D-pad | `ABS_HAT0X/Y` | `ABS_HAT0X/Y` |

Sticks passam por deadzone radial (padrão `0.08`) e renormalização.

### Botão MODE

O MODE é um switch de hardware (LED verde/vermelho), não um botão reportado ao
evdev. Use o **modo analógico (LED vermelho)**: D-pad em `ABS_HAT0X/Y` e os
sticks em `ABS_X/Y` e `ABS_Z/RZ`. No modo verde o D-pad vem nos eixos e os
botões de face se duplicam no stick direito.

## Estrutura

```
main.go                        CLI: detect / listen / run
internal/evdev/evdev.go        leitura de /dev/input (ioctl EVIOC*)
internal/uinput/uinput.go      criação do pad Xbox 360 virtual (/dev/uinput)
internal/mapper/mapper.go      tradução, deadzone e gatilhos analógicos
install-systemd.sh             instalação como serviço systemd
pad2xbox.service               unit systemd (modelo)
99-pad2xbox.rules              regra udev para /dev/uinput
```

## Desenvolvimento

```bash
go test ./...
go vet ./...
```

Requer Go 1.27+ e Linux com `uinput` habilitado.
