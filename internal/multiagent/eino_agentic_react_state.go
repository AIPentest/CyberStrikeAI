package multiagent

import (
	"context"
	"fmt"
	"reflect"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
)

// mutateADKReactState updates ChatModelAgent react state regardless of whether
// the live graph stores typedState[*schema.Message] or typedState[*schema.AgenticMessage].
// Eino v0.9.14 only exports SendToolGenAction against the classic Message state.
func mutateADKReactState(ctx context.Context, mutate func(st reflect.Value) error) error {
	if mutate == nil {
		return fmt.Errorf("adk react state mutate is nil")
	}
	return compose.ProcessState(ctx, func(_ context.Context, st any) error {
		if st == nil {
			return fmt.Errorf("adk react state is nil")
		}
		v := reflect.ValueOf(st)
		if v.Kind() != reflect.Pointer || v.IsNil() {
			return fmt.Errorf("unexpected adk react state type %T", st)
		}
		return mutate(v.Elem())
	})
}

func sendADKToolGenAction(ctx context.Context, toolName string, action *adk.AgentAction) error {
	if action == nil {
		return fmt.Errorf("adk tool gen action is nil")
	}
	key := toolName
	if toolCallID := compose.GetToolCallID(ctx); toolCallID != "" {
		key = toolCallID
	}
	return mutateADKReactState(ctx, func(st reflect.Value) error {
		field := st.FieldByName("ToolGenActions")
		if !field.IsValid() || field.Kind() != reflect.Map {
			return fmt.Errorf("adk react state missing ToolGenActions")
		}
		if !field.CanSet() {
			return fmt.Errorf("cannot set ToolGenActions on adk react state")
		}
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
		field.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(action))
		return nil
	})
}

func clearADKReturnDirectly(ctx context.Context) error {
	return mutateADKReactState(ctx, func(st reflect.Value) error {
		zeroExportedField(st, "ReturnDirectlyToolCallID")
		zeroExportedField(st, "HasReturnDirectly")
		zeroExportedField(st, "ReturnDirectlyEvent")
		return nil
	})
}

func zeroExportedField(st reflect.Value, name string) {
	field := st.FieldByName(name)
	if !field.IsValid() || !field.CanSet() {
		return
	}
	field.Set(reflect.Zero(field.Type()))
}
