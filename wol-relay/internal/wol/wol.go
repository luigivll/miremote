// Package wol construye y envía paquetes mágicos Wake-on-LAN.
//
// Un paquete mágico son 6 bytes 0xFF seguidos de 16 repeticiones de la MAC
// de destino (102 bytes). Se envía por UDP a la dirección de broadcast de la
// LAN. El paquete NO autentica al remitente: la autorización es
// responsabilidad del relevo que llama a esta función, nunca de la red.
package wol

import (
	"fmt"
	"net"
	"time"
)

// Wake envía un paquete mágico a la MAC indicada por el broadcast dado.
// `broadcast` debe venir de configuración de confianza, no de una petición
// arbitraria, para no convertir el relevo en un reflector de paquetes.
func Wake(macText, broadcast string, port int) error {
	mac, err := net.ParseMAC(macText)
	if err != nil || len(mac) != 6 {
		return fmt.Errorf("MAC Ethernet inválida: %q", macText)
	}
	ip := net.ParseIP(broadcast)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("broadcast IPv4 inválido: %q", broadcast)
	}
	if port == 0 {
		port = 9
	}

	packet := buildMagicPacket(mac)

	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: port})
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	n, err := conn.Write(packet)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("escritura parcial del paquete WoL: %d/%d", n, len(packet))
	}
	return nil
}

func buildMagicPacket(mac net.HardwareAddr) []byte {
	packet := make([]byte, 102)
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], mac)
	}
	return packet
}
