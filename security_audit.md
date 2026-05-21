# Auditoría de Seguridad - Notification Service

Basado en la teoría de Seguridad (CIA Triad, Tácticas y Patrones Arquitectónicos):

## Cumple

### Tácticas: Resistir Ataques
*   **Encrypt Data:** Go/Gin expone únicamente el puerto **8443** con TLS mutuo. En `cmd/server/main.go` se construye un `tls.Config` con:
    *   `MinVersion: tls.VersionTLS12`.
    *   `ClientAuth: tls.RequireAndVerifyClientCert` (mTLS estricta).
    *   `ClientCAs` cargado desde el CA raíz (`ca.crt`).
    *   El servidor arranca con `ListenAndServeTLS` usando certificado y key del servicio.
*   **Authenticate Actor:** 
    *   **mTLS mutua:** `RequireAndVerifyClientCert` exige y valida el certificado de cliente contra el pool de CAs configurado.
    *   **Aplicación (JWT/Firebase):** `internal/server/router.go` implementa `requireFirebaseAuth`, un middleware Gin que valida el `Authorization: Bearer <idToken>` contra Firebase Auth. Además, restringe que un usuario solo pueda consultar sus propias notificaciones (`userId` del query param debe coincidir con el UID del token).
*   **Limit Access:** En docker-compose solo se expone el puerto **8443** internamente; el tráfico entra únicamente a través del gateway NGINX.
*   **Change Default Settings:** 
    *   No hay secrets hardcodeados en el código; todas las credenciales se inyectan por variables de entorno y se validan en `config.Load()` (`fail-fast` si falta alguna requerida).
    *   PostgreSQL se conecta con `sslmode=verify-full` y `sslrootcert=/app/certs/ca.crt`, validando identidad del servidor de base de datos.
    *   RabbitMQ se conecta vía **AMQPS 5671** (`amqps://...` en `.env`). No se usa `InsecureSkipVerify`.
    *   La imagen Alpine instala `ca-certificates` para que las llamadas TLS salientes a SendGrid y Firebase Cloud Messaging funcionen correctamente.

### Tácticas: Detectar Ataques / Recuperar
*   **Maintain Audit Trail:** Logs estructurados con `zap` (JSON en producción, desarrollo en modo debug). Endpoint de healthcheck (`/health`). Healthcheck en docker-compose realiza petición HTTPS con certificado de cliente.
*   **Idempotencia / Recuperación:** El repositorio PostgreSQL implementa upsert por `(event_id, channel)` con `ON CONFLICT`, evitando duplicados en reintentos tras fallos transitorios.

## No Cumple / Gaps conocidos
*   **Dockerfile desincronizado:** El `Dockerfile` expone `8082`, que no coincide con el puerto TLS 8443 utilizado en producción. Esto no afecta la seguridad en docker-compose (donde se expone 8443), pero debe alinearse para evitar confusiones operativas.
*   **Modo degradado de auth:** Si Firebase no inicializa (`authClient == nil`), `requireFirebaseAuth` permite el request sin autenticar y loggea una advertencia. Aunque esto evita bloquear demos del prototipo, en producción debería rechazar estrictamente (`AbortWithStatusJSON 503`).

## Decisiones del Laboratorio 5
*   **Aplicación del Secure Channel Pattern en este servicio:** Se implementó TLS 1.2+ en el puerto 8443 con autenticación mutua (`RequireAndVerifyClientCert`) en el servidor HTTP de Go. La conexión a PostgreSQL se endureció a `sslmode=verify-full` con certificado CA raíz. La comunicación con RabbitMQ se migró a AMQPS 5671 **sin** `InsecureSkipVerify`, garantizando la verificación completa de la cadena de certificados del broker. Se eliminó cualquier canal en texto plano del servicio.
