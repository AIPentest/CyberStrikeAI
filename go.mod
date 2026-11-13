module cyberstrike-ai

// 若 go mod download 超时，可执行: go env -w GOPROXY=https://goproxy.cn,direct

go 1.25.0

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/bytedance/sonic v1.15.0
	github.com/creack/pty v1.1.24
	github.com/disintegration/imaging v1.6.2
	github.com/gin-gonic/gin v1.9.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/larksuite/oapi-sdk-go/v3 v3.4.22
	github.com/mattn/go-sqlite3 v1.14.18
	github.com/modelcontextprotocol/go-sdk v1.3.1
	github.com/open-dingtalk/dingtalk-stream-sdk-go v0.9.1
	github.com/pkoukk/tiktoken-go v0.1.8
	github.com/robfig/cron/v3 v3.0.1
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/slack-go/slack v0.27.0
	github.com/tencent-connect/botgo v0.2.1
	go.uber.org/zap v1.26.0
	golang.org/x/crypto v0.50.0
	golang.org/x/net v0.53.0
	golang.org/x/sys v0.43.0
	golang.org/x/term v0.42.0
	golang.org/x/text v0.37.0
	golang.org/x/time v0.14.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic/loader v0.5.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/dlclark/regexp2 v1.10.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.2 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.14.0 // indirect
	github.com/go-resty/resty/v2 v2.6.0 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/leodido/go-urn v1.2.4 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.11 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/arch v0.15.0 // indirect
	golang.org/x/image v0.0.0-20191009234506-e7c1f5e7dbb8 // indirect
	golang.org/x/oauth2 v0.34.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

// 修复钉钉 Stream SDK 在长连接断开（熄屏/网络中断）后 "panic: send on closed channel" 问题
// 详见: https://github.com/open-dingtalk/dingtalk-stream-sdk-go/issues/28
replace github.com/open-dingtalk/dingtalk-stream-sdk-go => github.com/uouuou/dingtalk-stream-sdk-go v0.0.0-20250626025113-079132acc406
