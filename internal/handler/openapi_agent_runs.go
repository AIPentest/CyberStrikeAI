package handler

// addAgentRunSpec reuses each synchronous request schema without changing it.
func addAgentRunSpec(spec map[string]interface{}) {
	paths := spec["paths"].(map[string]interface{})
	for _, base := range []string{"/api/eino-agent", "/api/multi-agent"} {
		original := paths[base].(map[string]interface{})["post"].(map[string]interface{})
		paths[base+"/runs"] = map[string]interface{}{"post": map[string]interface{}{
			"tags": []string{"对话交互"}, "summary": "异步提交对话任务", "description": "接收后返回202，客户端断线不取消任务。必须提供按用户隔离的 Idempotency-Key；相同键和请求返回原回执，不同请求返回409。通过 statusUrl 查询最终结果；取消使用 /api/agent-loop/cancel。",
			"requestBody": original["requestBody"],
			"parameters":  []map[string]interface{}{{"in": "header", "name": "Idempotency-Key", "required": true, "schema": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 200}}},
			"responses":   map[string]interface{}{"202": map[string]interface{}{"description": "已接收：runId、conversationId、assistantMessageId、status、statusUrl；Location 为查询地址"}, "400": map[string]interface{}{"description": "参数或模式无效"}, "403": map[string]interface{}{"description": "无权限"}, "409": map[string]interface{}{"description": "幂等键载荷冲突或会话正在执行"}, "500": map[string]interface{}{"description": "接收记录失败"}},
		}}
		paths[base+"/runs/{runId}"] = map[string]interface{}{"get": map[string]interface{}{
			"tags": []string{"对话交互"}, "summary": "查询异步对话任务", "description": "返回持久状态、result、会话和消息标识及时间。服务重启后未完成任务为 interrupted，不自动重放。",
			"parameters": []map[string]interface{}{{"in": "path", "name": "runId", "required": true, "schema": map[string]interface{}{"type": "string"}}},
			"responses":  map[string]interface{}{"200": map[string]interface{}{"description": "任务状态与结果"}, "403": map[string]interface{}{"description": "无权访问对话"}, "404": map[string]interface{}{"description": "任务不存在"}},
		}}
	}
}
