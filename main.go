package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/core"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed web/*
var ui embed.FS
var version = "0.5.0"

func main() {
	listen := flag.String("listen", "127.0.0.1:9816", "listen address")
	data := flag.String("data", "", "persistent data path")
	flag.Parse()
	path := *data
	if path == "" {
		if p := os.Getenv("BILIPDJ_DATA_DIR"); p != "" {
			path = p
		} else {
			path = filepath.Join(".", "data")
		}
	}
	content, e := fs.Sub(ui, "web")
	if e != nil {
		log.Fatal(e)
	}
	app := core.New(path, version, "ZzzHe2333/bilipdj-go")
	server := &http.Server{Addr: *listen, Handler: app.Routes(http.FileServer(http.FS(content))), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 65 * time.Second}
	app.Start()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		app.Stop()
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(closeCtx)
	}()
	fmt.Printf("BiliPDJ Go v%s → http://%s/\n", version, *listen)
	if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
