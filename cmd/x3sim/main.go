package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/droxey/x3vault/internal/sim"
)

func main() {
	os.Exit(run())
}

func run() int {
	listen := flag.String("listen", "127.0.0.1:8080", "host:port to bind (localhost default)")
	seedRoot := flag.String("seed-root", "", "optional directory to create at start (e.g. /ereader)")
	flag.Parse()

	srv := sim.NewServer()
	if *seedRoot != "" {
		parent := "/"
		name := strings.Trim(*seedRoot, "/")
		if err := srv.Store.Mkdir(parent, name); err != nil {
			fmt.Fprintf(os.Stderr, "seed-root: %v\n", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{
		Addr:    *listen,
		Handler: srv.Handler(),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	fmt.Fprintf(os.Stderr, "x3sim listening on http://%s\n", *listen)
	fmt.Fprintf(os.Stderr, "open the file browser, then set device.base_url to that URL\n")
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
