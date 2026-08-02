package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	"github.com/s12ryt/s12ryt-vps-sh/internal/nodes"
	"github.com/s12ryt/s12ryt-vps-sh/internal/panel"
	"github.com/s12ryt/s12ryt-vps-sh/internal/store"
)

const defaultConfigPath = "/opt/s12ryt-ipv6/config/config.json"
const defaultPasswordHashPath = "/opt/s12ryt-ipv6/secrets/password.hash"
const defaultProjectRoot = "/opt/s12ryt-ipv6"
const shutdownTimeout = 10 * time.Second

type managedHTTPServer interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
}

type runtimeOptions struct {
	ConfigPath             string
	PasswordHashPath       string
	Entropy                io.Reader
	Clock                  func() time.Time
	PortChecker            projectnetwork.PortAvailabilityChecker
	PortAllocationAttempts int
	Listen                 func(string, string) (net.Listener, error)
	NewHTTPServer          func(string, http.Handler) managedHTTPServer
}

type application struct {
	address string
	handler http.Handler
}

type initializationOptions struct {
	ProjectRoot string
	Entropy     io.Reader
}

type initializationResult struct {
	Password string
	WebPath  string
	Port     int
}

type commandOptions struct {
	ProjectRoot string
	Entropy     io.Reader
	Output      io.Writer
	Context     context.Context
	Addresses   func() ([]netip.Addr, error)
}

func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "錯誤：s12ryt IPv6 管理面板必須以 root 執行。")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCommand(os.Args[1:], commandOptions{Context: ctx, Output: os.Stdout}); err != nil {
		fmt.Fprintln(os.Stderr, "錯誤：", err)
		os.Exit(1)
	}
}

