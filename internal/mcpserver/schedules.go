package mcpserver

import (
	"github.com/fishioon/aicat/internal/scheduler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addSchedules(server *mcp.Server, schedules *scheduler.Service) {
	add(server, "schedules_set", "Create or replace a schedule for a specific Room. room_id is the stable ID returned by rooms_list. Reuse id to modify or pause it. At the due time a system message is written to that Room.", false, false, schedules.Set)
	add(server, "schedules_list", "List schedules for a specific Room. room_id is the stable ID returned by rooms_list.", true, false, schedules.List)
	add(server, "schedules_delete", "Delete a Room schedule. Already delivered messages are retained. room_id is the stable ID returned by rooms_list.", false, true, schedules.Delete)
}
