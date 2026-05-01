# Adopti Notification Service

Servicio de notificaciones asíncrono y orientado a eventos creado en **Go** para la plataforma Adopti. Este microservicio se encarga de escuchar eventos globales a través de RabbitMQ, resolver la información de contacto orgánico de los usuarios (vía Firebase Auth) y despachar notificaciones (Correos electrónicos o Push) a los destinatarios finales.

## Arquitectura

```mermaid
flowchart TD
    %% Componentes
    RabbitMQ[("RabbitMQ\n(adopti.events)")]
    NS["Notification Service\n(Go / Gin)"]
    DB[(PostgreSQL\nnotificationsdb)]
    FirebaseAuth("Firebase Auth\n(Resolver UID -> Correo)")
    SendGrid(("SendGrid API\n(Emisor Email)"))
    FCM(("Firebase Cloud Messaging\n(Emisor Push)"))
    Frontend["React Frontend\n(Cliente App)"]

    %% Flujos de datos
    Frontend -- " POST /api/device-tokens" --> NS
    RabbitMQ -- " 1. Consume Evento " --> NS
    NS -- " 2. Obtiene Correo Físico " --> FirebaseAuth
    NS -- " 3A. pet.report.* " --> SendGrid
    NS -- " 3B. chat.message.sent " --> FCM
    NS -- " 4. Registra Idempotencia e Historial " --> DB
```

- **RabbitMQ**: Cola central de eventos (intercambio `adopti.events`). El servicio auto-aprovisiona sus propias topologías al iniciar.
- **Go / Gin**: Servidor ligero de alta concurrencia.
- **PostgreSQL**: Base de datos dedicada (`notificationsdb`) para asegurar **idempotencia** y registro del estado (historial de notificaciones) sin acoplarse con otros servicios.
- **Twilio SendGrid**: Utilizado para disparar alertas vía correo electrónico (`pet.report.created`, `pet.report.reunited`).
- **Firebase Cloud Messaging (FCM)**: Utilizado para despachar notificaciones Push al navegador / aplicación móvil cuando ocurre un evento de comunicación inter-usuario (`chat.message.sent`).
- **Firebase Auth**: Integración directa orientada por Google para desentramar el `UID` puro y transformarlo dinámicamente en el correo físico verificado del destinatario.

## Estructura del Proyecto

```text
notification-service/
├── cmd/
│   └── server/
│       └── main.go           # Punto central de orquestación y arranque
├── internal/
│   ├── config/               # Extracción y parsing de variables (.env)
│   ├── domain/               # Estructuras lógicas y modelos de persistencia
│   ├── handlers/             # Controladores aislados (email.go, push.go)
│   ├── messaging/            # Interfaz AMQP (Consumer de RabbitMQ, Dispatcher de payloads)
│   ├── repository/           # Interfaz de datos y sentencias SQL a Postgres
│   └── server/               # Router principal REST API usando el framework Gin
├── migrations/               # Esquemas base (.sql) para montar localmente en la base de datos
├── Dockerfile                # Motor de compilación multi-etapa (Builder -> Servidor Alpine)
├── go.mod / go.sum           # Dependencias de paquetes nativos en Golang
└── README.md                 # Documentación estructural de despliegue
```

## Eventos Escuchados

El microservicio utiliza un envoltorio de capa y extrae automáticamente los payloads interceptando los siguientes `routingKeys`:

| Evento                  | Canal   | Descripción                                                                                     |
|-------------------------|---------|-------------------------------------------------------------------------------------------------|
| `pet.report.created`    | Email   | Llama al SDK de SendGrid informando la creación de un nuevo reporte de mascota.                 |
| `pet.report.reunited`   | Email   | Indica que un caso se consolidó exitosamente, informando al dueño de la mascota vía Mail.      |
| `chat.message.sent`     | Push    | Manda mensaje en vivo como Push al dispositivo si el frontend registró activamente el hardware. |
| `match.found`           | -       | *Futuro soporte para motor de cruce algorítmico AI.*                                            |

## Endpoints (REST API)

A pesar de ser asíncrono primariamente, se exponen algunos Endpoints por **Gin** (`:8082`):

- **`GET /health`**: Status check para integraciones de proxy en Docker.
- **`GET /api/notifications`**: Retorna el historial. (Se puede anexar `?userId={UID}` y `?status={sent|failed}`).
- **`POST /api/device-tokens`**: Registra un Token FCM generado por el FrontEnd para habilitar Notificaciones Push.
  - Body: `{ "userId": "...", "token": "..." }`

## Entornos (Environment Variables)

Para correr en producción usando Docker, se requieren como mínimo las siguientes credenciales embebidas (Idealmente en `notification-service/.env`):

```bash
# Variables del contenedor Docker
PORT=8082
LOG_LEVEL=info

# Infraestructura
RABBITMQ_URL=amqp://adopti:rabbitmq_secret@rabbitmq:5672/
POSTGRES_DSN=postgres://postgres:password@postgres-notifications:5432/notificationsdb?sslmode=disable

# SendGrid (Email)
SENDGRID_API_KEY=SG.Tu_Token_Secreto_Extendido...
SENDGRID_FROM_EMAIL=notificaciones@adopti.app

# Firebase Auth y Push
GOOGLE_APPLICATION_CREDENTIALS=/app/firebase-credentials.json
FIREBASE_CREDENTIALS=/app/firebase-credentials.json

# (Opcional) Correo interceptor manual de pruebas (Bypass para cuando los uids caen)
TEST_EMAIL=tu_correo_de_prueba@correo.com
```

## Setup de Docker Compose

El servicio corre puramente encapsulado con su instancia dedicada para persistencia. Para inicializar una compilación en frío:

```bash
# Compilar contenedor muti-etapa y encender servicio
docker compose build notification-service
docker compose up -d notification-service postgres-notifications
```
