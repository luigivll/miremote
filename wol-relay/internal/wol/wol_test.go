package wol

import (
	"bytes"
	"net"
	"testing"
)

func TestBuildMagicPacket(t *testing.T) {
	mac, _ := net.ParseMAC("01:02:03:04:05:06")
	p := buildMagicPacket(mac)
	if len(p) != 102 {
		t.Fatalf("longitud %d != 102", len(p))
	}
	// Primeros 6 bytes 0xFF.
	if !bytes.Equal(p[:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatal("cabecera de sincronización incorrecta")
	}
	// 16 repeticiones de la MAC.
	for i := 0; i < 16; i++ {
		if !bytes.Equal(p[6+i*6:6+i*6+6], mac) {
			t.Fatalf("repetición %d de la MAC incorrecta", i)
		}
	}
}

func TestWakeValidates(t *testing.T) {
	if err := Wake("no-es-una-mac", "192.168.1.255", 9); err == nil {
		t.Fatal("MAC inválida debería fallar")
	}
	if err := Wake("01:02:03:04:05:06", "no-es-ip", 9); err == nil {
		t.Fatal("broadcast inválido debería fallar")
	}
}
