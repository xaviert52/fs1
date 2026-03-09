package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"suscriber/internal/lock"
	"suscriber/internal/metrics"
	"suscriber/internal/redisclient"
	"suscriber/internal/rediskeys"
	"suscriber/internal/types"
)

type apiResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func writeJSON(w http.ResponseWriter, code int, status, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(apiResponse{
		Status:  status,
		Message: message,
		Data:    data,
	})
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, "error", message, nil)
}

type LocalSubscription struct {
	ID         string
	ClientChan chan types.StatusUpdate
	cancelFunc context.CancelFunc
}

type Service struct {
	mu         sync.RWMutex
	devMode    bool              // Nuevo flag
	memStore   map[string]string // Memoria para status (simula Redis)
	redis      redis.UniversalClient
	instanceID string
	localSubs  map[string][]*LocalSubscription
	shutdownCh chan struct{}
	pubSub     *redis.PubSub
	metrics    *metrics.Metrics
	upgrader   websocket.Upgrader
}

// Modificado para aceptar isDev
func New(isDev bool) *Service {
	inst := os.Getenv("INSTANCE_ID")
	if strings.TrimSpace(inst) == "" {
		inst = fmt.Sprintf("instance-%s", uuid.New().String()[:8])
	}

	s := &Service{
		devMode:    isDev,
		instanceID: inst,
		localSubs:  make(map[string][]*LocalSubscription),
		memStore:   make(map[string]string), // Inicializar mapa de memoria
		shutdownCh: make(chan struct{}),
		metrics:    metrics.New(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	// Solo conectar a Redis si NO estamos en modo desarrollo
	if !isDev {
		client := redisclient.NewUniversal()
		s.redis = client
		s.pubSub = client.Subscribe(context.Background(), rediskeys.KeyGlobalChannel)
		go s.heartbeatLoop()
		go s.listenGlobalUpdates()
	}

	return s
}

func (s *Service) heartbeatLoop() {
	if s.devMode {
		return
	} // No heartbeat en memoria
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx := context.Background()
			key := rediskeys.KeyHeartbeat + s.instanceID
			_ = s.redis.SetEx(ctx, key, fmt.Sprint(time.Now().Unix()), rediskeys.HeartbeatTTL).Err()
			s.updateInstanceSubscriptions()
			s.metrics.InstanceHealth.Set(1)
		case <-s.shutdownCh:
			return
		}
	}
}

func (s *Service) updateInstanceSubscriptions() {
	if s.devMode {
		return
	}
	s.mu.RLock()
	uuids := make([]string, 0, len(s.localSubs))
	for u := range s.localSubs {
		uuids = append(uuids, u)
	}
	s.mu.RUnlock()
	ctx := context.Background()
	instanceKey := fmt.Sprintf("%s%s:subs", rediskeys.KeyInstanceSubs, s.instanceID)
	for _, u := range uuids {
		_ = s.redis.SAdd(ctx, instanceKey, u).Err()
	}
	_ = s.redis.Expire(ctx, instanceKey, rediskeys.InstanceTTL).Err()
}

func (s *Service) listenGlobalUpdates() {
	if s.devMode {
		return
	}
	ch := s.pubSub.Channel()
	for msg := range ch {
		var update types.StatusUpdate
		if err := json.Unmarshal([]byte(msg.Payload), &update); err != nil {
			continue
		}
		s.metrics.UpdatesReceived.Inc()
		s.notifyLocalSubscribers(update.UUID, update)
	}
}

func (s *Service) registerInstanceSubscription(processUUID string) {
	if s.devMode {
		return
	}
	ctx := context.Background()
	sKey := rediskeys.KeySubscribers + processUUID
	_ = s.redis.SAdd(ctx, sKey, s.instanceID).Err()
	_ = s.redis.Expire(ctx, sKey, rediskeys.InstanceTTL).Err()
	iKey := fmt.Sprintf("%s%s:subs", rediskeys.KeyInstanceSubs, s.instanceID)
	_ = s.redis.SAdd(ctx, iKey, processUUID).Err()
	_ = s.redis.Expire(ctx, iKey, rediskeys.InstanceTTL).Err()
}

