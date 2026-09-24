package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"easynotify/server"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func run() error {
	command, args := "start", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("easynotify-server", flag.ContinueOnError)
	config := flags.String("config", env("EASYNOTIFY_CONFIG", filepath.Join(home, ".config", "easynotify", "config.json")), "key configuration file")
	host := flags.String("host", env("HOST", "127.0.0.1"), "listen address")
	port := flags.String("port", env("PORT", "8787"), "listen port")
	stdin := flags.Bool("stdin", false, "read key from stdin (key command)")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: easynotify-server [start|key] [options]\n\nkey generates and saves a key; key --stdin saves your key.\nRestart the relay after changing the key.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments; see --help")
	}
	switch command {
	case "key":
		var key string
		if *stdin {
			data, err := io.ReadAll(io.LimitReader(os.Stdin, 4098))
			if err != nil {
				return err
			}
			if len(data) > 4097 {
				return errors.New("key input exceeds 4096 bytes")
			}
			key = strings.TrimSpace(string(data))
		} else {
			b := make([]byte, 24)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			key = hex.EncodeToString(b)
		}
		if err := server.SaveKey(*config, key); err != nil {
			return err
		}
		fmt.Println(key)
		fmt.Fprintln(os.Stderr, "Key saved. Restart relay to apply.")
		return nil
	case "start":
		settings, err := server.LoadConfig(*config)
		if err != nil && !(os.IsNotExist(err) && os.Getenv("EASYNOTIFY_KEY") != "") {
			return fmt.Errorf("cannot read configuration; run the key command first: %w", err)
		}
		if key := os.Getenv("EASYNOTIFY_KEY"); key != "" {
			settings.Key = key
		}
		number, err := strconv.Atoi(*port)
		if err != nil || number < 0 || number > 65535 {
			return errors.New("port must be 0–65535 (0 selects a free port)")
		}
		relay, err := server.NewWithConfig(settings)
		if err != nil {
			return err
		}
		defer relay.Close()
		listener, err := net.Listen("tcp", net.JoinHostPort(*host, *port))
		if err != nil {
			return err
		}
		httpServer := &http.Server{Handler: relay, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
		stopped, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-stopped.Done()
			relay.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(ctx)
		}()
		fmt.Fprintln(os.Stderr, "EasyNotify listening on", listener.Addr())
		if err := httpServer.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	default:
		return errors.New("use start or key; see --help")
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
