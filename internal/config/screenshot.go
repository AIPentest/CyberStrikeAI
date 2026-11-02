package config

// ScreenshotConfig 站点截图配置。
//
// 截图由独立的本地 Node 服务（Playwright + Chromium）完成，本程序只负责
// 排队、落库与鉴权下发。浏览器因此永远以非 root 身份跑在独立进程里，
// 避免主服务（当前以 root 运行）直接渲染不可信目标站点带来的风险。
type ScreenshotConfig struct {
	// Enabled 是否启用站点截图（发现新的 http/https 站点时自动排队截图）。
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// ServiceURL 截图服务地址（localhost HTTP）。
	ServiceURL string `yaml:"service_url,omitempty" json:"service_url,omitempty"`
	// Dir 截图落盘目录（相对进程工作目录）。
	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`
	// TimeoutSeconds 单张截图的超时时间。
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	// Concurrency 并发截图数（浏览器渲染吃 CPU/内存，建议 1-3）。
	Concurrency int `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	// ViewportWidth/Height 截图画布尺寸。
	ViewportWidth  int `yaml:"viewport_width,omitempty" json:"viewport_width,omitempty"`
	ViewportHeight int `yaml:"viewport_height,omitempty" json:"viewport_height,omitempty"`
	// MinIntervalHours 同一站点两次自动截图的最小间隔（小时）。
	// 用指针区分「未配置」（用默认 24 小时）与「显式写 0」（每次都重截）。
	MinIntervalHours *int `yaml:"min_interval_hours,omitempty" json:"min_interval_hours,omitempty"`
}

const (
	defaultScreenshotServiceURL    = "http://127.0.0.1:8099"
	defaultScreenshotDir           = "data/screenshots"
	defaultScreenshotTimeout       = 30
	defaultScreenshotConcurrency   = 2
	defaultScreenshotViewportW     = 1280
	defaultScreenshotViewportH     = 800
	defaultScreenshotMinIntervalHr = 24
)

// EffectiveScreenshot 返回补全默认值后的截图配置。
func (c *Config) EffectiveScreenshot() ScreenshotConfig {
	out := c.Screenshot
	if out.ServiceURL == "" {
		out.ServiceURL = defaultScreenshotServiceURL
	}
	if out.Dir == "" {
		out.Dir = defaultScreenshotDir
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = defaultScreenshotTimeout
	}
	if out.Concurrency <= 0 {
		out.Concurrency = defaultScreenshotConcurrency
	}
	if out.ViewportWidth <= 0 {
		out.ViewportWidth = defaultScreenshotViewportW
	}
	if out.ViewportHeight <= 0 {
		out.ViewportHeight = defaultScreenshotViewportH
	}
	if out.MinIntervalHours == nil {
		// 未配置时默认 24 小时；显式写 0 表示每次都重截（保持用户意图）。
		hours := defaultScreenshotMinIntervalHr
		out.MinIntervalHours = &hours
	}
	if out.Enabled == nil {
		// 未显式配置时默认开启；启动时会探测截图服务，不可用则自动降级为不自动截图。
		enabled := true
		out.Enabled = &enabled
	}
	return out
}

// ScreenshotEnabled 返回是否开启自动截图。
func (c *Config) ScreenshotEnabled() bool {
	enabled := c.EffectiveScreenshot().Enabled
	return enabled != nil && *enabled
}
