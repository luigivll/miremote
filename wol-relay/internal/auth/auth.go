// Package auth implementa el desafío-respuesta Ed25519 del relevo WoL y un
// limitador de frecuencia por dispositivo.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrNonceInvalid = errors.New("nonce inválido o caducado")
	ErrRateLimited  = errors.New("demasiadas peticiones, inténtalo más tarde")
	ErrBadSignature = errors.New("firma inválida")
)

// NonceStore emite y consume nonces de un solo uso con caducidad.
type NonceStore struct {
	mu     sync.Mutex
	nonces map[string]time.Time
	ttl    time.Duration
}

func NewNonceStore(ttl time.Duration) *NonceStore {
	ns := &NonceStore{nonces: make(map[string]time.Time), ttl: ttl}
	go ns.janitor()
	return ns
}

func (ns *NonceStore) Issue() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	n := base64.RawURLEncoding.EncodeToString(buf)
	ns.mu.Lock()
	ns.nonces[n] = time.Now().Add(ns.ttl)
	ns.mu.Unlock()
	return n
}

// Consume valida que el nonce exista y no haya caducado, y lo elimina.
func (ns *NonceStore) Consume(n string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	exp, ok := ns.nonces[n]
	if !ok || time.Now().After(exp) {
		delete(ns.nonces, n)
		return ErrNonceInvalid
	}
	delete(ns.nonces, n)
	return nil
}

func (ns *NonceStore) janitor() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		now := time.Now()
		ns.mu.Lock()
		for n, exp := range ns.nonces {
			if now.After(exp) {
				delete(ns.nonces, n)
			}
		}
		ns.mu.Unlock()
	}
}

// Verify comprueba la firma Ed25519 de un nonce.
func Verify(pubB64, nonce, sigB64 string) error {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ErrBadSignature
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return ErrBadSignature
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(nonce), sig) {
		return ErrBadSignature
	}
	return nil
}

// RateLimiter limita la frecuencia de peticiones aceptadas por dispositivo.
type RateLimiter struct {
	mu       sync.Mutex
	last     map[string]time.Time
	interval time.Duration
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{last: make(map[string]time.Time), interval: interval}
}

// Allow devuelve nil si el dispositivo puede actuar ahora, o ErrRateLimited.
func (r *RateLimiter) Allow(deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if t, ok := r.last[deviceID]; ok && now.Sub(t) < r.interval {
		return ErrRateLimited
	}
	r.last[deviceID] = now
	return nil
}
