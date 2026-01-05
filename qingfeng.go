// Package qingfeng provides a beautiful Swagger UI replacement for Go web frameworks
// 青锋 - 青出于蓝，锋芒毕露
package qingfeng

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
)

// Version is the current version of QingFeng
const Version = "1.6.1"

//go:embed ui/default/* ui/minimal/* ui/modern/* ui/assets/css/* ui/assets/webfonts/*
var uiFS embed.FS

// UITheme represents available UI themes
// UI 主题类型
type UITheme string

const (
	// ThemeDefault is the default theme (原默认主题)
	ThemeDefault UITheme = "default"
	// ThemeMinimal is a minimal/clean theme (简约主题)
	ThemeMinimal UITheme = "minimal"
	// ThemeModern is a modern theme with gradients (现代主题)
	ThemeModern UITheme = "modern"
)

// Header represents a custom HTTP header with key-value pair
// 自定义 HTTP 请求头
type Header struct {
	// Key is the header name (e.g., "Authorization", "X-API-Key")
	Key string `json:"key"`
	// Value is the header value (e.g., "Bearer xxx", "your-api-key")
	Value string `json:"value"`
}

// Environment represents a deployment environment configuration
// 环境配置
type Environment struct {
	// Name is the display name (e.g., "开发环境", "Production")
	Name string `json:"name"`
	// BaseURL is the API base URL for this environment
	BaseURL string `json:"baseUrl"`
}

// Config holds the configuration for knife4j UI
type Config struct {
	// Title of the API documentation
	Title string
	// Description of the API
	Description string
	// Version of the API
	Version string
	// BasePath prefix for the documentation routes
	BasePath string
	// DocPath is the path to swagger.json file (swagger.json 文件路径)
	DocPath string
	// DocJSON allows passing swagger spec directly as JSON bytes
	DocJSON []byte
	// EnableDebug enables the API debug/testing feature
	EnableDebug bool
	// DarkMode enables dark theme by default
	DarkMode bool
	// PersistParams enables saving debug parameters to sessionStorage (default: true)
	// 是否将调试参数保存到 sessionStorage（默认: true）
	PersistParams *bool
	// GlobalHeaders are custom headers that will be sent with every API request
	// 全局请求头，会在每个 API 请求中自动添加
	GlobalHeaders []Header
	// AutoGenerate automatically runs swag init on startup (启动时自动生成 swagger 文档)
	AutoGenerate bool
	// SwagSearchDir is the directory to search for swagger comments (default: ".")
	// swag 搜索目录，默认为当前目录
	SwagSearchDir string
	// SwagOutputDir is the output directory for swagger files (default: "./docs")
	// swagger 文件输出目录，默认为 ./docs
	SwagOutputDir string
	// SwagArgs is additional arguments for swag init command
	// swag init 的额外参数，如 []string{"--parseDependency", "--parseInternal"}
	SwagArgs []string
	// UITheme selects the UI theme: "default", "minimal", "modern" (UI 主题选择)
	UITheme UITheme
	// Logo is the URL or base64 of custom logo image (自定义 Logo)
	Logo string
	// LogoLink is the URL to navigate when clicking the logo (Logo 点击跳转链接)
	LogoLink string
	// Environments is a list of environment configurations for switching baseUrl
	// 环境配置列表，用于切换不同环境的 baseUrl
	Environments []Environment
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		Title:       "API Documentation",
		Description: "API Documentation powered by wdc (青锋)",
		Version:     "1.0.0",
		BasePath:    "/doc",
		DocPath:     "./docs/swagger.json",
		EnableDebug: true,
		DarkMode:    false,
	}
}

// Handler returns a Gin handler for QingFeng UI
// 返回 Gin 框架的 handler，内部调用 HTTPHandler
func Handler(cfg Config) gin.HandlerFunc {
	httpHandler := HTTPHandler(cfg)
	
	return func(c *gin.Context) {
		httpHandler.ServeHTTP(c.Writer, c.Request)
	}
}

// RegisterRoutes registers QingFeng routes to a Gin router group
func RegisterRoutes(router *gin.RouterGroup, cfg Config) {
	handler := Handler(cfg)
	router.GET("/*filepath", handler)
}

