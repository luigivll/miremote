// Package identity gestiona la identidad criptográfica persistente del agente:
//
//   - Clave Ed25519 del dispositivo (identidad ante la señalización).
//   - Certificado WebRTC/DTLS PERSISTENTE. Esto es clave: para que el móvil
//     pueda vincular la huella DTLS a la identidad emparejada, el PC debe
//     presentar SIEMPRE el mismo certificado. Si se regenerase en cada sesión,
//     la huella cambiaría y la verificación no serviría de nada.
//
// Las claves privadas se guardan protegidas en disco. En Windows la protección
// adicional (DPAPI) la aporta la capa de almacenamiento del paquete config;
// aquí el material se serializa en PEM y se escribe con permisos restrictivos.
package identity

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/pion/webrtc/v4"
)

type Identity struct {
	DeviceID   string
	PublicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey

	// Certificado DTLS persistente para Pion.
	dtlsCert    webrtc.Certificate
	dtlsCertPEM string
	dtlsKeyPEM  string
}

// Load carga la identidad desde `dir`, generándola la primera vez.
// `protect` y `unprotect` permiten envolver los bytes antes de escribir / tras
// leer (p.ej. DPAPI en Windows). Si son nil, se escribe en claro con permisos
// 0600 (aceptable en Linux/macOS con ACL correctas; en Windows usar DPAPI).
func Load(dir string, protect, unprotect func([]byte) ([]byte, error)) (*Identity, error) {
	if protect == nil {
		protect = func(b []byte) ([]byte, error) { return b, nil }
	}
	if unprotect == nil {
		unprotect = func(b []byte) ([]byte, error) { return b, nil }
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	id := &Identity{}

	// --- Clave Ed25519 ---
	edPath := filepath.Join(dir, "device_ed25519.key")
	if raw, err := os.ReadFile(edPath); err == nil {
		dec, err := unprotect(raw)
		if err != nil {
			return nil, fmt.Errorf("desproteger clave ed25519: %w", err)
		}
		if len(dec) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("clave ed25519 corrupta")
		}
		id.privateKey = ed25519.PrivateKey(dec)
		id.PublicKey = id.privateKey.Public().(ed25519.PublicKey)
	} else {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		enc, err := protect(priv)
		if err != nil {
			return nil, err
		}
		if err := writeFile0600(edPath, enc); err != nil {
			return nil, err
		}
		id.privateKey = priv
		id.PublicKey = pub
	}

	id.DeviceID = deviceIDFromPub(id.PublicKey)

	// --- Certificado DTLS persistente ---
	certPath := filepath.Join(dir, "dtls_cert.pem")
	keyPath := filepath.Join(dir, "dtls_key.pem")
	certPEM, certErr := os.ReadFile(certPath)
	keyRaw, keyErr := os.ReadFile(keyPath)

	if certErr == nil && keyErr == nil {
		keyPEM, err := unprotect(keyRaw)
		if err != nil {
			return nil, fmt.Errorf("desproteger clave dtls: %w", err)
		}
		cert, err := certificateFromPEM(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("cargar certificado dtls: %w", err)
		}
		id.dtlsCert = cert
		id.dtlsCertPEM = string(certPEM)
		id.dtlsKeyPEM = string(keyPEM)
	} else {
		cert, certPEM, keyPEM, err := generateDTLSCertificate()
		if err != nil {
			return nil, err
		}
		encKey, err := protect([]byte(keyPEM))
		if err != nil {
			return nil, err
		}
		if err := writeFile0600(certPath, []byte(certPEM)); err != nil {
			return nil, err
		}
		if err := writeFile0600(keyPath, encKey); err != nil {
			return nil, err
		}
		id.dtlsCert = cert
		id.dtlsCertPEM = certPEM
		id.dtlsKeyPEM = keyPEM
	}

	return id, nil
}

// Sign firma un mensaje (p.ej. un nonce) con la clave Ed25519 del dispositivo.
func (id *Identity) Sign(msg []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(id.privateKey, msg))
}

// PublicKeyB64 devuelve la clave pública en base64 estándar.
func (id *Identity) PublicKeyB64() string {
	return base64.StdEncoding.EncodeToString(id.PublicKey)
}

// Certificate devuelve el certificado DTLS persistente para Pion.
func (id *Identity) Certificate() webrtc.Certificate { return id.dtlsCert }

// DTLSFingerprint devuelve la huella SHA-256 del certificado DTLS, en el mismo
// formato que aparece en el SDP (a:b:c:...). El móvil debe comprobar que la
// huella del SDP recibido coincide con esta, anclada a la identidad emparejada.
func (id *Identity) DTLSFingerprint() (string, error) {
	fps, err := id.dtlsCert.GetFingerprints()
	if err != nil {
		return "", err
	}
	for _, fp := range fps {
		if fp.Algorithm == "sha-256" {
			return fp.Value, nil
		}
	}
	if len(fps) > 0 {
		return fps[0].Value, nil
	}
	return "", fmt.Errorf("sin huellas disponibles")
}

func deviceIDFromPub(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	id := base64.RawURLEncoding.EncodeToString(sum[:])[:16]
	return "p-" + id[2:]
}

// PublicKeyFingerprint devuelve sha256:hex de la clave pública Ed25519.
func (id *Identity) PublicKeyFingerprint() string {
	sum := sha256.Sum256(id.PublicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// --- Certificado DTLS ---

func generateDTLSCertificate() (webrtc.Certificate, string, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return webrtc.Certificate{}, "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "MiRemote Host"},
		NotBefore:    time.Now().Add(-time.Hour),
		// Vida larga: el certificado es persistente a propósito.
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	// SignatureAlgorithm explícito: debe coincidir con la clave ECDSA para que
	// el DER sea estable y reutilizable.
	tmpl.SignatureAlgorithm = x509.ECDSAWithSHA256
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return webrtc.Certificate{}, "", "", err
	}
	x509Cert, err := x509.ParseCertificate(der)
	if err != nil {
		return webrtc.Certificate{}, "", "", err
	}
	// CertificateFromX509 reutiliza el certificado tal cual, SIN re-firmarlo.
	// NewCertificate, en cambio, vuelve a crear el DER y cambiaría la huella en
	// cada carga, rompiendo el anclaje de la huella DTLS a la identidad.
	cert := webrtc.CertificateFromX509(priv, x509Cert)

	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return webrtc.Certificate{}, "", "", err
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return cert, certPEM, keyPEM, nil
}

func certificateFromPEM(certPEM, keyPEM []byte) (webrtc.Certificate, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return webrtc.Certificate{}, fmt.Errorf("PEM de certificado inválido")
	}
	x509Cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return webrtc.Certificate{}, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return webrtc.Certificate{}, fmt.Errorf("PEM de clave inválido")
	}
	priv, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return webrtc.Certificate{}, err
	}
	// CertificateFromX509: reutiliza el DER cargado sin re-firmar, de modo que
	// la huella es idéntica a la de la primera generación.
	return webrtc.CertificateFromX509(priv, x509Cert), nil
}

func writeFile0600(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
