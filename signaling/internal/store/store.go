// Package store implementa la persistencia del servidor de señalización.
//
// Implementación: estado en memoria protegido por mutex, con persistencia
// append-safe a un fichero JSON en disco para lo que debe sobrevivir a un
// reinicio (dispositivos y vínculos aprobados). Las invitaciones y nonces son
// efímeros y no necesitan sobrevivir a reinicios.
//
// El consumo de invitaciones es ATÓMICO bajo el mutex: la comprobación de
// estado y la transición a 'claimed' ocurren sin que otra goroutine pueda
// intercalarse, de modo que dos reclamaciones simultáneas no pueden ganar las
// dos.
//
// El modelo de datos es pequeño (unos pocos dispositivos y vínculos por
// usuario), así que este enfoque es suficiente y evita dependencias de CGo o
// de registros de módulos no disponibles.
package store

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrNotFound      = errors.New("no encontrado")
	ErrAlreadyClaim  = errors.New("invitación ya reclamada o caducada")
	ErrBadSecret     = errors.New("secreto inválido")
	ErrNonceReplayed = errors.New("nonce ya usado o caducado")
)

type Device struct {
	DeviceID  string    `json:"device_id"`
	PublicKey []byte    `json:"public_key"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Invitation struct {
	PairingID   string    `json:"pairing_id"`
	HostID      string    `json:"host_id"`
	SecretHash  []byte    `json:"secret_hash"`
	PollToken   string    `json:"poll_token"`
	Status      string    `json:"status"`
	MobileID    string    `json:"mobile_id"`
	MobilePub   []byte    `json:"mobile_pub"`
	MobileName  string    `json:"mobile_name"`
	MobilePlat  string    `json:"mobile_plat"`
	Permissions []string  `json:"permissions"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

type Link struct {
	HostID      string    `json:"host_id"`
	MobileID    string    `json:"mobile_id"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

// persisted es lo único que se guarda en disco.
type persisted struct {
	Devices map[string]Device `json:"devices"`
	Links   map[string]Link   `json:"links"` // clave: hostID + "|" + mobileID
}

type nonceEntry struct {
	expiresAt time.Time
}

type Store struct {
	mu sync.Mutex

	devices     map[string]Device
	links       map[string]Link
	invitations map[string]Invitation
	nonces      map[string]nonceEntry

	path string
}

func linkKey(hostID, mobileID string) string { return hostID + "|" + mobileID }

func Open(ctx context.Context, path string) (*Store, error) {
	s := &Store{
		devices:     make(map[string]Device),
		links:       make(map[string]Link),
		invitations: make(map[string]Invitation),
		nonces:      make(map[string]nonceEntry),
		path:        path,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.flushLocked() }

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // primer arranque
		}
		return err
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.Devices != nil {
		s.devices = p.Devices
	}
	if p.Links != nil {
		s.links = p.Links
	}
	return nil
}

// flushLocked escribe el estado persistente a disco de forma atómica
// (escritura a temporal + rename). Debe llamarse con el mutex ya tomado, o
// desde Close.
func (s *Store) flushLocked() error {
	p := persisted{Devices: s.devices, Links: s.links}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".store-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

// ---- Dispositivos ----

func (s *Store) UpsertDevice(ctx context.Context, d Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.devices[d.DeviceID] = d
	return s.flushLocked()
}

func (s *Store) GetDevice(ctx context.Context, id string) (*Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := d
	return &cp, nil
}

// ---- Invitaciones ----

func (s *Store) CreateInvitation(ctx context.Context, inv Invitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv.Status = "pending"
	inv.CreatedAt = time.Now()
	s.invitations[inv.PairingID] = inv
	return nil // efímero: no se persiste
}

func (s *Store) GetInvitation(ctx context.Context, pairingID string) (*Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invitations[pairingID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := inv
	return &cp, nil
}

// ClaimInvitation realiza el canje atómico bajo el mutex.
func (s *Store) ClaimInvitation(ctx context.Context, pairingID string, secretHash []byte,
	mobileID string, mobilePub []byte, mobileName, mobilePlat string) (*Invitation, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	inv, ok := s.invitations[pairingID]
	if !ok {
		return nil, ErrNotFound
	}
	if subtle.ConstantTimeCompare(inv.SecretHash, secretHash) != 1 {
		return nil, ErrBadSecret
	}
	if inv.Status != "pending" || time.Now().After(inv.ExpiresAt) {
		return nil, ErrAlreadyClaim
	}

	inv.Status = "claimed"
	inv.MobileID = mobileID
	inv.MobilePub = mobilePub
	inv.MobileName = mobileName
	inv.MobilePlat = mobilePlat
	s.invitations[pairingID] = inv

	cp := inv
	return &cp, nil
}

// DecideInvitation aprueba o rechaza una invitación reclamada.
func (s *Store) DecideInvitation(ctx context.Context, pairingID, hostID, decision string,
	permissions []string) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	inv, ok := s.invitations[pairingID]
	if !ok {
		return ErrNotFound
	}
	if inv.HostID != hostID {
		return errors.New("la invitación no pertenece a este host")
	}
	if inv.Status != "claimed" {
		return errors.New("la invitación no está en estado reclamado")
	}

	if decision != "approve" {
		inv.Status = "rejected"
		s.invitations[pairingID] = inv
		return nil
	}

	// Registrar identidad del móvil y crear vínculo.
	s.devices[inv.MobileID] = Device{
		DeviceID:  inv.MobileID,
		PublicKey: inv.MobilePub,
		Name:      inv.MobileName,
		Platform:  inv.MobilePlat,
		Role:      "mobile",
		CreatedAt: time.Now(),
	}
	s.links[linkKey(hostID, inv.MobileID)] = Link{
		HostID:      hostID,
		MobileID:    inv.MobileID,
		Permissions: permissions,
		CreatedAt:   time.Now(),
	}
	inv.Status = "approved"
	inv.Permissions = permissions
	s.invitations[pairingID] = inv

	return s.flushLocked()
}

// ---- Vínculos ----

func (s *Store) GetLink(ctx context.Context, hostID, mobileID string) (*Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[linkKey(hostID, mobileID)]
	if !ok {
		return nil, ErrNotFound
	}
	cp := l
	return &cp, nil
}

func (s *Store) LinksForDevice(ctx context.Context, deviceID string) ([]Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Link
	for _, l := range s.links {
		if l.HostID == deviceID || l.MobileID == deviceID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *Store) DeleteLink(ctx context.Context, hostID, mobileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, linkKey(hostID, mobileID))
	return s.flushLocked()
}

// ---- Nonces (anti-repetición) ----

func (s *Store) ConsumeNonce(ctx context.Context, nonce string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.nonces[nonce]; ok {
		// Existe. Si no ha caducado, es repetición.
		if time.Now().Before(e.expiresAt) {
			return ErrNonceReplayed
		}
	}
	s.nonces[nonce] = nonceEntry{expiresAt: time.Now().Add(ttl)}
	return nil
}

// GC elimina invitaciones y nonces caducados.
func (s *Store) GC(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for n, e := range s.nonces {
		if now.After(e.expiresAt) {
			delete(s.nonces, n)
		}
	}
	for id, inv := range s.invitations {
		if (inv.Status == "pending" || inv.Status == "claimed") && now.After(inv.ExpiresAt) {
			inv.Status = "expired"
			s.invitations[id] = inv
		}
		// Limpiar invitaciones terminadas y viejas.
		if now.Sub(inv.CreatedAt) > time.Hour {
			delete(s.invitations, id)
		}
	}
	return nil
}
