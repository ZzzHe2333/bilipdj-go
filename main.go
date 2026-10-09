package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/core"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var ui embed.FS
var version = "0.8.0"

func main() {
	listen := flag.String("listen", "127.0.0.1:9816", "listen address")
	data := flag.String("data", "", "persistent data path")
	flag.Parse()
	path := *data
	if path == "" {
		if p := os.Getenv("BILIPDJ_DATA_DIR"); p != "" {
			path = p
		} else {
			path = desktopDataDir()
		}
	}
	content, err := fs.Sub(ui, "web")
	if err != nil {
		desktopError(err.Error())
		log.Fatal(err)
	}
	app := core.New(path, version, "ZzzHe2333/bilipdj-go")
	server := &http.Server{Addr: *listen, Handler: app.Routes(http.FileServer(http.FS(content))), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 65 * time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		desktopError("BiliPDJ Go 无法启动监听服务（可能已有实例在运行）：\n" + err.Error())
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app.Start()
	go func() {
		<-ctx.Done()
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(closeCtx)
		app.Stop()
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	url := fmt.Sprintf("http://%s:%s/", host, port)
	stopDesktop := desktopStart(url, stop)
	defer stopDesktop()
	fmt.Printf("BiliPDJ Go v%s → %s\n", version, url)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		desktopError("BiliPDJ Go 服务异常退出：\n" + err.Error())
		log.Print(err)
	}
}
