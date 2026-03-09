## Alcance y Objetivos
- Implementar un servicio Go con WebSocket y HTTP/REST para suscripción y actualización de estados.
- Persistencia y difusión con Redis (caché, Pub/Sub, heartbeats, locks distribuidos).
- Balanceo con Nginx y sticky por UUID, más métricas Prometheus y healthchecks.
- Entregables: código Go, go.mod, Dockerfile, docker-compose.yml, nginx.conf, tests y README.

## Ajustes y Correcciones Clave
- WebSocket server: usar gorilla/websocket.Upgrader en lugar de websocket.Upgrade.
- Unsubscribe/limpieza: eliminar suscripción local al cerrar conexión y depurar sets en Redis cuando el último suscriptor local de un UUID desaparece.
- Reducción de KEYS: evitar KEYS en producción; usar SCAN o sets preindexados (subscribers:{uuid} y instance:{id}:subs) para descubrimiento.
- Nginx: corregir proxy_pass (sin backticks) y declarar upstreams reales; en compose clásico replicación con “deploy” es ignorada, usaremos 3 servicios explícitos (subscription-service-1..3) o escala con nombres deterministas.
- Redis Cluster: soportar dos modos vía env (standalone y cluster) creando NewClient o NewClusterClient según variables.
- Rate limiting: reemplazar INCR+Expire ad-hoc con script Lua atómico o redis-cell; manejar errores de Redis siempre.
- Pub/Sub: filtrar por UUID localmente; contabilizar y medir latencias; tolerar reintentos.

## Estructura de Proyecto
- cmd/subscription-service/main.go
- internal/redis/keys.go, client.go (standalone/cluster)
- internal/service/service.go (RedisSubscriptionService)
- internal/http/handlers.go (Subscribe, UpdateStatus, Health, /metrics)
- internal/ws/connection.go (Upgrader, ping/pong, envío)
- internal/lock/lock.go (redislock)
- internal/metrics/metrics.go
- internal/types/types.go (StatusUpdate, ProcessStatus, enums, validadores)
- Dockerfile, docker-compose.yml, nginx.conf, go.mod, README.md
- tests: internal/..._test.go y pruebas e2e ligeras (WebSocket + update)

## Dependencias
- github.com/go-redis/redis/v8 (y rediscluster en runtime cuando aplique)
- github.com/gorilla/websocket
- github.com/google/uuid
- github.com/bsm/redislock
- github.com/prometheus/client_golang
- testify para tests

## Implementación del Servicio
- Configuración por env: PORT, REDIS_ADDR, REDIS_MODE (standalone|cluster), REDIS_CLUSTER_ENDPOINTS, INSTANCE_ID opcional.
- Arranque: crear cliente Redis, inicializar métricas y Upgrader, suscribir a canal global, iniciar heartbeat.
- Heartbeat: SetEX heartbeat:{instance_id} y refresco de instance:{id}:subs con TTL; intervalo 10s.

## Endpoints HTTP
- GET /subscribe?uuid=…: valida UUID, realiza upgrade con Upgrader, registra LocalSubscription, envía estado actual (status:{uuid}) si existe y mantiene canal con ping/pong.
- POST /update-status: valida payload, persiste status:{uuid} con TTL, publica en canal global, notifica suscriptores locales; si estado terminal, programa limpieza.
- GET /health: retorna métricas básicas (instancia, suscripciones locales, clientes Redis activos, instancias activas usando SCAN heartbeat:* o conteo de sets) sin INFO costoso en caliente.
- GET /metrics: expositor Prometheus (promhttp.Handler).

## Redis y Pub/Sub
- Estructuras: status:{uuid} (JSON), subscribers:{uuid} (SET de instancia_id), instance:{id}:subs (SET de UUIDs), heartbeat:{id} (timestamp con TTL).
- Pub/Sub: canal global “global_updates”. Al recibir update, notificar solo si uuid pertenece a localSubs.
- Persistencia: StatusTTL ~10m; expiración de sets ligada a actividad.

## Limpieza Distribuida
- Trigger tras estados terminales + grace period; adquirir lock con redislock en lock:cleanup:{uuid}.
- Verificar instancias activas por heartbeats; remover referencias obsoletas en subscribers:{uuid} y instance:{id}:subs.
- Si subscribers:{uuid} vacío: borrar status:{uuid} y set de subs.
- Reconfirmar localSubs antes de borrar para evitar TOCTOU si hay nuevas suscripciones.

## WebSocket y Manejo de Conexiones
- Upgrader con límites de tamaños, CheckOrigin configurable.
- Canales con buffer y backpressure; cerrar conexión si el buffer se satura tras umbral.
- Ping/pong y timeouts (read/write deadlines) para detectar desconexiones.
- Al cierre: remover LocalSubscription; si count local para uuid llega a 0, actualizar Redis sets.

## Métricas y Observabilidad
- Counters: updates_published, updates_received.
- Gauges: active_subscriptions por instancia/uuid, instance_health.
- Histogramas: redis_operation_duration_seconds por operación.
- Logs estructurados; niveles y correlación por uuid.

## Docker y Nginx
- docker-compose.yml con:
  - redis:7-alpine
  - subscription-service-1..3: mismos build/env; exponer puertos 8081, 8082, 8083 para pruebas locales.
  - nginx: upstream con hash $arg_uuid consistent; servers apuntando a los tres contenedores.
- Quitar “deploy” (compose lo ignora). Alternativa: usar Swarm o Kubernetes si se prefiere escala dinámica.
- nginx.conf corregido: proxy_pass http://subscription_backend; timeouts, upgrade headers.

## Tests y Verificación
- Unit tests: validación de isValidStatus, serialización de StatusUpdate.
- Tests de integración (con Redis real):
  - Persistencia y TTL de status.
  - Pub/Sub y recepción local filtrada.
  - Limpieza distribuida con lock simulado.
- Test e2e: levantar stack con compose, abrir 2 WebSockets a /subscribe (mismo uuid en instancias distintas) y publicar /update-status; verificar recepción y orden parcial aceptable.

## Seguridad y Robustez
- Límite de tasa en /update-status vía Lua atómica (bucket por uuid/productor).
- CheckOrigin/CSRF: permitir orígenes esperados; TLS en proxy.
- Manejo de fallos de Redis: CB y reintentos exponenciales; degradar a solo locales si Redis cae, con aviso.
- Sanitización y tamaños máximos de payload.

## Entregables Iniciales
- Primer PR con esqueleto del servicio, go.mod, claves Redis, Upgrader, endpoints básicos, métricas, compose y nginx funcionales.
- Segundo PR con limpieza distribuida, rate limiting Lua y tests e2e.

## Próximos Pasos
- Confirmar si usaremos compose con 3 servicios explícitos o Swarm/K8s.
- Confirmar si Redis será cluster (y endpoints) en producción.
- Aceptar este plan para proceder a implementar y entregar el primer PR.