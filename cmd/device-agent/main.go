// device-agent runs on the parking's local network. It connects out to the
// backend (no open ports or static IP needed at the parking), executes
// barrier commands on local devices and forwards camera events.
//
//	device-agent -config /etc/parking-agent/agent.env
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/smart-parking/parking-backend/internal/agent"
)

// version is set at build time: -ldflags "-X main.version=1.0.0"
var version = "dev"

func main() {
	configPath := flag.String("config", "", "AGENT_* sozlamalari yozilgan .env fayl (ixtiyoriy)")
	showVersion := flag.Bool("version", false, "versiyani ko'rsatish")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *configPath != "" {
		if err := godotenv.Load(*configPath); err != nil {
			log.Fatalf("[agent] %s o'qilmadi: %v", *configPath, err)
		}
	}

	cfg, err := agent.ConfigFromEnv()
	if err != nil {
		log.Fatalf("[agent] sozlama xatosi: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("[agent] device-agent %s ishga tushdi, backend: %s\n", version, cfg.ServerURL)

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
