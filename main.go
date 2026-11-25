// pad2xbox traduz um gamepad USB genérico (DragonRise / PS-style)
// para um controle Xbox 360 virtual, usando apenas a biblioteca padrão do Go.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
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
  -source string    caminho do gamepad (ex.: /dev/input/event30); vazio = autodetectar
  -deadzone float   deadzone radial dos sticks (padrão 0.08)
  -name string      nome do controle virtual (padrão "Microsoft X-Box 360 pad")
  -vendor uint      vendor ID virtual (padrão 0x045e)
  -product uint     product ID virtual (padrão 0x028e)
  -verbose          logar eventos traduzidos`)
}

type options struct {
	source   *string
	deadzone *float64
	name     *string
	vendor   *uint
	product  *uint
	verbose  *bool
}

func newFlags(name string) (*flag.FlagSet, options) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	o := options{
		source:   fs.String("source", "", "caminho do gamepad; vazio = autodetectar"),
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

func runCmd(args []string) {
	fs, o := newFlags("run")
	_ = fs.Parse(args)

	name := *o.name
	vendor := uint16(*o.vendor)
	product := uint16(*o.product)
	deadzone := *o.deadzone
	verbose := *o.verbose
	sourcePath := *o.source

	pad, err := uinput.Create(name, vendor, product)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	defer pad.Close()
	fmt.Printf("Controle virtual criado: %q (%04x:%04x)\n", name, vendor, product)

	done, stop := doneChannel()
	defer stop()

	for {
		select {
		case <-done:
			return
		default:
		}

		src, err := openSource(sourcePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "aviso:", err, "-- tentando novamente em 2s")
			select {
			case <-done:
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		fmt.Printf("Conectado em %s (%s). Pressione Ctrl+C para parar.\n",
			src.Path, stringsTrim(src.Name))

		state := mapper.New(src, deadzone)
		keys, axes := state.Snapshot()
		if err := pad.Send(keys, axes); err != nil {
			fmt.Fprintln(os.Stderr, "erro ao publicar estado inicial:", err)
			src.Close()
			return
		}

		connected := true
		for connected {
			select {
			case <-done:
				src.Close()
				return
			default:
			}
			ev, err := src.ReadTimeout(250 * time.Millisecond)
			if errors.Is(err, evdev.ErrTimeout) {
				continue
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "gamepad desconectado:", err)
				_ = src.Close()
				connected = false
				break
			}
			if ev.Type == evdev.EV_SYN {
				keys, axes := state.Snapshot()
				if err := pad.Send(keys, axes); err != nil {
					fmt.Fprintln(os.Stderr, "erro ao enviar eventos:", err)
					_ = src.Close()
					return
				}
				continue
			}
			state.Apply(ev)
			if verbose {
				fmt.Println("→", ev.String())
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
