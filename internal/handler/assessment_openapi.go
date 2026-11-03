package handler

func addAssessmentSpec(spec map[string]interface{}) {
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		return
	}
	op := func(summary, permission string, params ...string) map[string]interface{} {
		parameters := []map[string]interface{}{}
		for _, name := range params {
			parameters = append(parameters, map[string]interface{}{"in": "path", "name": name, "required": true, "schema": map[string]interface{}{"type": "string"}})
		}
		return map[string]interface{}{"tags": []string{"Deep assessments"}, "summary": summary, "security": []map[string]interface{}{{"bearerAuth": []string{}}}, "x-csai-permission": permission, "parameters": parameters, "responses": map[string]interface{}{"200": map[string]interface{}{"description": "Success"}, "400": map[string]interface{}{"description": "Invalid input or budget unavailable"}, "403": map[string]interface{}{"description": "Permission or resource access denied"}, "409": map[string]interface{}{"description": "Conflicting task state"}}}
	}
	body := func(operation map[string]interface{}, schema map[string]interface{}) {
		operation["requestBody"] = map[string]interface{}{"required": true, "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": schema}}}
	}
	str := func() map[string]interface{} { return map[string]interface{}{"type": "string"} }
	create := op("Create a durable assessment draft (no conversation)", "assessment:write")
	create["responses"].(map[string]interface{})["201"] = map[string]interface{}{"description": "Draft created"}
	body(create, map[string]interface{}{"type": "object", "required": []string{"name", "goal", "scope", "tools"}, "properties": map[string]interface{}{
		"criteria": map[string]interface{}{"type": "array", "items": str(), "maxItems": 12}, "reviewer_model": str(), "skill_ids": map[string]interface{}{"type": "array", "items": str(), "maxItems": 4}, "worker_models": map[string]interface{}{"type": "object", "additionalProperties": str()},
		"name": str(), "goal": str(), "scope": str(), "material": str(), "project_id": str(), "model": str(),
		"approval_mode":     map[string]interface{}{"type": "string", "enum": []string{"policy", "manual"}},
		"tools":             map[string]interface{}{"type": "array", "items": str(), "minItems": 1, "maxItems": 100},
		"concurrency":       map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 4, "default": 2},
		"token_budget":      map[string]interface{}{"type": "integer", "minimum": 4000, "maximum": 10000000, "default": 200000},
		"remaining_seconds": map[string]interface{}{"type": "integer", "minimum": 60, "maximum": 86400, "default": 7200},
		"max_rounds":        map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "default": 12},
	}})
	list := op("List visible assessment summaries", "assessment:read")
	list["parameters"] = []map[string]interface{}{{"in": "query", "name": "limit", "schema": map[string]interface{}{"type": "integer", "default": 50, "maximum": 100}}, {"in": "query", "name": "offset", "schema": map[string]interface{}{"type": "integer", "default": 0}}}
	paths["/api/assessments"] = map[string]interface{}{"get": list, "post": create}
	paths["/api/assessments/options"] = map[string]interface{}{"get": op("List model channels and conversation-independent tools", "assessment:read")}
	paths["/api/assessments/{id}"] = map[string]interface{}{"get": op("Get assessment without private model transcript", "assessment:read", "id"), "delete": op("Delete a settled assessment and its events", "assessment:delete", "id")}
	control := op("Control: start, resume, pause, stop, steer or budget (start also requires agent:execute)", "assessment:write", "id", "action")
	body(control, map[string]interface{}{"type": "object", "properties": map[string]interface{}{"message": str(), "token_budget": map[string]interface{}{"type": "integer"}, "remaining_seconds": map[string]interface{}{"type": "integer"}, "max_rounds": map[string]interface{}{"type": "integer"}}})
	paths["/api/assessments/{id}/control/{action}"] = map[string]interface{}{"post": control}
	decision := op("Decide one exact tool call (also requires hitl:write)", "assessment:write", "id", "stepId")
	body(decision, map[string]interface{}{"type": "object", "required": []string{"approved", "approval_id"}, "properties": map[string]interface{}{"approval_id": str(), "approved": map[string]interface{}{"type": "boolean"}}})
	paths["/api/assessments/{id}/steps/{stepId}/decision"] = map[string]interface{}{"post": decision}
	paths["/api/assessments/{id}/steps/{stepId}/retry"] = map[string]interface{}{"post": op("Explicitly retry blocked work after reviewing partial effects", "assessment:write", "id", "stepId")}
	events := op("Read up to 200 durable events after a cursor (approximately 1 MiB per page)", "assessment:read", "id")
	events["parameters"] = append(events["parameters"].([]map[string]interface{}), map[string]interface{}{"in": "query", "name": "after", "schema": map[string]interface{}{"type": "integer", "format": "int64", "default": 0}})
	paths["/api/assessments/{id}/events"] = map[string]interface{}{"get": events}
	publish := op("Publish reviewed evidence (also requires destination write permissions)", "assessment:write", "id")
	body(publish, map[string]interface{}{"type": "object", "required": []string{"step_id", "index", "kind", "revision"}, "properties": map[string]interface{}{"step_id": str(), "revision": str(), "index": map[string]interface{}{"type": "integer", "minimum": 0}, "kind": map[string]interface{}{"type": "string", "enum": []string{"fact", "asset", "vulnerability"}}}})
	paths["/api/assessments/{id}/publish"] = map[string]interface{}{"post": publish}
	report := op("Download an evidence-based Markdown report, including partial results", "assessment:read", "id")
	report["responses"].(map[string]interface{})["200"] = map[string]interface{}{"description": "Markdown report", "content": map[string]interface{}{"text/markdown": map[string]interface{}{"schema": str()}}}
	paths["/api/assessments/{id}/metrics"] = map[string]interface{}{"get": op("Download reproducible evaluation metrics", "assessment:read", "id")}
	paths["/api/assessments/{id}/report"] = map[string]interface{}{"get": report}
}
