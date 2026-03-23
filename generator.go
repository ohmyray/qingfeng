// Package qingfeng provides a beautiful Swagger UI replacement for Go web frameworks
package qingfeng

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/swaggo/swag/v2/gen"
)

// GeneratorType 文档生成器类型
type GeneratorType string

const (
	// GeneratorBuiltin 内置生成器（默认，使用 swag v2，支持 OpenAPI 3.0）
	GeneratorBuiltin GeneratorType = "builtin"
	// GeneratorNone 不自动生成，使用 DocPath 或 DocJSON
	GeneratorNone GeneratorType = "none"
)

// generatorConfig 内部生成器配置
type generatorConfig struct {
	SearchDir   string
	MainFile    string
	OutputDir   string
	OpenAPI3    bool
	ParseDepth  int
	Title       string
	Description string
	Version     string
	Host        string
	BasePath    string
}

// generateSpec 使用内置生成器生成 OpenAPI 文档
func generateSpec(cfg Config) ([]byte, error) {
	genCfg := generatorConfig{
		SearchDir:   cfg.SwagSearchDir,
		MainFile:    "main.go",
		OutputDir:   cfg.SwagOutputDir,
		OpenAPI3:    true, // 默认生成 OpenAPI 3.0
		ParseDepth:  100,
		Title:       cfg.Title,
		Description: cfg.Description,
		Version:     cfg.Version,
	}

	if genCfg.SearchDir == "" {
		genCfg.SearchDir = "."
	}
	if genCfg.OutputDir == "" {
		genCfg.OutputDir = "./docs"
	}

	return runBuiltinGenerator(genCfg)
}

// runBuiltinGenerator 运行内置的 swag 生成器
func runBuiltinGenerator(cfg generatorConfig) ([]byte, error) {
	log.Println("[QingFeng] 正在生成 API 文档...")

	// 确保输出目录存在
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}

	// 配置 swag 生成器
	genConfig := &gen.Config{
		SearchDir:          cfg.SearchDir,
		MainAPIFile:        cfg.MainFile,
		OutputDir:          cfg.OutputDir,
		OutputTypes:        []string{"json"}, // 只生成 JSON
		PropNamingStrategy: "camelcase",
		ParseDepth:         cfg.ParseDepth,
		ParseDependency:    1, // 解析依赖中的 models
		GenerateOpenAPI3Doc: cfg.OpenAPI3,
		ParseGoList:        true,
	}

	g := gen.New()
	if err := g.Build(genConfig); err != nil {
		return nil, fmt.Errorf("生成文档失败: %w", err)
	}

	// 读取生成的文件
	var specPath string
	if cfg.OpenAPI3 {
		specPath = filepath.Join(cfg.OutputDir, "openapi.json")
		// 如果 openapi.json 不存在，尝试 swagger.json
		if _, err := os.Stat(specPath); os.IsNotExist(err) {
			specPath = filepath.Join(cfg.OutputDir, "swagger.json")
		}
	} else {
		specPath = filepath.Join(cfg.OutputDir, "swagger.json")
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("读取生成的文档失败: %w", err)
	}

	// 如果配置了标题等信息，更新文档
	if cfg.Title != "" || cfg.Description != "" || cfg.Version != "" {
		data = updateSpecInfo(data, cfg)
	}

	log.Printf("[QingFeng] API 文档生成成功: %s\n", specPath)
	return data, nil
}

// updateSpecInfo 更新文档的基本信息
func updateSpecInfo(data []byte, cfg generatorConfig) []byte {
	var spec map[string]interface{}
	if err := json.Unmarshal(data, &spec); err != nil {
		return data
	}

	// 获取或创建 info 对象
	info, ok := spec["info"].(map[string]interface{})
	if !ok {
		info = make(map[string]interface{})
		spec["info"] = info
	}

	// 更新信息
	if cfg.Title != "" {
		info["title"] = cfg.Title
	}
	if cfg.Description != "" {
		info["description"] = cfg.Description
	}
	if cfg.Version != "" {
		info["version"] = cfg.Version
	}

	updated, err := json.Marshal(spec)
	if err != nil {
		return data
	}
	return updated
}

// detectSpecFormat 检测文档格式（Swagger 2.0 或 OpenAPI 3.x）
func detectSpecFormat(data []byte) string {
	var spec map[string]interface{}
	if err := json.Unmarshal(data, &spec); err != nil {
		return "unknown"
	}

	if _, ok := spec["openapi"]; ok {
		return "openapi3"
	}
	if _, ok := spec["swagger"]; ok {
		return "swagger2"
	}
	return "unknown"
}

