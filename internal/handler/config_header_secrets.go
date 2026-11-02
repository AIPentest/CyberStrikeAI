package handler

import (
	"fmt"
	"strings"
)

// Any private header name may carry a credential. A fixed list such as
// Authorization/X-Api-Key cannot safely classify a user-defined header map.
func transformHeaderSecrets(headers, stored map[string]interface{}, restoring bool) error {
	seen := map[string]bool{}
	for name, value := range headers {
		key := strings.ToLower(strings.TrimSpace(name))
		if restoring && seen[key] {
			return fmt.Errorf("重复的请求头名称")
		}
		seen[key] = true
		text, ok := value.(string)
		if !ok {
			continue
		}
		if !restoring {
			if text != "" {
				headers[name] = maskedSecret
			}
			continue
		}
		if text != maskedSecret {
			continue
		}
		found := ""
		count := 0
		for oldName, oldValue := range stored {
			if strings.EqualFold(strings.TrimSpace(oldName), strings.TrimSpace(name)) {
				if oldText, ok := oldValue.(string); ok {
					found = oldText
					count++
				}
			}
		}
		if count != 1 || found == "" || found == maskedSecret {
			return fmt.Errorf("没有唯一的已保存请求头凭据，请填写新的值")
		}
		headers[name] = found
	}
	return nil
}
