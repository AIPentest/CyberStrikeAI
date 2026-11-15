package multiagent

import (
	"cyberstrike-ai/internal/agents"
	"cyberstrike-ai/internal/config"
	"strings"
)

func DefaultPlanExecuteOrchestratorInstruction() string {
	return `你是 CyberStrikeAI 的 Pi 计划执行协调者。
将用户目标、已知范围、约束和验收要求整理为少量可执行步骤。使用可用的任务板工具记录进度，通过 task 委派 executor 执行具体步骤，并根据真实工具结果更新后续计划。
每次委派说明目标、已知证据、允许的操作与期望输出。不要把计划、尝试和已完成的工作混为一谈；记录执行 ID、未解决的问题和覆盖缺口。
优先使用已注册的工具和匹配的技能。工具失败时区分参数、网络、服务与权限原因；遵守预算和人工决定。已有证据可复用，避免重复有副作用的操作。
需要用户作出关键取舍或补充无法可靠推断的信息时，说明已完成的工作、阻碍及具体选项。最终只交付已验证的结果；任务未完成时明确说明剩余工作。`
}

func DefaultSupervisorOrchestratorInstruction() string {
	return `你是 CyberStrikeAI 的 Pi 专家协调者。
根据用户目标和已注册的专家目录选择合适的子代理，通过 task 或 transfer_to_agent 委派有边界的工作。每个子代理返回后核对其证据和未完成项，必要时安排后续步骤。
你可以在既定工具范围内完成补充检查。子代理不得扩大主任务范围、工具权限或预算；人工决定与用户更正必须传递下去。
保持目标、工作进度、观察结果和推测的区别。优先使用已有执行证据，不因单次工具失败就宣布整个任务完成，也不盲目重复失败操作。
阶段性说明完成项、剩余项和下一步；遇到必须由用户决定的事项，提出具体问题。交付经过核对的最终结果时使用 exit，不能将纯文字声明代替证据。`
}

// resolveMainOrchestratorInstruction 按编排模式解析主代理系统提示与可选的 Markdown 元数据（name/description）。plan_execute / supervisor **不**回退到 Deep 的 orchestrator_instruction，避免混用提示词。
func resolveMainOrchestratorInstruction(mode string, ma *config.MultiAgentConfig, markdownLoad *agents.MarkdownDirLoad) (instruction string, meta *agents.OrchestratorMarkdown) {
	if ma == nil {
		return "", nil
	}
	switch mode {
	case "plan_execute":
		if markdownLoad != nil && markdownLoad.OrchestratorPlanExecute != nil {
			meta = markdownLoad.OrchestratorPlanExecute
			if s := strings.TrimSpace(meta.Instruction); s != "" {
				return s, meta
			}
		}
		if s := strings.TrimSpace(ma.OrchestratorInstructionPlanExecute); s != "" {
			if markdownLoad != nil {
				meta = markdownLoad.OrchestratorPlanExecute
			}
			return s, meta
		}
		if markdownLoad != nil {
			meta = markdownLoad.OrchestratorPlanExecute
		}
		return DefaultPlanExecuteOrchestratorInstruction(), meta
	case "supervisor":
		if markdownLoad != nil && markdownLoad.OrchestratorSupervisor != nil {
			meta = markdownLoad.OrchestratorSupervisor
			if s := strings.TrimSpace(meta.Instruction); s != "" {
				return s, meta
			}
		}
		if s := strings.TrimSpace(ma.OrchestratorInstructionSupervisor); s != "" {
			if markdownLoad != nil {
				meta = markdownLoad.OrchestratorSupervisor
			}
			return s, meta
		}
		if markdownLoad != nil {
			meta = markdownLoad.OrchestratorSupervisor
		}
		return DefaultSupervisorOrchestratorInstruction(), meta
	default: // deep
		if markdownLoad != nil && markdownLoad.Orchestrator != nil {
			meta = markdownLoad.Orchestrator
			if s := strings.TrimSpace(markdownLoad.Orchestrator.Instruction); s != "" {
				return s, meta
			}
		}
		return strings.TrimSpace(ma.OrchestratorInstruction), meta
	}
}
