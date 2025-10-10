package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"time"
)

// Config 应用配置
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Remote       RemoteConfig       `yaml:"remote"`
	Harbor       HarborConfig       `yaml:"harbor"`
	Callback     CallbackConfig     `yaml:"callback"`
	Web          WebConfig          `yaml:"web"`
	Whitelist    WhitelistConfig    `yaml:"whitelist"`
	Projects     ProjectsConfig     `yaml:"projects"`
	Deployment   DeploymentConfig   `yaml:"deployment"`
	Notification NotificationConfig `yaml:"notification"`
	TrafficProxy TrafficProxyConfig `yaml:"traffic_proxy"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port string `yaml:"port"`
}

// RemoteConfig 远程服务配置
type RemoteConfig struct {
	UpdateURL string `yaml:"update_url"`
}

// HarborConfig Harbor配置
type HarborConfig struct {
	Online          string `yaml:"online"`
	Offline         string `yaml:"offline"`
	OfflineUser     string `yaml:"offline_user"`
	OfflinePassword string `yaml:"offline_password"`
}

// SSHConfig SSH连接配置
type SSHConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	User    string `yaml:"user"`
	KeyFile string `yaml:"key_file"`
	Timeout int    `yaml:"timeout"`
}

// CallbackConfig 回调配置
type CallbackConfig struct {
	Domain string `yaml:"domain"`
	Path   string `yaml:"path"`
}

// WebConfig Web部署配置
type WebConfig struct {
	DownloadURL string `yaml:"download_url"`
	DownloadDir string `yaml:"download_dir"`
	WebDir      string `yaml:"web_dir"`
}

// WhitelistConfig IP白名单配置
type WhitelistConfig struct {
	Domains        []string `yaml:"domains"`
	UpdateInterval string   `yaml:"update_interval"`
}

// ProjectsConfig 项目配置
type ProjectsConfig struct {
	ValidNames []string `yaml:"valid_names"`
	WebKeyword string   `yaml:"web_keyword"`
}

// DeploymentConfig 部署配置
type DeploymentConfig struct {
	Double map[string]string `yaml:"double"` // 支持AB版本切换的项目
	Single map[string]string `yaml:"single"` // 单版本项目
}

// NotificationConfig 通知配置
type NotificationConfig struct {
	Enable         bool   `yaml:"enable"`
	NotifyURL      string `yaml:"notify_url"`
	EncryptionSalt string `yaml:"encryption_salt"`
}

// TrafficProxyConfig 流量代理配置
type TrafficProxyConfig struct {
	Enable   bool   `yaml:"enable"`
	ProxyURL string `yaml:"proxy_url"`
}

var AppConfig *Config

// LoadConfig 从YAML文件加载配置
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	AppConfig = config
	log.Printf("配置加载成功: %s", configPath)
	return AppConfig, nil
}

// GetEncryptionSalt 获取加密盐值
func GetEncryptionSalt() string {
	if AppConfig != nil && AppConfig.Notification.EncryptionSalt != "" {
		return AppConfig.Notification.EncryptionSalt
	}
	return "DqJHGSTaw11yWhyjhMmiX1hgd3AoYARg" // 默认值
}

// GetCallbackURL 获取完整的回调URL
func (c *Config) GetCallbackURL() string {
	return c.Callback.Domain + c.Callback.Path
}

// GetServerAddr 获取服务器监听地址
func (c *Config) GetServerAddr() string {
	return c.Server.Host + ":" + c.Server.Port
}

// GetUpdateInterval 获取更新间隔时间
func (c *Config) GetUpdateInterval() time.Duration {
	duration, err := time.ParseDuration(c.Whitelist.UpdateInterval)
	if err != nil {
		log.Printf("解析更新间隔失败，使用默认值5分钟: %v", err)
		return 5 * time.Minute
	}
	return duration
}

// ResolveWhitelistIPs 解析白名单域名为IP地址
func (c *Config) ResolveWhitelistIPs() []string {
	var ips []string
	for _, domain := range c.Whitelist.Domains {
		if ip := net.ParseIP(domain); ip != nil {
			// 如果已经是IP地址，直接添加
			ips = append(ips, domain)
		} else {
			// 解析域名
			if resolvedIPs, err := net.LookupIP(domain); err == nil {
				for _, ip := range resolvedIPs {
					if ipv4 := ip.To4(); ipv4 != nil {
						ips = append(ips, ipv4.String())
					}
				}
			} else {
				log.Printf("解析域名失败 %s: %v", domain, err)
			}
		}
	}
	return ips
}

// IsValidProject 检查项目名称是否有效
func (c *Config) IsValidProject(projectName string) bool {
	// 检查是否在有效项目列表中
	for _, project := range c.Projects.ValidNames {
		if projectName == project {
			return true
		}
	}
	// 检查是否包含web关键字
	if strings.Contains(projectName, c.Projects.WebKeyword) {
		return true
	}
	return false
}

// IsWebProject 判断是否为Web项目
func (c *Config) IsWebProject(projectName string) bool {
	return strings.Contains(projectName, c.Projects.WebKeyword)
}

// GetProjectPath 获取项目路径
func (c *Config) GetProjectPath(projectName string) (string, bool) {
	// 先检查double项目
	if path, exists := c.Deployment.Double[projectName]; exists {
		return path, true
	}
	// 再检查single项目
	if path, exists := c.Deployment.Single[projectName]; exists {
		return path, true
	}
	return "", false
}

// IsDoubleProject 判断是否为支持AB版本切换的项目
func (c *Config) IsDoubleProject(projectName string) bool {
	_, exists := c.Deployment.Double[projectName]
	return exists
}

// IsSingleProject 判断是否为单版本项目
func (c *Config) IsSingleProject(projectName string) bool {
	_, exists := c.Deployment.Single[projectName]
	return exists
}

// GetWebPath 根据项目名生成web路径
// ysh-web -> /www/ysh/web
// ysh-risk-web -> /www/ysh-risk/web
func (c *Config) GetWebPath(projectName string) string {
	// 去掉-web后缀
	project := strings.TrimSuffix(projectName, "-web")
	return c.Web.WebDir + project + "/web"
}

// GetWebDownloadURL 获取产物下载URL
func (c *Config) GetWebDownloadURL() string {
	return c.Web.DownloadURL
}
func (c *Config) GetWebDownloadDir() string {
	return c.Web.DownloadDir
}

// GetTrafficProxyURL 获取流量代理URL
func (c *Config) GetTrafficProxyURL() string {
	return c.TrafficProxy.ProxyURL
}

// GetTrafficProxyEnable 获取流量代理是否开启
func (c *Config) GetTrafficProxyEnable() bool {
	return c.TrafficProxy.Enable
}
