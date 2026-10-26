// Package pairing construye el payload del QR del lado del agente.
package pairing

import (
	"encoding/json"
	"time"
)

type QRPayload struct {
	Version         int    `json:"version"`
	Server          string `json:"server"`
	HostDeviceID    string `json:"host_device_id"`
	PairingID       string `json:"pairing_id"`
	Secret          string `json:"secret"`
	HostFingerprint string `json:"host_key_fingerprint"`
	ExpiresAt       string `json:"expires_at"`
}

func BuildQRPayload(server, hostDeviceID, pairingID, secret, hostFP string, expires time.Time) (string, error) {
	p := QRPayload{
		Version:         1,
		Server:          server,
		HostDeviceID:    hostDeviceID,
		PairingID:       pairingID,
		Secret:          secret,
		HostFingerprint: hostFP,
		ExpiresAt:       expires.UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(p)
	return string(b), err
}
