// Package finalshell 解析 FinalShell 的连接配置文件（conn/*_connect_config.json）。
// 仅提取非敏感字段：名称/主机/端口/用户名。FinalShell 的密码为同机加密
// 存储（不同版本分别为 DES 与 AES 两种算法），密钥文件（secret_key_id）
// 亦为私有格式，导入后需用户重新补录凭据（与 Xshell 导入策略一致）。
// 本包零内部依赖（依赖方向见 ARCHITECTURE.md）。
package finalshell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Preview 单个 FinalShell 连接的导入预览
type Preview struct {
	File         string `json:"file"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	AuthType     string `json:"authType"` // password | key
	KeyName      string `json:"keyName,omitempty"`
	HasProxy     bool   `json:"hasProxy"` // 配置了跳板（proxy_id 非空），导入后需手动配置跳板链
	Valid        bool   `json:"valid"`
	SkipReason   string `json:"skipReason,omitempty"`
}

// connectConfig FinalShell 连接配置 JSON 的兼容结构。
// port 在不同版本中可能为数字或字符串，故用 json.Number 接收。
type connectConfig struct {
	Name          string      `json:"name"`
	Host          string      `json:"host"`
	Port          json.Number `json:"port"`
	UserName      string      `json:"user_name"`
	Password      string      `json:"password"`
	ConectionType json.Number `json:"conection_type"`
	SecretKeyID   string      `json:"secret_key_id"`
	ProxyID       string      `json:"proxy_id"`
}

// ScanDir 扫描 FinalShell 配置目录（conn），返回按名称排序的预览列表。
// 解析失败的文件以 Valid=false + SkipReason 呈现，不中断整体导入。
func ScanDir(dir string) ([]Preview, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	var out []Preview
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_connect_config.json") {
			continue
		}
		out = append(out, parseFile(filepath.Join(dir, e.Name())))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parseFile(path string) Preview {
	base := filepath.Base(path)
	p := Preview{
		File: base,
		Port: 22,
		// 名称兜底：解析失败时用文件名（去掉 _connect_config.json 后缀）
		Name: strings.TrimSuffix(base, "_connect_config.json"),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		p.SkipReason = fmt.Sprintf("read: %v", err)
		return p
	}
	var cfg connectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		p.SkipReason = fmt.Sprintf("解析 JSON 失败: %v", err)
		return p
	}
	if n := strings.TrimSpace(cfg.Name); n != "" {
		p.Name = n
	}

	// conection_type：100 为 SSH（FinalShell 拼写如此）；其他类型跳过
	if cfg.ConectionType.String() != "" && cfg.ConectionType.String() != "100" {
		p.SkipReason = fmt.Sprintf("非 SSH 连接类型 (%s)", cfg.ConectionType)
		return p
	}
	if strings.TrimSpace(cfg.Host) == "" {
		p.SkipReason = "缺少 Host 字段"
		return p
	}
	p.Host = strings.TrimSpace(cfg.Host)
	if n, err := cfg.Port.Int64(); err == nil && n > 0 && n <= 65535 {
		p.Port = int(n)
	} else if s := cfg.Port.String(); s != "" {
		var n int
		if _, serr := fmt.Sscanf(s, "%d", &n); serr == nil && n > 0 && n <= 65535 {
			p.Port = n
		}
	}
	p.Username = strings.TrimSpace(cfg.UserName)
	if p.Username == "" {
		p.SkipReason = "缺少用户名"
		return p
	}
	if strings.TrimSpace(cfg.Name) != "" {
		p.Name = strings.TrimSpace(cfg.Name)
	} else {
		p.Name = p.Host
	}
	p.HasProxy = strings.TrimSpace(cfg.ProxyID) != ""
	if strings.TrimSpace(cfg.SecretKeyID) != "" {
		p.AuthType = "key"
		// 密钥本体为 FinalShell 私有格式，无法直接使用：仅提示，需用户重新指定本地私钥
		p.KeyName = "FinalShell 密钥（需重新指定本地私钥）"
	} else {
		p.AuthType = "password"
	}
	p.Valid = true
	return p
}
