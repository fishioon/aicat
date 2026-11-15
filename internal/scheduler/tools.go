package scheduler

func AgentTools() []any {
	id := map[string]any{"type": "string", "pattern": "^[a-z0-9_-]{1,64}$"}
	spec := map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string"}, "prompt": map[string]any{"type": "string"},
		"at":       map[string]any{"type": "string", "description": "Single future RFC3339 timestamp with UTC offset. Mutually exclusive with cron."},
		"cron":     map[string]any{"type": "string", "description": "Five fields: minute hour day-of-month month day-of-week, e.g. 0 18 * * * for daily at 18:00. Requires timezone."},
		"timezone": map[string]any{"type": "string", "description": "Explicit IANA timezone, e.g. Asia/Taipei. Only for cron."},
		"enabled":  map[string]any{"type": "boolean"},
	}, "required": []string{"name", "prompt", "enabled"}, "additionalProperties": false}
	makeTool := func(name, description string, properties map[string]any, required []string) any {
		return map[string]any{"type": "function", "name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
	}
	return []any{
		makeTool("schedules_set", "Create or replace a scheduled task in the current Room. Reuse its id to modify or pause it. At the due time a system message asks this Room Agent to execute prompt. This does not itself send to the platform. Rules persist across restarts; missed runs are skipped without retry or catch-up; enabled=false pauses. One-shot rules are disabled when due. Ask the user if time or timezone is unclear.", map[string]any{"id": id, "schedule": spec}, []string{"id", "schedule"}),
		makeTool("schedules_list", "List scheduled rules and their next trigger time in the current Room.", map[string]any{}, []string{}),
		makeTool("schedules_delete", "Delete a scheduled task in the current Room. Already delivered messages and running work are retained.", map[string]any{"id": id}, []string{"id"}),
	}
}
