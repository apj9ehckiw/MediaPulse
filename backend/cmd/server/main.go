// MediaPulse 服务入口。
//
// 环境变量（大部分配置已移至网页端设置页，持久化在 config.json）：
//   MP_ADDR        监听地址（默认 :8080）
//   MP_DATA_DIR    数据目录（默认 .）：config.json / state.json / downloads.json / videos/
//   MP_INTERVAL    首次启动的默认轮询间隔秒（之后以网页端配置为准）
package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/apj9ehckiw/mediapulse/backend/internal/api"
	"github.com/apj9ehckiw/mediapulse/backend/internal/config"
	"github.com/apj9ehckiw/mediapulse/backend/internal/ffmpeg"
	"github.com/apj9ehckiw/mediapulse/backend/internal/history"
	"github.com/apj9ehckiw/mediapulse/backend/internal/monitor"
)

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envStrCompat 带旧名回退：MP_ 未设置时尝试 HJ_（兼容旧部署）
func envStrCompat(key, oldKey, def string) string {
	if v := envStr(key, ""); v != "" {
		return v
	}
	return envStr(oldKey, def)
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

	dataDir := envStrCompat("MP_DATA_DIR", "HJ_DATA_DIR", ".")

	// 初始配置：首次运行时以环境变量为种子，之后一切以 config.json（网页端可改）为准
	seed := config.Default()
	seed.Interval = envInt("MP_INTERVAL", seed.Interval)
	seed.APIBase = envStrCompat("MP_API_BASE", "HJ_API_BASE", seed.APIBase)

	store, err := config.Open(filepath.Join(dataDir, "config.json"), seed)
	if err != nil {
		log.Fatalf("open config: %v", err)
	}

	// MP_PASSWORD：为配置种子访问密码（配置里已有密码时不覆盖）
	if pw := envStrCompat("MP_PASSWORD", "HJ_PASSWORD", ""); pw != "" {
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

	addr := envStrCompat("MP_ADDR", "HJ_ADDR", ":8080")
	srv := api.New(mon, store, hist, addr, dataDir)
	cfg := store.Get()
	log.Printf("mediapulse listening on %s (authors=%d, interval=%ds)",
		addr, len(cfg.Authors), cfg.Interval)
	log.Fatal(srv.ListenAndServe())
}
