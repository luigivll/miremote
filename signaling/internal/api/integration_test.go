package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"example.com/miremote/signaling/internal/api"
	"example.com/miremote/signaling/internal/config"
	"example.com/miremote/signaling/internal/store"
	"example.com/miremote/signaling/internal/ws"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	cfg := &config.Config{
		JWTSecret:     []byte("0123456789abcdef0123456789abcdef"),
		TURNSecret:    []byte("turnsecret1234567890"),
		TURNUrls:      []string{"turn:turn.example.com:3478?transport=udp"},
		InvitationTTL: 120 * time.Second,
		SessionTTL:    5 * time.Minute,
		TURNCredTTL:   10 * time.Minute,
		NonceTTL:      30 * time.Second,
	}
	st, err := store.Open(context.Background(), t.TempDir()+"/store.json")
	if err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub(st)
	srv := api.NewServer(cfg, st, hub)
	return httptest.NewServer(srv.Routes()), st
}

func deviceID(pub ed25519.PublicKey, role string) string {
	sum := sha256.Sum256(pub)
	id := base64.RawURLEncoding.EncodeToString(sum[:])[:16]
	prefix := "m-"
	if role == "host" {
		prefix = "p-"
	}
	return prefix + id[2:]
}

func getNonce(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Get(base + "/v1/auth/nonce")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ Nonce string }
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Nonce
}

func postJSON(t *testing.T, url string, body any, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

func authHeader(deviceID, nonce string, sk ed25519.PrivateKey) string {
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(sk, []byte(nonce)))
	return "v1 " + deviceID + " " + nonce + " " + sig
}

func TestFullPairingFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	base := srv.URL

	// 1. Host genera identidad y se registra.
	hostPub, hostSk, _ := ed25519.GenerateKey(nil)
	hostID := deviceID(hostPub, "host")
	nonce := getNonce(t, base)
	code, _ := postJSON(t, base+"/v1/hosts/register", map[string]any{
		"public_key": base64.StdEncoding.EncodeToString(hostPub),
		"name":       "PC-Test",
		"nonce":      nonce,
		"signature":  base64.StdEncoding.EncodeToString(ed25519.Sign(hostSk, []byte(nonce))),
	}, nil)
	if code != 200 {
		t.Fatalf("register host: código %d", code)
	}

	// 2. Host crea invitación (autenticado).
	nonce = getNonce(t, base)
	code, inv := postJSON(t, base+"/v1/pairing/invitations", map[string]any{},
		map[string]string{"X-Device-Auth": authHeader(hostID, nonce, hostSk)})
	if code != 201 {
		t.Fatalf("crear invitación: código %d (%v)", code, inv)
	}
	pairingID := inv["pairing_id"].(string)
	secret := inv["secret"].(string)
	pollToken := inv["poll_token"].(string)

	// 3. Móvil reclama la invitación.
	mobPub, mobSk, _ := ed25519.GenerateKey(nil)
	mobID := deviceID(mobPub, "mobile")
	code, claim := postJSON(t, base+"/v1/pairing/claim", map[string]any{
		"pairing_id": pairingID,
		"secret":     secret,
		"device": map[string]any{
			"device_id":  mobID,
			"public_key": base64.StdEncoding.EncodeToString(mobPub),
			"name":       "Pixel-Test",
			"platform":   "android",
		},
	}, nil)
	if code != 200 {
		t.Fatalf("claim: código %d (%v)", code, claim)
	}
	if claim["status"] != "pending_host_approval" {
		t.Fatalf("estado inesperado tras claim: %v", claim["status"])
	}

	// 4. Host sondea y ve 'claimed'.
	resp, _ := http.Get(base + "/v1/pairing/invitations/" + pairingID + "?poll_token=" + pollToken)
	var poll map[string]any
	json.NewDecoder(resp.Body).Decode(&poll)
	resp.Body.Close()
	if poll["status"] != "claimed" {
		t.Fatalf("sondeo: estado %v", poll["status"])
	}

	// 5. Host aprueba.
	nonce = getNonce(t, base)
	code, _ = postJSON(t, base+"/v1/pairing/invitations/"+pairingID+"/decision", map[string]any{
		"decision":    "approve",
		"permissions": []string{"screen.view", "input.control"},
	}, map[string]string{"X-Device-Auth": authHeader(hostID, nonce, hostSk)})
	if code != 200 {
		t.Fatalf("aprobar: código %d", code)
	}

	// 6. Móvil pide token de sesión (autenticado) para el host.
	nonce = getNonce(t, base)
	code, tok := postJSON(t, base+"/v1/session/token", map[string]any{
		"target_device_id": hostID,
	}, map[string]string{"X-Device-Auth": authHeader(mobID, nonce, mobSk)})
	if code != 200 {
		t.Fatalf("session token: código %d (%v)", code, tok)
	}
	if _, ok := tok["session_token"]; !ok {
		t.Fatalf("sin session_token en respuesta: %v", tok)
	}

	// 7. Credenciales TURN.
	nonce = getNonce(t, base)
	code, turnc := postJSON(t, base+"/v1/turn/credentials", map[string]any{},
		map[string]string{"X-Device-Auth": authHeader(mobID, nonce, mobSk)})
	if code != 200 {
		t.Fatalf("turn: código %d", code)
	}
	if _, ok := turnc["username"]; !ok {
		t.Fatalf("sin username TURN: %v", turnc)
	}
}

