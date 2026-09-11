// Command memos-wechat 是微信公众号测试号回调服务：接收文本消息并写入自建 Memos。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"memos-wechat/internal/config"
	"memos-wechat/internal/dedupe"
	"memos-wechat/internal/handler"
	"memos-wechat/internal/memos"
)

const (
	// 覆盖微信的重试窗口即可，过长的保留期只会白占内存。
	dedupeTTL = 60 * time.Second

	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("加载配置失败，服务无法启动", "error", err)
		os.Exit(1)
	}

	callback := handler.NewCallback(
		cfg,
		memos.New(cfg.MemosAPIURL, cfg.MemosAccessToken),
		dedupe.NewCache(dedupeTTL),
		logger,
	)

	mux := http.NewServeMux()
	callback.Register(mux)

	server := &http.Server{
		Addr:              ":" + cfg.ListenPort,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	// 只打印非敏感配置，令牌一律不出现在日志中。
	logger.Info("服务启动",
		"addr", server.Addr,
		"memosApiURL", cfg.MemosAPIURL,
		"allowOpenID", cfg.WechatAllowOpenID,
		"dedupeTTL", dedupeTTL.String(),
	)

	serveErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		logger.Error("HTTP 服务异常退出", "error", err)
		os.Exit(1)
	case sig := <-signals:
		logger.Info("收到退出信号，开始优雅关闭", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("HTTP 服务关闭异常", "error", err)
	}

	// 等完在途写入再退出，避免刚收下的消息被丢弃。
	callback.Wait()
	logger.Info("服务已退出")
}
