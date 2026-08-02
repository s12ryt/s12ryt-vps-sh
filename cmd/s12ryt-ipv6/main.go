package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/panel"
	"github.com/s12ryt/s12ryt-vps-sh/internal/store"
)

const defaultConfigPath = "/opt/s12ryt-ipv6/config/config.json"
const defaultPasswordHashPath = "/opt/s12ryt-ipv6/secrets/password.hash"
const shutdownTimeout = 10 * time.Second

type managedHTTPServer interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
}

type runtimeOptions struct {
	ConfigPath       string
	PasswordHashPath string
	Entropy          io.Reader
	Clock            func() time.Time
	Listen           func(string, string) (net.Listener, error)
	NewHTTPServer    func(string, http.Handler) managedHTTPServer
}

type application struct {
	address string
	handler http.Handler
}

func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "錯誤：s12ryt IPv6 管理面板必須以 root 執行。")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runApplication(ctx, runtimeOptions{}); err != nil {
		fmt.Fprintln(os.Stderr, "錯誤：", err)
		os.Exit(1)
	}
}

func loadApplication(options runtimeOptions) (application, error) {
	options = withRuntimeDefaults(options)
	configStore := store.NewConfigStore(options.ConfigPath)
	config, err := configStore.Load()
	if err != nil {
		return application{}, fmt.Errorf("載入面板設定：%w", err)
	}
	passwordHash, err := readProtectedPasswordHash(options.PasswordHashPath)
	if err != nil {
		return application{}, err
	}
	hasher := auth.NewPasswordHasher(options.Entropy)
	if _, err := hasher.Verify(passwordHash, ""); err != nil {
		return application{}, fmt.Errorf("驗證管理密碼雜湊格式：%w", err)
	}
	sessions := auth.NewSessionManager(options.Entropy, options.Clock)
	limiter := auth.NewLoginLimiter(options.Clock)
	server := panel.NewServer(panel.Options{
		BasePath:     config.Panel.Path,
		PasswordHash: passwordHash,
		Hasher:       hasher,
		Sessions:     sessions,
		Limiter:      limiter,
		Config:       config,
		Store:        configStore,
	})
	return application{
		address: fmt.Sprintf("[::]:%d", config.Panel.Port),
		handler: server.Handler(),
	}, nil
}

func runApplication(ctx context.Context, options runtimeOptions) error {
	options = withRuntimeDefaults(options)
	application, err := loadApplication(options)
	if err != nil {
		return err
	}
	listener, err := options.Listen("tcp", application.address)
	if err != nil {
		return fmt.Errorf("監聽管理面板 %s：%w", application.address, err)
	}
	defer listener.Close()

	server := options.NewHTTPServer(application.address, application.handler)
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("執行管理面板：%w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("關閉管理面板：%w", err)
		}
		err := <-serveResult
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("管理面板關閉後回傳錯誤：%w", err)
		}
		return nil
	}
}

func withRuntimeDefaults(options runtimeOptions) runtimeOptions {
	if options.ConfigPath == "" {
		options.ConfigPath = defaultConfigPath
	}
	if options.PasswordHashPath == "" {
		options.PasswordHashPath = defaultPasswordHashPath
	}
	if options.Entropy == nil {
		options.Entropy = rand.Reader
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Listen == nil {
		options.Listen = net.Listen
	}
	if options.NewHTTPServer == nil {
		options.NewHTTPServer = func(address string, handler http.Handler) managedHTTPServer {
			return &http.Server{
				Addr:              address,
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      30 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
		}
	}
	return options
}

func readProtectedPasswordHash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("讀取管理密碼雜湊資訊：%w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("管理密碼雜湊必須是一般檔案")
	}
	if info.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("管理密碼雜湊權限必須是 0600，目前為 %04o", info.Mode().Perm())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("讀取管理密碼雜湊：%w", err)
	}
	passwordHash := strings.TrimSpace(string(contents))
	if passwordHash == "" {
		return "", errors.New("管理密碼雜湊不得為空")
	}
	return passwordHash, nil
}
