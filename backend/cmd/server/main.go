// haijiao-web 服务入口。
//
// 环境变量（大部分配置已移至网页端设置页，持久化在 config.json）：
//   HJ_ADDR        监听地址（默认 :8080）
//   HJ_DATA_DIR    数据目录（默认 .）：config.json / state.json / downloads.json / videos/
//   HJ_INTERVAL    首次启动的默认轮询间隔秒（之后以网页端配置为准）
package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"

	"haijiao-web/backend/internal/api"
	"haijiao-web/backend/internal/config"
	"haijiao-web/backend/internal/ffmpeg"
	"haijiao-web/backend/internal/history"
	"haijiao-web/backend/internal/monitor"
)

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	log.SetFlags(log.Ltime)

	dataDir := envStr("HJ_DATA_DIR", ".")

	// 初始配置：首次运行时以环境变量为种子，之后一切以 config.json（网页端可改）为准
	seed := config.Default()
	seed.Interval = envInt("HJ_INTERVAL", seed.Interval)
	seed.APIBase = envStr("HJ_API_BASE", seed.APIBase)

	store, err := config.Open(filepath.Join(dataDir, "config.json"), seed)
	if err != nil {
		log.Fatalf("open config: %v", err)
	}

	// HJ_PASSWORD：为配置种子访问密码（配置里已有密码时不覆盖）
	if pw := os.Getenv("HJ_PASSWORD"); pw != "" {
		cfg := store.Get()
		if cfg.Password == "" {
			cfg.Password = pw
			if _, err := store.Update(cfg); err != nil {
				log.Printf("set password from env: %v", err)
			}
		}
	}

	// ffmpeg 自动检测/下载（后台执行，不阻塞启动；结果写运行日志）
	go func() {
		if err := ffmpeg.Ensure(dataDir); err != nil {
			log.Printf("[ffmpeg] %v", err)
			return
		}
		log.Printf("[ffmpeg] ready: %s", ffmpeg.Path())
	}()

	outDir := filepath.Join(dataDir, "videos")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("create out dir: %v", err)
	}

	hist, err := history.Open(filepath.Join(dataDir, "downloads.json"))
	if err != nil {
		log.Fatalf("open history: %v", err)
	}

	mon := monitor.New(store, monitor.Paths{
		OutDir:    outDir,
		StateFile: filepath.Join(dataDir, "state.json"),
	}, hist)
	mon.Run()

	addr := envStr("HJ_ADDR", ":8080")
	srv := api.New(mon, store, hist, addr, dataDir)
	cfg := store.Get()
	log.Printf("haijiao-web listening on %s (authors=%d, interval=%ds)",
		addr, len(cfg.Authors), cfg.Interval)
	log.Fatal(srv.ListenAndServe())
}