func (s *Service) sendCurrentStatus(processUUID string, conn *websocket.Conn) {
	var val string
	var err error

	if s.devMode {
		// Leer de memoria
		s.mu.RLock()
		v, ok := s.memStore[processUUID]
		s.mu.RUnlock()
		if !ok {
			return
		}
		val = v
	} else {
		// Leer de Redis
		ctx := context.Background()
		key := rediskeys.KeyStatusPrefix + processUUID
		val, err = s.redis.Get(ctx, key).Result()
		if err != nil {
			return
		}
	}

	var status types.ProcessStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		return
	}
	update := types.StatusUpdate{
		UUID:      processUUID,
		Status:    status,
		Timestamp: time.Now(),
	}
	_ = conn.WriteJSON(update)
}

func (s *Service) notifyLocalSubscribers(id string, update types.StatusUpdate) {
	s.mu.RLock()
	subs, ok := s.localSubs[id]
	s.mu.RUnlock()
	if !ok {
		return
	}
	for _, sub := range subs {
		select {
		case sub.ClientChan <- update:
		default:
		}
	}
}

// Status godoc
// @Summary Consultar estado actual
// @Tags status
// @Produce json
// @Param uuid query string true "UUID del proceso" format(uuid)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /status [get]
func (s *Service) Status(w http.ResponseWriter, r *http.Request) {
	processUUID := r.URL.Query().Get("uuid")
	if _, err := uuid.Parse(processUUID); err != nil {
		writeError(w, http.StatusBadRequest, "UUID inválido")
		return
	}

	var val string
	var err error
	if s.devMode {
		s.mu.RLock()
		v, ok := s.memStore[processUUID]
		s.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusNotFound, "No encontrado")
			return
		}
		val = v
	} else {
		ctx := context.Background()
		key := rediskeys.KeyStatusPrefix + processUUID
		val, err = s.redis.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				writeError(w, http.StatusNotFound, "No encontrado")
				return
			}
			writeError(w, http.StatusBadGateway, "Error Redis")
			return
		}
	}
	var status types.ProcessStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		writeError(w, http.StatusInternalServerError, "Estado almacenado inválido")
		return
	}
	resp := types.StatusUpdate{
		UUID:      processUUID,
		Status:    status,
		Timestamp: time.Now(),
	}
	writeJSON(w, http.StatusOK, "success", "Consulta exitosa", resp)
}

