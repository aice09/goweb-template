// Fast CRUD MCP server. Uses stdio JSON-RPC and exposes database tools.
// Read-only tools are enabled by default. Set MCP_ENABLE_MUTATIONS=true only
// when the MCP client and its host are trusted and mutation approvals are enforced.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Item struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var db *pgxpool.Pool
var mutations bool

func response(id json.RawMessage, result any) rpcResponse {
	var v any
	if len(id) > 0 {
		_ = json.Unmarshal(id, &v)
	}
	return rpcResponse{JSONRPC: "2.0", ID: v, Result: result}
}

func failure(id json.RawMessage, code int, message string) rpcResponse {
	var v any
	if len(id) > 0 {
		_ = json.Unmarshal(id, &v)
	}
	return rpcResponse{JSONRPC: "2.0", ID: v, Error: &rpcError{Code: code, Message: message}}
}

func tools() []map[string]any {
	out := []map[string]any{
		{
			"name":        "health_check",
			"description": "Check PostgreSQL connectivity and return service status. Read-only.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_items",
			"description": "List items using bounded pagination. Read-only.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					"offset": map[string]any{"type": "integer", "minimum": 0},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "get_item",
			"description": "Retrieve one item by numeric ID. Read-only.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "minimum": 1},
				},
				"additionalProperties": false,
			},
		},
	}
	if mutations {
		out = append(out,
			map[string]any{
				"name":        "create_item",
				"description": "Create an item. Requires MCP_ENABLE_MUTATIONS=true and a trusted MCP host.",
				"inputSchema": map[string]any{
					"type":     "object",
					"required": []string{"name"},
					"properties": map[string]any{
						"name":        map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
						"description": map[string]any{"type": "string", "maxLength": 10000},
					},
					"additionalProperties": false,
				},
			},
			map[string]any{
				"name":        "update_item",
				"description": "Update an item. Requires MCP_ENABLE_MUTATIONS=true and a trusted MCP host.",
				"inputSchema": map[string]any{
					"type":     "object",
					"required": []string{"id", "name"},
					"properties": map[string]any{
						"id":          map[string]any{"type": "integer", "minimum": 1},
						"name":        map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
						"description": map[string]any{"type": "string", "maxLength": 10000},
					},
					"additionalProperties": false,
				},
			},
			map[string]any{
				"name":        "delete_item",
				"description": "Delete an item. Destructive; requires MCP_ENABLE_MUTATIONS=true and explicit user approval in the MCP client.",
				"inputSchema": map[string]any{
					"type":     "object",
					"required": []string{"id"},
					"properties": map[string]any{
						"id": map[string]any{"type": "integer", "minimum": 1},
					},
					"additionalProperties": false,
				},
			},
		)
	}
	return out
}

