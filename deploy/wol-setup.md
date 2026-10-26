# Configuración de Wake-on-LAN

El encendido remoto depende de hardware, firmware, controlador y estado de
energía. No se puede garantizar desde apagado completo (S5): funciona de forma
fiable desde suspensión (S3) y, según la placa, desde hibernación (S4). Windows
distingue estos estados y documenta limitaciones del inicio rápido y de S5.

## 1. Firmware (BIOS/UEFI)

- Activa la opción de encendido por red: suele llamarse *Wake on LAN*, *Power On
  by PCIe/PCI*, *Resume by PME* o similar.
- Desactiva el *Fast Boot* del firmware si impide WoL desde apagado.

## 2. Windows — adaptador de red

```powershell
# Identificar adaptador y MAC
Get-NetAdapter | Select-Object Name, Status, MacAddress

# Estados de suspensión disponibles
powercfg /a

# Dispositivos autorizados a despertar el equipo
powercfg /devicequery wake_armed
```

En el Administrador de dispositivos, en las propiedades del adaptador Ethernet:

- Pestaña *Administración de energía*: activa "Permitir que este dispositivo
  reactive el equipo" y "Permitir solo un paquete mágico…".
- Pestaña *Opciones avanzadas*: activa "Wake on Magic Packet".

Desactiva el inicio rápido de Windows si interfiere:
`Opciones de energía → Elegir el comportamiento de los botones → Cambiar la
configuración actualmente no disponible → desmarcar "Activar inicio rápido"`.

## 3. Pruebas

- Prueba primero desde la **misma LAN** y desde **suspensión**.
- Prueba por separado hibernación y apagado; pueden comportarse distinto.

## 4. Relevo

El PC apagado no puede recibir peticiones: necesitas un intermediario encendido
en la LAN (router con contenedores, NAS, Raspberry Pi) ejecutando
`miremote-wolrelay`. Configúralo con la MAC y el broadcast del PC en
`wol-relay.json` (ver `wol-relay/wol-relay.example.json`).

El paquete WoL no autentica al remitente: la autenticación la hace el relevo
(desafío Ed25519 + rate-limit). **No expongas el puerto UDP 9 ni el relevo a
Internet.**