// Subscribe godoc
// @Summary Suscribirse a actualizaciones
// @Tags subscription
// @Param uuid query string true "UUID del proceso" format(uuid)
// @Success 101 {string} string "Switching Protocols"
// @Router /subscribe [get]
func (s *Service) Subscribe(w http.ResponseWriter, r *http.Request) {
	processUUID := r.URL.Query().Get("uuid")
	// Permitir cualquier string en modo dev para facilitar pruebas, o validar siempre
	if _, err := uuid.Parse(processUUID); err != nil {
		writeError(w, http.StatusBadRequest, "UUID inválido")
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	localSub := &LocalSubscription{
		ID:         uuid.New().String(),
		ClientChan: make(chan types.StatusUpdate, 50),
		cancelFunc: cancel,
	}
	s.mu.Lock()
	s.localSubs[processUUID] = append(s.localSubs[processUUID], localSub)
	count := len(s.localSubs[processUUID])
	s.mu.Unlock()

	s.metrics.Subscriptions.WithLabelValues(s.instanceID, processUUID).Set(float64(count))

	if count == 1 {
		s.registerInstanceSubscription(processUUID)
	}
	go s.sendCurrentStatus(processUUID, conn)
	go s.writerLoop(conn, localSub)
	go s.readerLoop(conn, ctx, processUUID, localSub.ID)
}

func (s *Service) writerLoop(conn *websocket.Conn, sub *LocalSubscription) {
	defer conn.Close()
	for {
		select {
		case msg, ok := <-sub.ClientChan:
			if !ok {
				return
			}
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
		}
	}
}

func (s *Service) readerLoop(conn *websocket.Conn, ctx context.Context, uuidStr, subID string) {
	defer func() {
		conn.Close()
		s.cleanupLocalSubscription(uuidStr, subID)
	}()
	conn.SetReadLimit(1 << 20)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (s *Service) cleanupLocalSubscription(uuidStr, subID string) {
	s.mu.Lock()
	list := s.localSubs[uuidStr]
	n := make([]*LocalSubscription, 0, len(list))
	for _, v := range list {
		if v.ID != subID {
			n = append(n, v)
		}
	}
	if len(n) == 0 {
		delete(s.localSubs, uuidStr)
	} else {
		s.localSubs[uuidStr] = n
	}
	count := len(s.localSubs[uuidStr])
	s.mu.Unlock()

	s.metrics.Subscriptions.WithLabelValues(s.instanceID, uuidStr).Set(float64(count))

	if count == 0 && !s.devMode {
		ctx := context.Background()
		sKey := rediskeys.KeySubscribers + uuidStr
		_ = s.redis.SRem(ctx, sKey, s.instanceID).Err()
		iKey := fmt.Sprintf("%s%s:subs", rediskeys.KeyInstanceSubs, s.instanceID)
		_ = s.redis.SRem(ctx, iKey, uuidStr).Err()
	}
}

// UpdateStatus godoc
// @Summary Actualizar estado
// @Tags status
// @Accept json
// @Produce json
// @Param update body types.StatusUpdate true "Datos de actualización"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 502 {object} map[string]interface{}
// @Router /update-status [post]
func (s *Service) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UUID      string              `json:"uuid"`
		Status    types.ProcessStatus `json:"status"`
		Timestamp string              `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if _, err := uuid.Parse(input.UUID); err != nil {
		writeError(w, http.StatusBadRequest, "UUID inválido")
		return
	}
	if !types.IsValidStatus(input.Status) {
		vals := make([]string, 0, len(types.ValidStatuses()))
		for _, v := range types.ValidStatuses() {
			vals = append(vals, string(v))
		}
		writeError(w, http.StatusBadRequest, "Estado inválido. Valores permitidos: "+strings.Join(vals, ", "))
		return
	}
	var ts time.Time
	if strings.TrimSpace(input.Timestamp) != "" {
		t, err := time.Parse(time.RFC3339, input.Timestamp)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Timestamp inválido, formato RFC3339 (ej: 2026-03-03T23:17:00Z)")
			return
		}
		ts = t
	} else {
		loc, err := time.LoadLocation("America/Guayaquil")
		if err != nil {
			loc = time.FixedZone("ECT", -5*60*60)
		}
		ts = time.Now().In(loc)
	}
	update := types.StatusUpdate{
		UUID:      input.UUID,
		Status:    input.Status,
		Timestamp: ts,
	}

	statusJSON, _ := json.Marshal(update.Status)

	if s.devMode {
		// Lógica en MEMORIA
		s.mu.Lock()
		s.memStore[update.UUID] = string(statusJSON)
		s.mu.Unlock()
		// Notificar directamente
		s.notifyLocalSubscribers(update.UUID, update)
	} else {
		// Lógica REDIS
		ctx := context.Background()
		start := time.Now()
		err := s.redis.SetEx(ctx, rediskeys.KeyStatusPrefix+update.UUID, string(statusJSON), rediskeys.StatusTTL).Err()
		observeRedis(s.metrics, "set", start)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Error Redis")
			return
		}
		payload, _ := json.Marshal(update)
		start = time.Now()
		_ = s.redis.Publish(ctx, rediskeys.KeyGlobalChannel, payload).Err()
		observeRedis(s.metrics, "publish", start)
		s.metrics.UpdatesPublished.Inc()
		s.notifyLocalSubscribers(update.UUID, update)

		if update.Status == types.StatusCompleted || update.Status == types.StatusFailed {
			go s.distributedCleanup(update.UUID)
		}
	}

	writeJSON(w, http.StatusOK, "success", "Estado actualizado", map[string]interface{}{
		"instance":  s.instanceID,
		"update":    update,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *Service) distributedCleanup(uuidStr string) {
	if s.devMode {
		return
	}
	time.Sleep(5 * time.Minute)
	l, err := lock.AcquireCleanupLock(s.redis, uuidStr, 30*time.Second)
	if err != nil || l == nil {
		return
	}
	defer lock.ReleaseCleanupLock(l)
	ctx := context.Background()
	sKey := rediskeys.KeySubscribers + uuidStr
	instances, err := s.redis.SMembers(ctx, sKey).Result()
	if err != nil {
		return
	}
	for _, inst := range instances {
		hKey := rediskeys.KeyHeartbeat + inst
		exists, _ := s.redis.Exists(ctx, hKey).Result()
		if exists == 0 {
			_ = s.redis.SRem(ctx, sKey, inst).Err()
			iKey := fmt.Sprintf("%s%s:subs", rediskeys.KeyInstanceSubs, inst)
			_ = s.redis.SRem(ctx, iKey, uuidStr).Err()
		}
	}
	cnt, _ := s.redis.SCard(ctx, sKey).Result()
	if cnt == 0 {
		stKey := rediskeys.KeyStatusPrefix + uuidStr
		_ = s.redis.Del(ctx, stKey, sKey).Err()
	}
	s.mu.Lock()
	delete(s.localSubs, uuidStr)
	s.mu.Unlock()
}

func observeRedis(m *metrics.Metrics, op string, start time.Time) {
	if m == nil || m.RedisLatency == nil {
		return
	}
	m.RedisLatency.WithLabelValues(op).Observe(time.Since(start).Seconds())
}

// Health godoc
// @Summary Estado del servicio
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /health [get]
func (s *Service) Health(w http.ResponseWriter, r *http.Request) {
	var active int
	var redisStatus string

	if s.devMode {
		redisStatus = "disabled (dev mode)"
		active = 0
	} else {
		redisStatus = "connected"
		ctx := context.Background()
		var cursor uint64
		prefix := rediskeys.KeyHeartbeat
		for {
			keys, next, err := s.redis.Scan(ctx, cursor, prefix+"*", 100).Result()
			if err != nil {
				redisStatus = fmt.Sprintf("error: %v", err)
				break
			}
			active += len(keys)
			if next == 0 {
				break
			}
			cursor = next
		}
	}

	s.mu.RLock()
	localUUIDs := len(s.localSubs)
	totalLocal := 0
	for _, subs := range s.localSubs {
		totalLocal += len(subs)
	}
	s.mu.RUnlock()
	data := map[string]interface{}{
		"status":      "healthy",
		"mode":        map[bool]string{true: "development", false: "production"}[s.devMode],
		"instance_id": s.instanceID,
		"timestamp":   time.Now().Format(time.RFC3339),
		"local": map[string]interface{}{
			"active_uuids":        localUUIDs,
			"total_subscriptions": totalLocal,
		},
		"cluster": map[string]interface{}{
			"active_instances": active,
			"redis_status":     redisStatus,
		},
	}
	writeJSON(w, http.StatusOK, "success", "OK", data)
}

func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", s.Subscribe)
	mux.HandleFunc("/update-status", s.UpdateStatus)
	mux.HandleFunc("/status", s.Status)
	mux.HandleFunc("/health", s.Health)
	return mux
}
