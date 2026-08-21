package herdrapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
)

// ErrEmptySnapshot marks a decode that produced no workspaces. Herdr always
// has at least one, so this means the payload was misread rather than that
// the session is genuinely empty. Treated as an error so the daemon logs and
// skips instead of concluding there is nothing to do.
var ErrEmptySnapshot = errors.New("herdrapi: snapshot decoded with zero workspaces")

type Client struct {
	socket string
	seq    atomic.Uint64
}

func NewClient(socket string) *Client { return &Client{socket: socket} }

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// call performs one request on one connection. Herdr closes the connection
// after a single response, so connections are never reused.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return fmt.Errorf("herdrapi: dial: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	id := fmt.Sprintf("herdr-tokens-%d", c.seq.Add(1))
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("herdrapi: marshal %s: %w", method, err)
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("herdrapi: write %s: %w", method, err)
	}

	// Herdr writes exactly one response and then closes the connection (see
	// call's doc comment), so EOF is the reliable end-of-message signal.
	// Reading only up to the first '\n' is unsafe: a response payload can
	// itself contain literal newlines (e.g. pretty-printed JSON), which would
	// truncate the message before this point rather than at its real end.
	data, err := io.ReadAll(conn)
	if err != nil && len(data) == 0 {
		return fmt.Errorf("herdrapi: read %s: %w", method, err)
	}
	var res response
	if err := json.Unmarshal(data, &res); err != nil {
		return fmt.Errorf("herdrapi: decode %s: %w", method, err)
	}
	if res.Error != nil {
		return fmt.Errorf("herdrapi: %s: %w", method, res.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(res.Result, out); err != nil {
		return fmt.Errorf("herdrapi: decode %s result: %w", method, err)
	}
	return nil
}

// Snapshot unwraps the NESTED result envelope: result is
// {"type":"session_snapshot","snapshot":{...}}, not the snapshot itself.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var res snapshotResult
	if err := c.call(ctx, "session.snapshot", map[string]any{}, &res); err != nil {
		return Snapshot{}, err
	}
	if len(res.Snapshot.Workspaces) == 0 {
		return Snapshot{}, ErrEmptySnapshot
	}
	return res.Snapshot, nil
}

// ReportWorkspaceMetadata writes tokens for one workspace. A nil value in
// tokens is sent as an explicit JSON null, which clears that key.
func (c *Client) ReportWorkspaceMetadata(ctx context.Context, workspaceID, source string, tokens map[string]*string, ttlMS int64) error {
	params := map[string]any{
		"workspace_id": workspaceID,
		"source":       source,
		"tokens":       tokens,
	}
	if ttlMS > 0 {
		params["ttl_ms"] = ttlMS
	}
	return c.call(ctx, "workspace.report_metadata", params, nil)
}
