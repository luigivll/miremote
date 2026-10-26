// Package signaling implementa el cliente del agente contra el servidor de
// señalización: registro, creación de invitaciones (QR), sondeo, decisión,
// credenciales TURN y la conexión WebSocket por la que llegan las ofertas.
package signaling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"example.com/miremote/agent/internal/identity"
)

type Client struct {
	httpURL string
	wsURL   string
	id      *identity.Identity
	hc      *http.Client
}

func NewClient(httpURL, wsURL string, id *identity.Identity) *Client {
	return &Client{
		httpURL: httpURL,
		wsURL:   wsURL,
		id:      id,
		hc:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) getNonce(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.httpURL+"/v1/auth/nonce", nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Nonce, nil
}

// authHeader construye la cabecera X-Device-Auth firmando un nonce fresco.
func (c *Client) authHeader(ctx context.Context) (string, error) {
	nonce, err := c.getNonce(ctx)
	if err != nil {
		return "", err
	}
	sig := c.id.Sign([]byte(nonce))
	return fmt.Sprintf("v1 %s %s %s", c.id.DeviceID, nonce, sig), nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, auth bool, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.httpURL+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		h, err := c.authHeader(ctx)
		if err != nil {
			return 0, err
		}
		req.Header.Set("X-Device-Auth", h)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		_ = json.Unmarshal(data, out)
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, string(data))
	}
	return resp.StatusCode, nil
}

// Register registra la identidad del host (idempotente).
func (c *Client) Register(ctx context.Context, name string) error {
	nonce, err := c.getNonce(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{
		"public_key": c.id.PublicKeyB64(),
		"name":       name,
		"nonce":      nonce,
		"signature":  c.id.Sign([]byte(nonce)),
	}
	_, err = c.doJSON(ctx, "POST", "/v1/hosts/register", body, false, nil)
	return err
}

// Invitation es la respuesta de crear una invitación.
type Invitation struct {
	PairingID       string `json:"pairing_id"`
	Secret          string `json:"secret"`
	PollToken       string `json:"poll_token"`
	ExpiresAt       string `json:"expires_at"`
	HostFingerprint string `json:"host_fingerprint"`
}

func (c *Client) CreateInvitation(ctx context.Context) (*Invitation, error) {
	var inv Invitation
	_, err := c.doJSON(ctx, "POST", "/v1/pairing/invitations", map[string]any{}, true, &inv)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// PollResult es el estado de una invitación.
type PollResult struct {
	Status string `json:"status"`
	Mobile *struct {
		DeviceID    string `json:"device_id"`
		Name        string `json:"name"`
		Platform    string `json:"platform"`
		Fingerprint string `json:"fingerprint"`
	} `json:"mobile"`
}

func (c *Client) Poll(ctx context.Context, pairingID, pollToken string) (*PollResult, error) {
	var out PollResult
	_, err := c.doJSON(ctx, "GET",
		"/v1/pairing/invitations/"+pairingID+"?poll_token="+pollToken, nil, false, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Decide(ctx context.Context, pairingID, decision string, permissions []string) error {
	body := map[string]any{"decision": decision, "permissions": permissions}
	_, err := c.doJSON(ctx, "POST",
		"/v1/pairing/invitations/"+pairingID+"/decision", body, true, nil)
	return err
}

// TURNCredentials replica la forma devuelta por el servidor.
type TURNCredentials struct {
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
	TTL        int      `json:"ttl"`
	URLs       []string `json:"urls"`
}

func (c *Client) TURN(ctx context.Context) (*TURNCredentials, error) {
	var out TURNCredentials
	_, err := c.doJSON(ctx, "POST", "/v1/turn/credentials", map[string]any{}, true, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
