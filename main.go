// pad2xbox traduz um gamepad USB genérico (DragonRise / PS-style)
// para um controle Xbox 360 virtual, usando apenas a biblioteca padrão do Go.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/evandrojr/pad2xbox/internal/evdev"
	"github.com/evandrojr/pad2xbox/internal/mapper"
	"github.com/evandrojr/pad2xbox/internal/uinput"
)

const (
	defaultName    = "Microsoft X-Box 360 pad"
	defaultVendor  = 0x045e
	defaultProduct = 0x028e
)

func usage() {
	fmt.Fprintln(os.Stderr, `Uso:
  go run . detect                 listar gamepads conectados
  go run . listen [-source PATH]  mostrar eventos brutos (aperte botões para mapear)
  go run . run [opções]           iniciar o emulador (padrão)

Opções de run/listen:
  -source string    caminho(s) do gamepad, separados por vírgula; vazio = autodetectar
  -max-pads int     número máximo de pads virtuais (padrão 2)
  -deadzone float   deadzone radial dos sticks (padrão 0.08)
  -name string      nome do controle virtual (padrão "Microsoft X-Box 360 pad")
  -vendor uint      vendor ID virtual (padrão 0x045e)
  -product uint     product ID virtual (padrão 0x028e)
  -verbose          logar eventos traduzidos`)
}

type options struct {
	source   *string
	maxPads  *int
	deadzone *float64
	name     *string
	vendor   *uint
	product  *uint
	verbose  *bool
}

func newFlags(name string) (*flag.FlagSet, options) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	o := options{
		source:   fs.String("source", "", "caminhos dos gamepads separados por vírgula; vazio = autodetectar"),
		maxPads:  fs.Int("max-pads", 2, "número máximo de pads virtuais"),
		deadzone: fs.Float64("deadzone", 0.08, "deadzone radial dos sticks"),
		name:     fs.String("name", defaultName, "nome do controle virtual"),
		vendor:   fs.Uint("vendor", defaultVendor, "vendor ID virtual"),
		product:  fs.Uint("product", defaultProduct, "product ID virtual"),
		verbose:  fs.Bool("verbose", false, "logar eventos"),
	}
	return fs, o
}

func openSource(path string) (*evdev.Device, error) {
	if path == "" {
		pads, err := evdev.Detect()
		if err != nil {
			return nil, err
		}
		if len(pads) == 0 {
			return nil, errors.New("nenhum gamepad encontrado em /dev/input/event*")
		}
		path = pads[0].Path
		fmt.Printf("Gamepad detectado: %s (%s) em %s\n", stringsTrim(pads[0].Name), deviceID(pads[0].ID), path)
	}
	return evdev.Open(path)
}

func stringsTrim(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r != 0 && r != ' ' {
			out = append(out, r)
		}
	}
	return string(out)
}

func deviceID(id evdev.ID) string {
	return fmt.Sprintf("%04x:%04x", id.Vendor, id.Product)
}

func detectCmd(args []string) {
	pads, err := evdev.Detect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	if len(pads) == 0 {
		fmt.Println("Nenhum gamepad encontrado.")
		return
	}
	for _, p := range pads {
		fmt.Printf("\n%s\n", p.Path)
		fmt.Printf("  Nome: %q\n", p.Name)
		fmt.Printf("  ID:   bus=%#04x vendor=%#04x product=%#04x version=%#04x\n",
			p.ID.Bus, p.ID.Vendor, p.ID.Product, p.ID.Version)
		fmt.Print("  Botões: ")
		var keys []string
		for code := uint16(0x120); code <= 0x13f; code++ {
			if p.HasKey(code) {
				keys = append(keys, evdev.KeyName(code))
			}
		}
		if len(keys) == 0 {
			keys = append(keys, "(nenhum BTN_GAMEPAD/BTN_JOYSTICK)")
		}
		fmt.Println(join(keys, ", "))
		fmt.Print("  Eixos: ")
		var axes []string
		for code := uint16(0); code < 64; code++ {
			if p.HasAbs(code) {
				info := p.AbsInfo[code]
				axes = append(axes, fmt.Sprintf("%s [%d..%d]", evdev.AbsName(code), info.Minimum, info.Maximum))
			}
		}
		fmt.Println(join(axes, ", "))
	}
}

func join(items []string, sep string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += sep
		}
		out += item
	}
	return out
}

func doneChannel() (<-chan struct{}, func()) {
	done := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		close(done)
	}()
	return done, func() { signal.Stop(sig) }
}

