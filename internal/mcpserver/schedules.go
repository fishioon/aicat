package mcpserver

import (
	"github.com/fishioon/aicat/internal/scheduler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addSchedules(server *mcp.Server, schedules *scheduler.Service) {
	add(server, "schedules_set", "Create or replace a Room schedule. Reuse id to modify or pause it. room_id is the stable ID from GET /api/rooms.", false, false, schedules.Set)
	add(server, "schedules_list", "List schedules for room_id, the stable ID from GET /api/rooms.", true, false, schedules.List)
	add(server, "schedules_delete", "Delete a Room schedule. Already delivered messages are retained.", false, true, schedules.Delete)
}
