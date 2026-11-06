package mcpserver

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type messageStatusArgs struct {
	MessageID int64 `json:"message_id" jsonschema:"message id returned by send_session"`
}

type listMessagesArgs struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session whose messages to list, sent to it or by it; omit for every session on this machine"`
	Queued    bool   `json:"queued,omitempty" jsonschema:"true lists only the messages still waiting to be typed in, next first"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many messages to list, default 20"`
}

type listMessagesOutput struct {
	Messages []sessioncmd.MessageEntry `json:"messages"`
}

type readMessageArgs struct {
	MessageID int64 `json:"message_id" jsonschema:"message id from list_messages or send_session"`
}

func addMessageLogTools(server *mcp.Server, noCaller error, sessions sessionCommands, sessionID string) {
	addTool(server, noCaller, &mcp.Tool{
		Name: "list_messages",
		Description: "List the messages sessions on this machine sent each other: who sent each to whom, when, a one-line subject, and where it stands. " +
			"Call it to see what is waiting for a session, or to follow a handoff you did not send yourself. " +
			"Queued and held mean what they mean in message_status, and a held message's reason says what to answer or restore. " +
			"With queued set it lists only what still waits, next first; otherwise the most recent messages, oldest first. Delivered messages leave the log after a day. " +
			"Call read_message for one message's full text.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listMessagesArgs) (*mcp.CallToolResult, listMessagesOutput, error) {
		if err := localOnly("list_messages", args.SessionID); err != nil {
			return nil, listMessagesOutput{}, err
		}
		limit := args.Limit
		if limit == 0 {
			limit = sessioncmd.DefaultMessagesLimit
		}
		entries, err := sessions.Messages(sessionID, sessioncmd.MessagesOptions{Session: args.SessionID, QueuedOnly: args.Queued, Limit: limit})
		if err != nil {
			return nil, listMessagesOutput{}, err
		}
		return textContent(sessioncmd.FormatMessageList(entries)), listMessagesOutput{Messages: entries}, nil
	})

	addTool(server, noCaller, &mcp.Tool{
		Name: "read_message",
		Description: "Read one message from the log in full, whoever sent it: the sender, the recipient, when it was sent, where it stands and its whole text. " +
			"It only reads: the message is not delivered, answered or acknowledged by it.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readMessageArgs) (*mcp.CallToolResult, sessioncmd.MessageEntry, error) {
		entry, err := sessions.ReadMessage(sessionID, args.MessageID)
		if err != nil {
			return nil, sessioncmd.MessageEntry{}, err
		}
		return textContent(sessioncmd.FormatMessage(entry)), entry, nil
	})
}