func listenCmd(args []string) {
	fs, o := newFlags("listen")
	_ = fs.Parse(args)
	src, err := openSource(*o.source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	defer src.Close()
	done, stop := doneChannel()
	defer stop()

	fmt.Printf("Ouvindo %s — aperte botões (Ctrl+C para sair)\n", src.Path)
	for {
		select {
		case <-done:
			return
		default:
		}
		ev, err := src.ReadTimeout(200 * time.Millisecond)
		if errors.Is(err, evdev.ErrTimeout) {
			continue
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro de leitura:", err)
			return
		}
		if ev.Type == evdev.EV_MSC {
			continue
		}
		fmt.Println(ev.String())
	}
}

// channel liga um gamepad físico ao seu próprio pad Xbox 360 virtual.
type channel struct {
	path  string
	src   *evdev.Device
	pad   *uinput.Device
	state *mapper.State
}

func parseSources(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runCmd(args []string) {
	fs, o := newFlags("run")
	_ = fs.Parse(args)

	name := *o.name
	vendor := uint16(*o.vendor)
	product := uint16(*o.product)
	deadzone := *o.deadzone
	verbose := *o.verbose
	explicit := parseSources(*o.source)
	maxPads := *o.maxPads
	if maxPads < 1 {
		maxPads = 1
	}

	done, stop := doneChannel()
	defer stop()

	channels := make(map[string]*channel)

	removeChannel := func(ch *channel) {
		delete(channels, ch.path)
		_ = ch.src.Close()
		_ = ch.pad.Close()
		fmt.Printf("Pad removido: %s\n", ch.path)
	}
	defer func() {
		for _, ch := range channels {
			removeChannel(ch)
		}
	}()

	addChannel := func(path string) {
		if len(channels) >= maxPads {
			return
		}
		if _, exists := channels[path]; exists {
			return
		}
		src, err := evdev.Open(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "aviso: não foi possível abrir", path+":", err)
			return
		}
		pad, err := uinput.Create(name, vendor, product)
		if err != nil {
			_ = src.Close()
			fmt.Fprintln(os.Stderr, "erro ao criar pad virtual:", err)
			return
		}
		state := mapper.New(src, deadzone)
		keys, axes := state.Snapshot()
		if err := pad.Send(keys, axes); err != nil {
			_ = src.Close()
			_ = pad.Close()
			fmt.Fprintln(os.Stderr, "erro ao publicar estado inicial:", err)
			return
		}
		channels[path] = &channel{path: path, src: src, pad: pad, state: state}
		fmt.Printf("Pad %d conectado: %s (%s) -> Xbox 360 #%d\n",
			len(channels), path, stringsTrim(src.Name), len(channels))
	}

	syncChannels := func() {
		targets := explicit
		if len(targets) == 0 {
			pads, err := evdev.Detect()
			if err != nil {
				fmt.Fprintln(os.Stderr, "erro ao detectar gamepads:", err)
				return
			}
			for _, p := range pads {
				targets = append(targets, p.Path)
			}
		}
		want := make(map[string]bool, len(targets))
		for _, p := range targets {
			want[p] = true
		}
		for path, ch := range channels {
			if !want[path] {
				removeChannel(ch)
			}
		}
		for _, p := range targets {
			if _, err := evdev.Inspect(p); err != nil {
				continue // ainda não conectado
			}
			addChannel(p)
		}
	}

	syncChannels()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		default:
		}
		select {
		case <-ticker.C:
			syncChannels()
		default:
		}

		sources := make([]*evdev.Device, 0, len(channels))
		for _, ch := range channels {
			sources = append(sources, ch.src)
		}
		ready, err := evdev.Select(sources, 200*time.Millisecond)
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro no select:", err)
			continue
		}
		for _, src := range ready {
			var target *channel
			for _, ch := range channels {
				if ch.src == src {
					target = ch
					break
				}
			}
			if target == nil {
				continue
			}
			for {
				ev, err := target.src.Read()
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
					break
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, "gamepad desconectado:", target.path, err)
					removeChannel(target)
					break
				}
				if ev.Type == evdev.EV_SYN {
					keys, axes := target.state.Snapshot()
					if err := target.pad.Send(keys, axes); err != nil {
						fmt.Fprintln(os.Stderr, "erro ao enviar eventos:", err)
						removeChannel(target)
						break
					}
					continue
				}
				target.state.Apply(ev)
				if verbose {
					fmt.Printf("[%s] → %s\n", target.path, ev.String())
				}
			}
		}
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "detect":
			detectCmd(os.Args[2:])
			return
		case "listen":
			listenCmd(os.Args[2:])
			return
		case "run":
			runCmd(os.Args[2:])
			return
		case "help", "-h", "--help":
			usage()
			return
		default:
			fmt.Fprintln(os.Stderr, "comando desconhecido:", os.Args[1])
			usage()
			os.Exit(2)
		}
	}
	runCmd(nil)
}
