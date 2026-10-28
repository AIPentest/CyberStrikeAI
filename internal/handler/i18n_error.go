package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// respondError 按 UI 语言（Accept-Language / ?lang=）写 JSON 错误响应。
// 与 notification.go 的 requestWantsEnglish / i18nText 同一协议，供各 handler 的
// c.JSON(..., gin.H{"error": "中文"}) 错误文案国际化。
func respondError(c *gin.Context, status int, zh string, en string) {
	if c == nil {
		return
	}
	c.JSON(status, gin.H{"error": i18nText(requestWantsEnglish(c), zh, en)})
}

// respondErrorf 按语言格式化（%s/%d 等），用于含参数的错误文案。
func respondErrorf(c *gin.Context, status int, zh string, en string, args ...interface{}) {
	if c == nil {
		return
	}
	if len(args) == 0 {
		respondError(c, status, zh, en)
		return
	}
	if requestWantsEnglish(c) {
		c.JSON(status, gin.H{"error": fmt.Sprintf(en, args...)})
		return
	}
	c.JSON(status, gin.H{"error": fmt.Sprintf(zh, args...)})
}
