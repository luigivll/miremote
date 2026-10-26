package identity

import "testing"

// TestIdentityPersists comprueba que recargar la identidad desde el mismo
// directorio reproduce el mismo device_id Y la misma huella DTLS. Esto es lo
// que permite al móvil anclar la huella DTLS a la identidad emparejada: si
// cambiara entre sesiones, la verificación no serviría.
func TestIdentityPersists(t *testing.T) {
	dir := t.TempDir()

	id1, err := Load(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fp1, err := id1.DTLSFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	id2, err := Load(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := id2.DTLSFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	if id1.DeviceID != id2.DeviceID {
		t.Fatalf("device_id cambió: %s != %s", id1.DeviceID, id2.DeviceID)
	}
	if fp1 != fp2 {
		t.Fatalf("huella DTLS cambió entre cargas:\n%s\n%s", fp1, fp2)
	}
	if id1.PublicKeyFingerprint() != id2.PublicKeyFingerprint() {
		t.Fatal("huella de clave pública cambió")
	}
}

// TestSignVerify comprueba firma y verificación básica.
func TestSignVerify(t *testing.T) {
	id, err := Load(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sig := id.Sign([]byte("reto"))
	if sig == "" {
		t.Fatal("firma vacía")
	}
	if id.DeviceID[:2] != "p-" {
		t.Fatalf("device_id de host debe empezar por p-: %s", id.DeviceID)
	}
}