func runCommand(arguments []string, options commandOptions) error {
	if options.ProjectRoot == "" {
		options.ProjectRoot = defaultProjectRoot
	}
	if options.Entropy == nil {
		options.Entropy = rand.Reader
	}
	if options.Output == nil {
		options.Output = os.Stdout
	}
	if options.Context == nil {
		options.Context = context.Background()
	}

	if len(arguments) == 1 && arguments[0] == "init" {
		result, err := initializeProject(initializationOptions{
			ProjectRoot: options.ProjectRoot,
			Entropy:     options.Entropy,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(options.Output, "初始管理密碼：%s\nWeb 路徑：%s\n管理埠：%d\n", result.Password, result.WebPath, result.Port)
		return nil
	}
	if len(arguments) == 1 && arguments[0] == "status" {
		if options.Addresses == nil {
			options.Addresses = systemGlobalAddresses
		}
		return printProjectStatus(options.ProjectRoot, options.Output, options.Addresses)
	}
	if len(arguments) == 0 || (len(arguments) == 1 && arguments[0] == "serve") {
		return runApplication(options.Context, runtimeOptions{
			ConfigPath:       filepath.Join(options.ProjectRoot, "config", "config.json"),
			PasswordHashPath: filepath.Join(options.ProjectRoot, "secrets", "password.hash"),
			Entropy:          options.Entropy,
		})
	}
	return errors.New("用法：s12ryt-ipv6 [init|serve|status]")
}

func printProjectStatus(projectRoot string, output io.Writer, addresses func() ([]netip.Addr, error)) error {
	config, err := store.NewConfigStore(filepath.Join(projectRoot, "config", "config.json")).Load()
	if err != nil {
		return fmt.Errorf("讀取面板設定：%w", err)
	}
	password, err := readProtectedSecret(filepath.Join(projectRoot, "secrets", "management.password"), "管理密碼")
	if err != nil {
		return err
	}
	availableAddresses, err := addresses()
	if err != nil {
		return fmt.Errorf("取得管理面板位址：%w", err)
	}
	var ipv4URL, ipv6URL string
	for _, address := range availableAddresses {
		if !address.IsValid() || !address.IsGlobalUnicast() {
			continue
		}
		url := "http://" + net.JoinHostPort(address.String(), strconv.Itoa(config.Panel.Port)) + config.Panel.Path
		if address.Is4() && ipv4URL == "" {
			ipv4URL = url
		}
		if address.Is6() && ipv6URL == "" {
			ipv6URL = url
		}
	}
	if ipv4URL == "" {
		ipv4URL = "{未獲取到}"
	}
	if ipv6URL == "" {
		ipv6URL = "{未獲取到}"
	}
	fmt.Fprintf(output, "ipv4: %s\nipv6: %s\n管理密碼：%s\nWeb 路徑：%s\n管理埠：%d\n", ipv4URL, ipv6URL, password, config.Panel.Path, config.Panel.Port)
	return nil
}

func systemGlobalAddresses() ([]netip.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []netip.Addr
	seen := make(map[netip.Addr]struct{})
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, networkAddress := range addresses {
			prefix, err := netip.ParsePrefix(networkAddress.String())
			if err != nil {
				continue
			}
			address := prefix.Addr().Unmap()
			if !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() {
				continue
			}
			if _, exists := seen[address]; exists {
				continue
			}
			seen[address] = struct{}{}
			result = append(result, address)
		}
	}
	return result, nil
}

func initializeProject(options initializationOptions) (initializationResult, error) {
	if options.ProjectRoot == "" {
		options.ProjectRoot = defaultProjectRoot
	}
	if options.Entropy == nil {
		options.Entropy = rand.Reader
	}

	configPath := filepath.Join(options.ProjectRoot, "config", "config.json")
	passwordHashPath := filepath.Join(options.ProjectRoot, "secrets", "password.hash")
	plainPasswordPath := filepath.Join(options.ProjectRoot, "secrets", "management.password")
	for _, path := range []string{configPath, passwordHashPath, plainPasswordPath} {
		if _, err := os.Lstat(path); err == nil {
			return initializationResult{}, fmt.Errorf("初始化狀態已存在：%s", path)
		} else if !os.IsNotExist(err) {
			return initializationResult{}, fmt.Errorf("檢查初始化狀態 %s：%w", path, err)
		}
	}

	secrets, err := domain.GenerateBootstrapSecrets(options.Entropy)
	if err != nil {
		return initializationResult{}, fmt.Errorf("產生初始管理資料：%w", err)
	}
	passwordHash, err := auth.NewPasswordHasher(options.Entropy).Hash(secrets.Password)
	if err != nil {
		return initializationResult{}, fmt.Errorf("建立管理密碼雜湊：%w", err)
	}
	config := domain.DefaultConfig()
	config.Panel.Path = secrets.WebPath
	if err := store.NewConfigStore(configPath).Save(config); err != nil {
		return initializationResult{}, fmt.Errorf("建立初始設定：%w", err)
	}
	if err := writeProtectedPasswordHash(passwordHashPath, passwordHash); err != nil {
		cleanupErr := errors.Join(removeInitializationFile(configPath), removeInitializationFile(configPath+".bak"))
		if cleanupErr != nil {
			return initializationResult{}, errors.Join(fmt.Errorf("建立管理密碼雜湊：%w", err), fmt.Errorf("清理未完成設定：%w", cleanupErr))
		}
		return initializationResult{}, fmt.Errorf("建立管理密碼雜湊：%w", err)
	}
	if err := writeProtectedSecret(plainPasswordPath, secrets.Password, ".management-password.*"); err != nil {
		cleanupErr := errors.Join(
			removeInitializationFile(passwordHashPath),
			removeInitializationFile(configPath),
			removeInitializationFile(configPath+".bak"),
		)
		if cleanupErr != nil {
			return initializationResult{}, errors.Join(fmt.Errorf("保存管理密碼：%w", err), fmt.Errorf("清理未完成設定：%w", cleanupErr))
		}
		return initializationResult{}, fmt.Errorf("保存管理密碼：%w", err)
	}
	return initializationResult{Password: secrets.Password, WebPath: secrets.WebPath, Port: config.Panel.Port}, nil
}

func writeProtectedPasswordHash(path string, passwordHash string) error {
	return writeProtectedSecret(path, passwordHash, ".password-hash.*")
}

func writeProtectedSecret(path string, value string, temporaryPattern string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("建立機密目錄：%w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("保護機密目錄：%w", err)
	}
	temporary, err := os.CreateTemp(directory, temporaryPattern)
	if err != nil {
		return fmt.Errorf("建立機密暫存檔：%w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("保護機密暫存檔：%w", err)
	}
	if _, err := io.WriteString(temporary, value+"\n"); err != nil {
		temporary.Close()
		return fmt.Errorf("寫入機密暫存檔：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("同步機密暫存檔：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("關閉機密暫存檔：%w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("安裝機密檔案：%w", err)
	}
	if err := syncRuntimeDirectory(directory); err != nil {
		os.Remove(path)
		return fmt.Errorf("同步機密目錄：%w", err)
	}
	return nil
}

func removeInitializationFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func syncRuntimeDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
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
	nodeManager, err := nodes.NewManager(nodes.ManagerOptions{
		Config:  config,
		Store:   configStore,
		Entropy: options.Entropy,
		AllocatePort: func() (int, error) {
			return projectnetwork.AllocateNodePort(options.Entropy, options.PortChecker, options.PortAllocationAttempts)
		},
	})
	if err != nil {
		return application{}, fmt.Errorf("建立節點管理服務：%w", err)
	}
	server := panel.NewServer(panel.Options{
		BasePath:     config.Panel.Path,
		PasswordHash: passwordHash,
		Hasher:       hasher,
		Sessions:     sessions,
		Limiter:      limiter,
		Config:       config,
		Store:        configStore,
		NodeManager:  nodeManager,
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
	if options.PortChecker == nil {
		options.PortChecker = projectnetwork.NewSystemSocketPortChecker()
	}
	if options.PortAllocationAttempts == 0 {
		options.PortAllocationAttempts = 128
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

func readProtectedSecret(path string, label string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("讀取%s資訊：%w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s必須是一般檔案", label)
	}
	if info.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("%s權限必須是 0600，目前為 %04o", label, info.Mode().Perm())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("讀取%s：%w", label, err)
	}
	value := strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("%s不得為空", label)
	}
	return value, nil
}