// TestClaimIsAtomic lanza N reclamaciones concurrentes sobre la misma
// invitación y comprueba que exactamente una gana.
func TestClaimIsAtomic(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	base := srv.URL

	hostPub, hostSk, _ := ed25519.GenerateKey(nil)
	hostID := deviceID(hostPub, "host")
	nonce := getNonce(t, base)
	postJSON(t, base+"/v1/hosts/register", map[string]any{
		"public_key": base64.StdEncoding.EncodeToString(hostPub),
		"name":       "PC", "nonce": nonce,
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(hostSk, []byte(nonce))),
	}, nil)
	nonce = getNonce(t, base)
	_, inv := postJSON(t, base+"/v1/pairing/invitations", map[string]any{},
		map[string]string{"X-Device-Auth": authHeader(hostID, nonce, hostSk)})
	pairingID := inv["pairing_id"].(string)
	secret := inv["secret"].(string)

	const N = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mp, _, _ := ed25519.GenerateKey(nil)
			mid := deviceID(mp, "mobile")
			code, _ := postJSON(t, base+"/v1/pairing/claim", map[string]any{
				"pairing_id": pairingID, "secret": secret,
				"device": map[string]any{
					"device_id": mid, "public_key": base64.StdEncoding.EncodeToString(mp),
					"name": "m", "platform": "android",
				},
			}, nil)
			if code == 200 {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("esperaba exactamente 1 reclamación ganadora, hubo %d", wins)
	}
}

// TestNonceReplayRejected comprueba que un nonce no puede reutilizarse.
func TestNonceReplayRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	base := srv.URL

	pub, sk, _ := ed25519.GenerateKey(nil)
	id := deviceID(pub, "host")
	nonce := getNonce(t, base)
	body := map[string]any{
		"public_key": base64.StdEncoding.EncodeToString(pub),
		"name":       "PC", "nonce": nonce,
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(sk, []byte(nonce))),
	}
	code1, _ := postJSON(t, base+"/v1/hosts/register", body, nil)
	code2, _ := postJSON(t, base+"/v1/hosts/register", body, nil) // mismo nonce
	if code1 != 200 {
		t.Fatalf("primer registro: %d", code1)
	}
	if code2 == 200 {
		t.Fatalf("segundo registro con nonce repetido debería fallar, dio 200")
	}
	_ = id
}
