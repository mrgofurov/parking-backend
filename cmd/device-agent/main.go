// device-agent runs on the parking's local network. It connects out to the
// backend (no open ports or static IP needed at the parking), executes
// barrier commands on local devices and forwards camera events.
//
//	device-agent -config /etc/parking-agent/agent.env
//
// Without -config it reads agent.env next to the executable, if present (the
// usual layout on Windows: parking-agent.exe + agent.env in one folder).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/smart-parking/parking-backend/internal/agent"
)

// version is set at build time: -ldflags "-X main.version=1.0.0"
var version = "dev"

// maxLogSize: the log file is moved to <name>.1 when it grows past this.
const maxLogSize = 10 << 20

func main() {
	configPath := flag.String("config", "", "AGENT_* sozlamalari yozilgan .env fayl (berilmasa .exe yonidagi agent.env)")
	showVersion := flag.Bool("version", false, "versiyani ko'rsatish")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	if *configPath == "" {
		if exe, err := os.Executable(); err == nil {
			if p := filepath.Join(filepath.Dir(exe), "agent.env"); fileExists(p) {
				*configPath = p
			}
		}
	}
	if *configPath != "" {
		if err := godotenv.Load(*configPath); err != nil {
			log.Fatalf("[agent] %s o'qilmadi: %v", *configPath, err)
		}
	}
	setupLogFile(os.Getenv("AGENT_LOG_FILE"))

	cfg, err := agent.ConfigFromEnv()
	if err != nil {
		log.Fatalf("[agent] sozlama xatosi: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("[agent] device-agent %s ishga tushdi, backend: %s, sozlama: %s\n", version, cfg.ServerURL, *configPath)

	client := agent.NewClient(cfg, version)
	go client.Run(ctx)

	if cfg.Listen != "" {
		queue, err := agent.NewQueue(cfg.QueueDir, cfg.QueueMaxItems)
		if err != nil {
			log.Fatalf("[agent] navbat papkasi (%s) ochilmadi: %v", cfg.QueueDir, err)
		}
		fwd := agent.NewForwarder(cfg, queue, client.Status, version)
		go fwd.Replay(ctx, 3*time.Second)

		srv := &http.Server{Addr: cfg.Listen, Handler: fwd.Handler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		}()
		log.Printf("[agent] kamera hodisalari qabul qilinadi: http://<agent-ip>%s/api/v1/anpr/...\n", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[agent] %s band yoki ochilmadi: %v", cfg.Listen, err)
		}
	}
	<-ctx.Done()
	log.Println("[agent] to'xtatildi")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// setupLogFile also writes the log to a file (a scheduled task or service has
// no console). The previous file is kept as <path>.1 once it gets large.
func setupLogFile(path string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		log.Printf("[agent] log papkasi ochilmadi: %v", err)
		return
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		log.Printf("[agent] log fayli ochilmadi: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}