func numArg(args map[string]any, key string, fallback int64) (int64, error) {
	v, ok := args[key]
	if !ok {
		return fallback, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return int64(f), nil
}

func strArg(args map[string]any, key string, required bool, max int) (string, error) {
	v, ok := args[key]
	if !ok {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	if len(s) > max {
		return "", fmt.Errorf("%s exceeds %d characters", key, max)
	}
	return s, nil
}

func runTool(ctx context.Context, call toolCall) (any, error) {
	switch call.Name {
	case "health_check":
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := db.Ping(c); err != nil {
			return nil, errors.New("PostgreSQL is not ready")
		}
		return map[string]any{"status": "ready", "database": "postgresql"}, nil
	case "list_items":
		limit, err := numArg(call.Arguments, "limit", 25)
		if err != nil {
			return nil, err
		}
		if limit < 1 {
			limit = 1
		}
		if limit > 100 {
			limit = 100
		}
		offset, err := numArg(call.Arguments, "offset", 0)
		if err != nil {
			return nil, err
		}
		if offset < 0 {
			return nil, errors.New("offset cannot be negative")
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		rows, err := db.Query(c, `SELECT id,name,description,created_at,updated_at FROM items ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
		if err != nil {
			return nil, errors.New("query failed")
		}
		defer rows.Close()
		items := make([]Item, 0)
		for rows.Next() {
			var it Item
			if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt); err != nil {
				return nil, errors.New("query result failed")
			}
			items = append(items, it)
		}
		if rows.Err() != nil {
			return nil, errors.New("query result failed")
		}
		return map[string]any{"data": items, "limit": limit, "offset": offset}, nil
	case "get_item":
		id, err := numArg(call.Arguments, "id", 0)
		if err != nil {
			return nil, err
		}
		if id < 1 {
			return nil, errors.New("id must be positive")
		}
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var it Item
		err = db.QueryRow(c, `SELECT id,name,description,created_at,updated_at FROM items WHERE id=$1`, id).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("item not found")
		}
		if err != nil {
			return nil, errors.New("query failed")
		}
		return it, nil
	case "create_item":
		if !mutations {
			return nil, errors.New("mutations are disabled")
		}
		name, err := strArg(call.Arguments, "name", true, 200)
		if err != nil {
			return nil, err
		}
		desc, err := strArg(call.Arguments, "description", false, 10000)
		if err != nil {
			return nil, err
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		var it Item
		err = db.QueryRow(c, `INSERT INTO items(name,description) VALUES($1,$2) RETURNING id,name,description,created_at,updated_at`, name, desc).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
		if err != nil {
			return nil, errors.New("create failed")
		}
		return it, nil
	case "update_item":
		if !mutations {
			return nil, errors.New("mutations are disabled")
		}
		id, err := numArg(call.Arguments, "id", 0)
		if err != nil {
			return nil, err
		}
		if id < 1 {
			return nil, errors.New("id must be positive")
		}
		name, err := strArg(call.Arguments, "name", true, 200)
		if err != nil {
			return nil, err
		}
		desc, err := strArg(call.Arguments, "description", false, 10000)
		if err != nil {
			return nil, err
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		var it Item
		err = db.QueryRow(c, `UPDATE items SET name=$1,description=$2,updated_at=now() WHERE id=$3 RETURNING id,name,description,created_at,updated_at`, name, desc, id).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("item not found")
		}
		if err != nil {
			return nil, errors.New("update failed")
		}
		return it, nil
	case "delete_item":
		if !mutations {
			return nil, errors.New("mutations are disabled")
		}
		id, err := numArg(call.Arguments, "id", 0)
		if err != nil {
			return nil, err
		}
		if id < 1 {
			return nil, errors.New("id must be positive")
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		tag, err := db.Exec(c, `DELETE FROM items WHERE id=$1`, id)
		if err != nil {
			return nil, errors.New("delete failed")
		}
		if tag.RowsAffected() == 0 {
			return nil, errors.New("item not found")
		}
		return map[string]any{"deleted": true, "id": id}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", call.Name)
	}
}

func handle(req rpcRequest) *rpcResponse {
	switch req.Method {
	case "initialize":
		returnPtr := response(req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fast-crud-mcp", "version": "0.1.0"},
			"instructions":    "Read-only tools are enabled by default. Mutating tools are available only when MCP_ENABLE_MUTATIONS=true.",
		})
		return &returnPtr
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		r := response(req.ID, map[string]any{})
		return &r
	case "tools/list":
		r := response(req.ID, map[string]any{"tools": tools()})
		return &r
	case "tools/call":
		var call toolCall
		if err := json.Unmarshal(req.Params, &call); err != nil {
			r := failure(req.ID, -32602, "invalid tool arguments")
			return &r
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := runTool(ctx, call)
		var r rpcResponse
		if err != nil {
			content := []map[string]any{{"type": "text", "text": err.Error()}}
			r = response(req.ID, map[string]any{"content": content, "isError": true})
		} else {
			b, _ := json.MarshalIndent(result, "", "  ")
			r = response(req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}, "structuredContent": result})
		}
		return &r
	default:
		if len(req.ID) == 0 {
			return nil
		}
		r := failure(req.ID, -32601, "method not found")
		return &r
	}
}

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	mutations, _ = strconv.ParseBool(os.Getenv("MCP_ENABLE_MUTATIONS"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pc, err := pgxpool.ParseConfig(url)
	if err != nil {
		cancel()
		log.Fatal("invalid DATABASE_URL")
	}
	pc.MaxConns = 4
	pc.MinConns = 0
	db, err = pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		cancel()
		log.Fatal("database connection failed")
	}
	if err = db.Ping(ctx); err != nil {
		db.Close()
		cancel()
		log.Fatal("database ping failed")
	}
	cancel()
	defer db.Close()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			out, _ := json.Marshal(failure(nil, -32700, "parse error"))
			_, _ = writer.Write(append(out, '\n'))
			_ = writer.Flush()
			continue
		}
		resp := handle(req)
		if resp != nil {
			out, err := json.Marshal(resp)
			if err == nil {
				_, _ = writer.Write(append(out, '\n'))
				_ = writer.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("MCP stdin error: %v", err)
	}
}
