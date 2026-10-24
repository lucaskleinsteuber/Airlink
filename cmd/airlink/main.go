package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lucasfd3/Airlink/internal/model"
	"github.com/lucasfd3/Airlink/internal/server"
)

func main() {
	var opts server.Options
	var stdio, version bool
	flag.StringVar(&opts.DataDir, "data-dir", "", "Private state directory (default: OS user config/AirLink)")
	flag.StringVar(&opts.ReceiveDir, "receive-dir", "", "Override persisted receive directory")
	flag.IntVar(&opts.Port, "port", 0, "HTTPS port (default 43871, fallback available port)")
	flag.BoolVar(&opts.NoDiscovery, "no-discovery", false, "Disable mDNS; QR and addresses remain available")
	flag.BoolVar(&stdio, "stdio", false, "Private native-app JSON control over stdin/stdout")
	flag.BoolVar(&version, "version", false, "Print version and exit")
	flag.Parse()
	if version {
		fmt.Println(model.Version)
		return
	}
	log.SetOutput(os.Stderr)
	s, err := server.New(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if stdio {
		if err = s.Stdio(os.Stdin, os.Stdout); err != nil {
			log.Print(err)
		}
		return
	}
	st := s.Status()
	fmt.Fprintf(os.Stderr, "AirLink %s · %s\n", model.Version, st.Name)
	for _, u := range st.URLs {
		fmt.Fprintln(os.Stderr, "Browser:", u)
	}
	fmt.Fprintln(os.Stderr, "Fingerprint:", st.Fingerprint)
	fmt.Fprintln(os.Stderr, "Private local console:", s.AdminURL())
	if flag.NArg() > 0 && flag.Arg(0) == "status" {
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
}
