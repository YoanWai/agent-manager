package catalog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// TestMain turns the test binary into a stand-in CLI when CATALOG_FAKE names
// one, so every reader talks to a real process replaying what the CLI
// answered when it was recorded into testdata.
func TestMain(m *testing.M) {
	if fake := os.Getenv("CATALOG_FAKE"); fake != "" {
		if err := runFake(fake); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixture(name string) map[string]json.RawMessage {
	raw, err := os.ReadFile(filepath.Join(os.Getenv("CATALOG_TESTDATA"), name))
	if err != nil {
		panic(err)
	}
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		panic(err)
	}
	return parts
}

func writeLine(value any) {
	line, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(line))
}

type fakeRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Type   string          `json:"type"`
	Error  json.RawMessage `json:"error"`
	// pi's commands carry their arguments beside the type.
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// serveLines answers each request line with what reply returns; a nil
// answer sends nothing.
func serveLines(reply func(fakeRequest) any) error {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var request fakeRequest
		if err := json.Unmarshal(in.Bytes(), &request); err != nil {
			return err
		}
		if answer := reply(request); answer != nil {
			writeLine(answer)
		}
	}
	return in.Err()
}

func result(request fakeRequest, value any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value}
}

func runFake(name string) error {
	switch name {
	case "exit":
		os.Exit(3)
	case "silent":
		// A bare select{} trips the runtime's deadlock check and exits.
		time.Sleep(time.Hour)
	case "claude":
		parts := fixture("claude_initialize.json")
		return serveLines(func(request fakeRequest) any {
			writeLine(map[string]any{"type": "system", "subtype": "commands_changed"})
			return parts
		})
	case "claude-refuses":
		return serveLines(func(request fakeRequest) any {
			return map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": "catalog", "error": "Not logged in"}}
		})
	case "codex":
		parts := fixture("codex_model_list.json")
		var page struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(parts["data"], &page.Data); err != nil {
			return err
		}
		hidden := map[string]any{"model": "hidden-one", "displayName": "Hidden", "hidden": true, "isDefault": false, "defaultReasoningEffort": "low", "supportedReasoningEfforts": []any{}}
		refused := false
		return serveLines(func(request fakeRequest) any {
			switch request.Method {
			case "initialize":
				return result(request, map[string]any{"userAgent": "fake"})
			case "model/list":
				var params struct {
					Cursor string `json:"cursor"`
				}
				_ = json.Unmarshal(request.Params, &params)
				if params.Cursor == "" {
					// A request of the server's own must be refused before
					// the answer arrives.
					writeLine(map[string]any{"jsonrpc": "2.0", "id": "srv-1", "method": "item/tool/requestUserInput", "params": map[string]any{}})
					writeLine(map[string]any{"jsonrpc": "2.0", "method": "remoteControl/status/changed", "params": map[string]any{}})
					return result(request, map[string]any{"data": page.Data[:1], "nextCursor": "2"})
				}
				if !refused {
					return result(request, map[string]any{"data": []any{}, "nextCursor": nil})
				}
				return result(request, map[string]any{"data": append(page.Data[1:], hidden), "nextCursor": nil})
			case "":
				refused = string(request.ID) == `"srv-1"` && request.Error != nil
			}
			return nil
		})
	case "acp-grok":
		parts := fixture("grok_acp.json")
		var sets map[string]json.RawMessage
		if err := json.Unmarshal(parts["set_config_option"], &sets); err != nil {
			return err
		}
		return serveLines(func(request fakeRequest) any {
			switch request.Method {
			case "initialize":
				return result(request, map[string]any{"protocolVersion": 1})
			case "session/new":
				return result(request, parts["session_new"])
			case "session/set_config_option":
				var params struct {
					Value string `json:"value"`
				}
				_ = json.Unmarshal(request.Params, &params)
				return result(request, sets[params.Value])
			}
			return nil
		})
	case "acp-gemini":
		parts := fixture("gemini_acp.json")
		return serveLines(func(request fakeRequest) any {
			switch request.Method {
			case "initialize":
				return result(request, map[string]any{"protocolVersion": 1})
			case "session/new":
				return result(request, parts["session_new"])
			}
			return nil
		})
	case "pi", "pi-0.84.2":
		if len(os.Args) > 1 && os.Args[1] == "--version" {
			version := "0.85.0"
			if name == "pi-0.84.2" {
				version = "0.84.2"
			}
			fmt.Println(version)
			return nil
		}
		parts := fixture("pi_rpc.json")
		var levels map[string][]string
		if err := json.Unmarshal(parts["levels"], &levels); err != nil {
			return err
		}
		current := ""
		return serveLines(func(request fakeRequest) any {
			var id string
			_ = json.Unmarshal(request.ID, &id)
			respond := func(data any) map[string]any {
				return map[string]any{"id": id, "type": "response", "command": request.Type, "success": true, "data": data}
			}
			switch request.Type {
			case "get_available_models":
				return respond(map[string]any{"models": parts["models"]})
			case "get_state":
				return respond(map[string]any{"model": parts["state_model"], "thinkingLevel": "medium"})
			case "set_model":
				current = request.Provider + "/" + request.ModelID
				return respond(map[string]any{"id": request.ModelID})
			case "get_available_thinking_levels":
				return respond(map[string]any{"levels": levels[current]})
			}
			return nil
		})
	case "muse":
		parts := fixture("muse_serve.json")
		return serveLines(func(request fakeRequest) any {
			switch request.Method {
			case "initialize":
				// Muse refuses a client name outside [a-z0-9_].
				var params struct {
					ClientInfo struct {
						Name string `json:"name"`
					} `json:"clientInfo"`
				}
				_ = json.Unmarshal(request.Params, &params)
				if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(params.ClientInfo.Name) {
					return map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32602, "message": "clientInfo.name must be an identifier matching ^[a-z0-9_]+$"}}
				}
				return result(request, map[string]any{})
			case "model/list":
				return result(request, parts["model_list"])
			}
			return nil
		})
	case "opencode":
		parts := fixture("opencode_serve.json")
		password := os.Getenv("OPENCODE_SERVER_PASSWORD")
		announce := func(address string) string { return "opencode server listening on http://" + address }
		return serveHTTP(announce, func(w http.ResponseWriter, r *http.Request) {
			if user, pass, ok := r.BasicAuth(); !ok || user != "opencode" || pass != password {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			switch r.URL.Path {
			case "/config/providers":
				_, _ = w.Write(parts["providers"])
			case "/config":
				_, _ = w.Write(parts["config"])
			default:
				http.NotFound(w, r)
			}
		})
	case "hermes", "hermes-work":
		parts := fixture("hermes_serve.json")
		if name == "hermes-work" {
			addWorkProfile(parts)
		}
		token := os.Getenv("HERMES_DASHBOARD_SESSION_TOKEN")
		announce := func(address string) string {
			_, port, _ := net.SplitHostPort(address)
			return "HERMES_BACKEND_READY port=" + port
		}
		return serveHTTP(announce, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Hermes-Session-Token") != token {
				http.Error(w, `{"detail":"Unauthorized"}`, http.StatusUnauthorized)
				return
			}
			switch r.URL.Path {
			case "/api/profiles":
				_, _ = w.Write(parts["profiles"])
			case "/api/model/options":
				if options, ok := parts["options:"+r.URL.Query().Get("profile")]; ok {
					_, _ = w.Write(options)
					return
				}
				_, _ = w.Write(parts["options"])
			default:
				http.NotFound(w, r)
			}
		})
	}
	return fmt.Errorf("no fake named %q", name)
}

// addWorkProfile gives the recorded hermes a second profile, "work", that
// offers only the anthropic models, for a reader asked per profile.
func addWorkProfile(parts map[string]json.RawMessage) {
	var listed struct {
		Profiles []map[string]any `json:"profiles"`
	}
	var options map[string]any
	if json.Unmarshal(parts["profiles"], &listed) != nil || json.Unmarshal(parts["options"], &options) != nil {
		panic("hermes fixture")
	}
	listed.Profiles = append(listed.Profiles, map[string]any{"name": "work", "model": "claude-opus-5", "provider": "anthropic"})
	var anthropic []any
	for _, row := range options["providers"].([]any) {
		if row.(map[string]any)["slug"] == "anthropic" {
			anthropic = append(anthropic, row)
		}
	}
	options["providers"], options["model"], options["provider"] = anthropic, "claude-opus-5", "anthropic"
	parts["profiles"], _ = json.Marshal(listed)
	parts["options:work"], _ = json.Marshal(options)
}

// serveHTTP listens on a free loopback port and prints the line announce
// makes of its address, the way the real server says it is up.
func serveHTTP(announce func(address string) string, handler http.HandlerFunc) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	fmt.Println(announce(listener.Addr().String()))
	return http.Serve(listener, handler)
}
