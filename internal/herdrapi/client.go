package herdrapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"
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

	// SetDeadline above only protects the deadline case. A context cancelled
	// via context.WithCancel (no deadline) would otherwise leave a blocked
	// read/write completely unprotected: cancel() alone never touches the
	// socket. Watch ctx.Done() for the lifetime of this call and force an
	// immediate deadline the instant it fires, so a blocked read returns
	// promptly instead of waiting on the peer.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.SetDeadline(time.Now())
		case <-done:
		}
	}()

	id := fmt.Sprintf("herdr-tokens-%d", c.seq.Add(1))
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("herdrapi: marshal %s: %w", method, err)
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("herdrapi: %s: %w", method, ctx.Err())
		}
		return fmt.Errorf("herdrapi: write %s: %w", method, err)
	}

	// Herdr's wire format is compact (single-line) JSON: exactly one
	// response per connection, terminated by exactly one '\n', which never
	// appears earlier in the body. Confirmed directly against the live
	// server: a captured session.snapshot response was ~20KB with exactly
	// one embedded newline, at the very last byte, followed by the peer
	// closing the connection. The newline is therefore both necessary and
	// sufficient as the frame terminator — do not build buffering or
	// multi-message logic here on an assumption of pretty-printed or
	// multi-line responses; the protocol never sends those.
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		if ctx.Err() != nil {
			return fmt.Errorf("herdrapi: %s: %w", method, ctx.Err())
		}
		return fmt.Errorf("herdrapi: read %s: %w", method, err)
	}
	var res response
	if err := json.Unmarshal(line, &res); err != nil {
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