// convertSwagger2ToOpenAPI3 将 Swagger 2.0 转换为 OpenAPI 3.0（简单转换）
func convertSwagger2ToOpenAPI3(data []byte) ([]byte, error) {
	var swagger2 map[string]interface{}
	if err := json.Unmarshal(data, &swagger2); err != nil {
		return nil, err
	}

	// 检查是否已经是 OpenAPI 3.x
	if _, ok := swagger2["openapi"]; ok {
		return data, nil
	}

	openapi3 := make(map[string]interface{})
	openapi3["openapi"] = "3.0.3"

	// 复制 info
	if info, ok := swagger2["info"]; ok {
		openapi3["info"] = info
	}

	// 转换 host + basePath + schemes 为 servers
	var servers []map[string]interface{}
	host := getString(swagger2, "host")
	basePath := getString(swagger2, "basePath")
	schemes := getStringArray(swagger2, "schemes")

	if host != "" {
		if len(schemes) == 0 {
			schemes = []string{"https"}
		}
		for _, scheme := range schemes {
			url := fmt.Sprintf("%s://%s%s", scheme, host, basePath)
			servers = append(servers, map[string]interface{}{
				"url": url,
			})
		}
		openapi3["servers"] = servers
	}

	// 复制 paths（需要转换 body 参数为 requestBody）
	if paths, ok := swagger2["paths"].(map[string]interface{}); ok {
		openapi3["paths"] = convertPaths(paths)
	}

	// 转换 definitions 为 components/schemas
	if definitions, ok := swagger2["definitions"].(map[string]interface{}); ok {
		openapi3["components"] = map[string]interface{}{
			"schemas": definitions,
		}
	}

	// 复制 tags
	if tags, ok := swagger2["tags"]; ok {
		openapi3["tags"] = tags
	}

	return json.Marshal(openapi3)
}

// convertPaths 转换 paths 中的参数格式
func convertPaths(paths map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for path, methods := range paths {
		methodsMap, ok := methods.(map[string]interface{})
		if !ok {
			result[path] = methods
			continue
		}

		convertedMethods := make(map[string]interface{})
		for method, operation := range methodsMap {
			opMap, ok := operation.(map[string]interface{})
			if !ok {
				convertedMethods[method] = operation
				continue
			}
			convertedMethods[method] = convertOperation(opMap)
		}
		result[path] = convertedMethods
	}

	return result
}

// convertOperation 转换单个操作
func convertOperation(op map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// 复制大部分字段
	for k, v := range op {
		if k != "parameters" && k != "consumes" && k != "produces" {
			result[k] = v
		}
	}

	// 处理 parameters，分离 body 参数
	if params, ok := op["parameters"].([]interface{}); ok {
		var newParams []interface{}
		var requestBody map[string]interface{}

		for _, p := range params {
			param, ok := p.(map[string]interface{})
			if !ok {
				newParams = append(newParams, p)
				continue
			}

			if getString(param, "in") == "body" {
				// 转换为 requestBody
				schema := param["schema"]
				requestBody = map[string]interface{}{
					"required": param["required"],
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": schema,
						},
					},
				}
				if desc := getString(param, "description"); desc != "" {
					requestBody["description"] = desc
				}
			} else {
				// 转换其他参数格式
				newParam := make(map[string]interface{})
				for k, v := range param {
					if k == "type" || k == "format" || k == "items" || k == "enum" || k == "default" {
						// 这些移到 schema 里
						if newParam["schema"] == nil {
							newParam["schema"] = make(map[string]interface{})
						}
						newParam["schema"].(map[string]interface{})[k] = v
					} else {
						newParam[k] = v
					}
				}
				newParams = append(newParams, newParam)
			}
		}

		if len(newParams) > 0 {
			result["parameters"] = newParams
		}
		if requestBody != nil {
			result["requestBody"] = requestBody
		}
	}

	// 转换 responses
	if responses, ok := op["responses"].(map[string]interface{}); ok {
		result["responses"] = convertResponses(responses)
	}

	return result
}

// convertResponses 转换响应格式
func convertResponses(responses map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for code, resp := range responses {
		respMap, ok := resp.(map[string]interface{})
		if !ok {
			result[code] = resp
			continue
		}

		newResp := make(map[string]interface{})
		if desc := getString(respMap, "description"); desc != "" {
			newResp["description"] = desc
		} else {
			newResp["description"] = "Response"
		}

		if schema, ok := respMap["schema"]; ok {
			newResp["content"] = map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": convertSchemaRef(schema),
				},
			}
		}

		result[code] = newResp
	}

	return result
}

// convertSchemaRef 转换 schema 引用
func convertSchemaRef(schema interface{}) interface{} {
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}

	result := make(map[string]interface{})
	for k, v := range schemaMap {
		if k == "$ref" {
			// 转换引用路径
			ref := v.(string)
			ref = strings.Replace(ref, "#/definitions/", "#/components/schemas/", 1)
			result["$ref"] = ref
		} else if k == "items" {
			result["items"] = convertSchemaRef(v)
		} else if k == "allOf" || k == "oneOf" || k == "anyOf" {
			if arr, ok := v.([]interface{}); ok {
				var newArr []interface{}
				for _, item := range arr {
					newArr = append(newArr, convertSchemaRef(item))
				}
				result[k] = newArr
			}
		} else if k == "properties" {
			if props, ok := v.(map[string]interface{}); ok {
				newProps := make(map[string]interface{})
				for pk, pv := range props {
					newProps[pk] = convertSchemaRef(pv)
				}
				result["properties"] = newProps
			}
		} else {
			result[k] = v
		}
	}

	return result
}

// 辅助函数
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringArray(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]interface{}); ok {
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// silentLogger 静默日志器
type silentLogger struct{}

func (s *silentLogger) Printf(format string, v ...interface{}) {}
