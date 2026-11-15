package agent

// GeneratedContextTypeKey marks host-created execution templates, separately
// from Agent's own summary/skill transport metadata.
const GeneratedContextTypeKey = "_csai_generated_context_type"
const PlanExecuteExecutorContext = "plan_execute_executor"
const PlanExecuteReplannerContext = "plan_execute_replanner"

func IsGeneratedExecutionContext(kind string) bool {
	return kind == PlanExecuteExecutorContext || kind == PlanExecuteReplannerContext
}
