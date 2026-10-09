package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/core"
	"github.com/ZzzHe2333/bilipdj-go/internal/storage"
	"github.com/ZzzHe2333/bilipdj-go/internal/update"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var ui embed.FS
var version = "0.10.0"

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--apply-update" {
		if err := update.RunHelper(os.Args[2]); err != nil {
			log.Printf("自更新失败：%v", err)
			desktopError("BiliPDJ Go 更新安装失败：\n" + err.Error())
			os.Exit(1)
		}
		return
	}

	listen := flag.String("listen", "127.0.0.1:9816", "listen address")
	data := flag.String("data", "", "persistent data path")
	flag.Parse()
	explicit := *data
	if explicit == "" {
		explicit = os.Getenv("BILIPDJ_DATA_DIR")
	}
	binary, err := os.Executable()
	if err != nil {
		binary, _ = filepath.Abs(".")
	}
	appDir := filepath.Dir(binary)
	legacyGo := desktopDataDir()
	if !filepath.IsAbs(legacyGo) {
		legacyGo, _ = filepath.Abs(legacyGo)
	}
	plan, err := storage.Resolve(appDir, legacyGo, explicit)
	if err != nil {
		desktopError(err.Error())
		log.Fatal(err)
	}
	if plan.Migrate {
		n, migrateErr := storage.CopyOnly(plan, appDir)
		if migrateErr != nil {
			desktopError("用户数据迁移失败（旧数据未删除）：\n" + migrateErr.Error())
			log.Fatal(migrateErr)
		}
		log.Printf("Copied %d user-data files to %s without deleting old files", n, plan.User)
		plan.Choice = "user"
	}
	path := plan.Active
	content, err := fs.Sub(ui, "web")
	if err != nil {
		desktopError(err.Error())
		log.Fatal(err)
	}
	app := core.New(path, version, "ZzzHe2333/bilipdj-go")
	app.SetStoragePlan(plan)
	server := &http.Server{Addr: *listen, Handler: app.Routes(http.FileServer(http.FS(content))), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 65 * time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		desktopError("BiliPDJ Go 无法启动监听服务（可能已有实例在运行）：\n" + err.Error())
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app.Start()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
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
	// Docker images must be replaced via their orchestrator, never mutated in-place.
	_, dockerErr := os.Stat("/.dockerenv")
	if os.IsNotExist(dockerErr) {
		app.SetInstallAction(func(d update.Downloaded) error {
			helper, job, e := update.PrepareInstall(d, binary, os.Args[1:], url+"health")
			if e != nil {
				return e
			}
			if e = update.LaunchInstall(helper, job); e != nil {
				return e
			}
			go func() { time.Sleep(900 * time.Millisecond); stop() }()
			return nil
		})
	}
	stopDesktop := desktopStart(url, stop)
	defer stopDesktop()
	fmt.Printf("BiliPDJ Go v%s → %s\n", version, url)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		desktopError("BiliPDJ Go 服务异常退出：\n" + err.Error())
		log.Print(err)
	}
	stop()
	<-shutdownDone
}
