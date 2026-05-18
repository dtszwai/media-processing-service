package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// rpcMessage is the union over JSON-RPC requests, responses, and
// notifications used by codex app-server. Methods carry params; replies carry
// either result or error keyed by the matching id.
type rpcMessage struct {
	ID     any             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcSession is a half-duplex JSON-RPC client over the codex app-server's
// stdio. Not safe for concurrent use; one Provider call owns one session.
type rpcSession struct {
	enc    *json.Encoder
	reader *bufio.Reader
}

func (s *rpcSession) notify(method string, params any) error {
	payload := map[string]any{"method": method}
	if params != nil {
		payload["params"] = params
	}
	return s.enc.Encode(payload)
}

func (s *rpcSession) request(id int, method string, params any) (json.RawMessage, error) {
	if err := s.enc.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := s.next()
		if err != nil {
			return nil, err
		}
		if sameID(msg.ID, id) {
			if msg.Error != nil {
				return nil, fmt.Errorf("codex %s: %s", method, msg.Error.Message)
			}
			return msg.Result, nil
		}
		// Reject server-initiated requests; the app-server occasionally
		// pings the client for permission and we run with approval=never.
		if msg.ID != nil && msg.Method != "" {
			_ = s.enc.Encode(map[string]any{
				"id": msg.ID,
				"error": map[string]any{
					"code":    -32000,
					"message": "media-processing-service does not handle app-server requests",
				},
			})
		}
	}
}

// waitForTurn drains stream events until turn/completed, capturing image
// items and the thread id needed for the on-disk cache fallback.
func (s *rpcSession) waitForTurn(ctx context.Context, threadID string) (turnResult, error) {
	result := turnResult{ThreadID: threadID}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		msg, err := s.next()
		if err != nil {
			return result, err
		}
		switch msg.Method {
		case "item/completed":
			var p struct {
				Item struct {
					Type      string  `json:"type"`
					Result    string  `json:"result"`
					SavedPath *string `json:"savedPath"`
				} `json:"item"`
			}
			if json.Unmarshal(msg.Params, &p) == nil && p.Item.Type == "imageGeneration" {
				result.ImageBase64 = strings.TrimSpace(p.Item.Result)
				if p.Item.SavedPath != nil {
					result.ImageSavedPath = strings.TrimSpace(*p.Item.SavedPath)
				}
			}
		case "turn/completed":
			var p struct {
				Turn struct {
					Status string `json:"status"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"turn"`
			}
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				return result, fmt.Errorf("decode turn/completed: %w", err)
			}
			if p.Turn.Error != nil && strings.TrimSpace(p.Turn.Error.Message) != "" {
				return result, generation.Transient("CODEX_TURN_ERROR", p.Turn.Error.Message)
			}
			if p.Turn.Status != "" && p.Turn.Status != "completed" {
				return result, generation.Transient("CODEX_TURN_STATUS",
					fmt.Sprintf("turn ended with status %s", p.Turn.Status))
			}
			return result, nil
		case "error":
			return result, generation.Transient("CODEX_RPC_ERROR", string(msg.Params))
		}
	}
}

func (s *rpcSession) next() (rpcMessage, error) {
	for {
		line, err := s.reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			var msg rpcMessage
			if jerr := json.Unmarshal([]byte(line), &msg); jerr == nil {
				return msg, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return rpcMessage{}, io.EOF
			}
			return rpcMessage{}, err
		}
	}
}

func sameID(value any, want int) bool {
	switch id := value.(type) {
	case float64:
		return int(id) == want
	case int:
		return id == want
	case string:
		return id == fmt.Sprint(want)
	default:
		return false
	}
}