// HTTPHandler returns a standard http.Handler for use with any Go web framework
// 返回标准 http.Handler，可用于任何 Go Web 框架
// 
// 使用示例:
//   - 标准库: http.Handle("/doc/", qingfeng.HTTPHandler(cfg))
//   - Echo: e.GET("/doc/*", echo.WrapHandler(qingfeng.HTTPHandler(cfg)))
//   - Fiber: app.Use("/doc", adaptor.HTTPHandler(qingfeng.HTTPHandler(cfg)))
//   - Chi: r.Handle("/doc/*", qingfeng.HTTPHandler(cfg))
func HTTPHandler(cfg Config) http.Handler {
	if cfg.BasePath == "" {
		cfg.BasePath = "/doc"
	}
	if cfg.DocPath == "" {
		cfg.DocPath = "./docs/swagger.json"
	}

	// Auto generate swagger docs if enabled
	if cfg.AutoGenerate {
		generateSwaggerDocs(cfg)
	}

	// Prepare file servers for each theme
	defaultFS, _ := fs.Sub(uiFS, "ui/default")
	minimalFS, _ := fs.Sub(uiFS, "ui/minimal")
	modernFS, _ := fs.Sub(uiFS, "ui/modern")
	assetsFS, _ := fs.Sub(uiFS, "ui/assets")

	fileServers := map[string]http.Handler{
		"default": http.FileServer(http.FS(defaultFS)),
		"minimal": http.FileServer(http.FS(minimalFS)),
		"modern":  http.FileServer(http.FS(modernFS)),
	}
	assetsServer := http.FileServer(http.FS(assetsFS))

	// Default theme from config
	defaultTheme := string(cfg.UITheme)
	if defaultTheme == "" {
		defaultTheme = "default"
	}

	// PersistParams default to true
	persistParams := true
	if cfg.PersistParams != nil {
		persistParams = *cfg.PersistParams
	}

	// Prepare config JSON for frontend
	configJSON, _ := json.Marshal(map[string]interface{}{
		"title":           cfg.Title,
		"description":     cfg.Description,
		"version":         cfg.Version,
		"enableDebug":     cfg.EnableDebug,
		"darkMode":        cfg.DarkMode,
		"globalHeaders":   cfg.GlobalHeaders,
		"defaultTheme":    defaultTheme,
		"themes":          []string{"default", "minimal", "modern"},
		"qingfengVersion": Version,
		"logo":            cfg.Logo,
		"logoLink":        cfg.LogoLink,
		"environments":    cfg.Environments,
		"persistParams":   persistParams,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Remove base path prefix
		if cfg.BasePath != "" && cfg.BasePath != "/" {
			path = strings.TrimPrefix(path, cfg.BasePath)
		}
		if path == "" {
			path = "/"
		}

		// Get theme from query parameter or use default
		theme := r.URL.Query().Get("theme")
		if theme == "" {
			theme = defaultTheme
		}
		if _, ok := fileServers[theme]; !ok {
			theme = defaultTheme
		}

		// Serve swagger.json
		if path == "/swagger.json" || path == "/api-docs" || path == "/doc.json" {
			w.Header().Set("Content-Type", "application/json")
			if cfg.DocJSON != nil {
				w.Write(cfg.DocJSON)
				return
			}
			data, err := os.ReadFile(cfg.DocPath)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error": "swagger.json not found"}`))
				return
			}
			w.Write(data)
			return
		}

		// Serve config
		if path == "/config.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(configJSON)
			return
		}

		// Serve assets (CSS, fonts)
		if strings.HasPrefix(path, "/assets/") {
			r.URL.Path = strings.TrimPrefix(path, "/assets")
			assetsServer.ServeHTTP(w, r)
			return
		}

		// Serve static files using selected theme
		r.URL.Path = path
		fileServers[theme].ServeHTTP(w, r)
	})
}

// generateSwaggerDocs runs swag init to generate swagger documentation
// 运行 swag init 生成 swagger 文档
func generateSwaggerDocs(cfg Config) {
	// Check if swag is installed
	_, err := exec.LookPath("swag")
	if err != nil {
		log.Println("[QingFeng] swag command not found, skipping auto-generation. Install with: go install github.com/swaggo/swag/cmd/swag@latest")
		log.Println("[QingFeng] swag 未找到, 跳过更新Swagger. 安装命令: go install github.com/swaggo/swag/cmd/swag@latest")
		return
	}

	searchDir := cfg.SwagSearchDir
	if searchDir == "" {
		searchDir = "."
	}

	outputDir := cfg.SwagOutputDir
	if outputDir == "" {
		outputDir = "./docs"
	}

	log.Println("[QingFeng] Auto-generating swagger docs...")

	// Build command arguments
	args := []string{"init", "-d", searchDir, "-o", outputDir}
	
	// Append custom arguments
	if len(cfg.SwagArgs) > 0 {
		args = append(args, cfg.SwagArgs...)
		log.Printf("[QingFeng] Using custom swag args: %v\n", cfg.SwagArgs)
	}

	cmd := exec.Command("swag", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Printf("[QingFeng] Failed to generate swagger docs: %v\n", err)
		return
	}

	log.Println("[QingFeng] Swagger docs generated successfully!")
}
