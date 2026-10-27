# Servidor de señalización en la nube (siempre activo)

El servidor ahora **arranca sin configuración** (genera su propio secreto) y
funciona por HTTP tras el TLS del proveedor. Tras desplegarlo tendrás una URL
`https://...`; esa es la que pones en el campo **"Servidor de señalización"**
del agente de PC y lo que usará el móvil.

Elige **una** de estas opciones.

---

## Opción A — Render.com (gratis, sin tarjeta, desde tu GitHub) — la más fácil

Render despliega directamente desde este repositorio con el `Dockerfile`.

1. Entra en https://render.com y regístrate con tu cuenta de GitHub.
2. **New → Web Service** → conecta el repo `luigivll/miremote`.
3. Render detecta el `Dockerfile` y `render.yaml`. Deja el plan **Free** y crea.
4. Al terminar te da una URL tipo `https://miremote-signaling.onrender.com`.
   Esa es tu URL de servidor.

Pega esa URL en el agente (campo del servidor) → Guardar → Generar QR.

> Nota del plan gratis: si nadie lo usa durante ~15 min, se "duerme"; la
> siguiente conexión lo despierta (tarda ~1 minuto esa primera vez). Para algo
> sin esperas, usa la Opción B.

---

## Opción B — Fly.io (siempre activo, sin dormirse) — la mejor experiencia

Requiere instalar `flyctl` y una cuenta de Fly (pide tarjeta para verificar;
una app tan pequeña entra en el uso mínimo, coste ~0–2 $/mes).

```bash
# 1. Instalar flyctl
#    Windows (PowerShell):  iwr https://fly.io/install.ps1 -useb | iex
#    macOS/Linux:           curl -L https://fly.io/install.sh | sh

# 2. Entrar
fly auth signup      # o: fly auth login

# 3. En la carpeta del repo (ya trae Dockerfile y fly.toml):
fly launch --no-deploy --copy-config --name miremote-TUNOMBRE
fly volumes create miremote_data --region mad --size 1 -y
fly deploy
```

Al terminar: `fly status` te muestra la URL `https://miremote-TUNOMBRE.fly.dev`.

---

## Opción C — Google Cloud Run (gratis, tu cuenta Google)

Escala a cero (se despierta al conectar). Necesita un proyecto de Google Cloud
con facturación activada (el nivel gratuito no cobra para este uso).

```bash
gcloud auth login
gcloud config set project TU_PROYECTO
gcloud run deploy miremote-signaling \
  --source . --region europe-southwest1 \
  --allow-unauthenticated --port 8080
```

Te devuelve una URL `https://miremote-signaling-xxxxx.a.run.app`.

> Cloud Run no guarda disco entre reinicios: si se duerme mucho tiempo puede que
> tengas que volver a emparejar el móvil. Para persistencia permanente, usa Fly
> (Opción B), que monta un volumen.

---

## Después de desplegar (cualquier opción)

1. Copia la URL `https://...` que te dio el proveedor.
2. En **MiRemoteAgent.exe** → campo **"Servidor de señalización"** pega esa URL
   → **Guardar**.
3. **Generar código QR** → escanéalo con la app del móvil (desde cualquier red)
   → **Aprobar** en el PC → **Iniciar agente**.

## ¿Y el TURN? (conexiones difíciles)

Con solo señalización + STUN la conexión funciona en la mayoría de redes
domésticas. Si estás detrás de un NAT estricto (algunas redes móviles o de
empresa) hará falta un servidor TURN (coturn). Es opcional y se añade después;
ver `deploy/turnserver.conf` y define `MR_TURN_SECRET` y `MR_TURN_URLS` en el
servidor.
