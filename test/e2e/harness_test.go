//go:build e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/rest"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	customerClient = "agora-customer-app"
	providerClient = "agora-provider-app"
	luandaWKT      = "MULTIPOLYGON(((12.9 -9.4,13.8 -9.4,13.8 -8.5,12.9 -8.5,12.9 -9.4)))"
)

var (
	env    *stack
	pickup = point{-8.8390, 13.2340}
	target = point{-8.9200, 13.1800}
)

type point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type stack struct {
	owner       *pgxpool.Pool
	redisDirect *goredis.Client
	redisProxy  *tcpProxy
	rabbitProxy *tcpProxy
	mapbox      *mapboxStub
	sms         *smsStub
	appypay     *appypayStub
	calls       *callStub
	fcm         *fcmStub
	api         *instance
}

type instance struct {
	container *containers.Container
	baseURL   string
	stop      context.CancelFunc
	done      chan error
}

func TestMain(m *testing.M) {
	code := 1
	func() {
		cleanup, err := startStack()
		defer cleanup()
		if err != nil {
			fmt.Fprintln(os.Stderr, "e2e stack:", err)
			return
		}
		code = m.Run()
	}()
	os.Exit(code)
}

func startStack() (func(), error) {
	ctx := context.Background()
	var closers []func()
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	gin.SetMode(gin.TestMode)
	logger.MustInit(logger.Options{ServiceName: "atlas-e2e", Environment: "test", Output: "stdout", Level: "error"})

	pg, err := tcpostgres.Run(ctx, "postgis/postgis:18-3.6-alpine",
		tcpostgres.WithDatabase("atlas"), tcpostgres.WithUsername("atlas"), tcpostgres.WithPassword("atlas"), tcpostgres.BasicWaitStrategies())
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, func() { _ = pg.Terminate(context.Background()) })
	dbURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return cleanup, err
	}
	redisEndpoint, closeRedis, err := startContainer(ctx, "redis:8.8.4-alpine", "6379/tcp", nil, wait.ForListeningPort("6379/tcp"))
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, closeRedis)
	rabbitEndpoint, closeRabbit, err := startContainer(ctx, "rabbitmq:4.4.0-management-alpine", "5672/tcp",
		map[string]string{"RABBITMQ_DEFAULT_USER": "atlas", "RABBITMQ_DEFAULT_PASS": "atlas"}, wait.ForLog("Server startup complete").WithStartupTimeout(2*time.Minute))
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, closeRabbit)

	redisProxy, err := newTCPProxy(redisEndpoint)
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, redisProxy.close)
	rabbitProxy, err := newTCPProxy(rabbitEndpoint)
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, rabbitProxy.close)

	mapbox := newMapboxStub()
	closers = append(closers, mapbox.server.Close)
	sms := newSMSStub()
	closers = append(closers, sms.server.Close)
	calls := newCallStub()
	closers = append(closers, calls.server.Close)
	appypay := newAppyPayStub()
	closers = append(closers, appypay.server.Close)
	storage := newS3Stub()
	closers = append(closers, storage.server.Close)
	fcm, err := newFCMStub()
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, fcm.server.Close)

	if err := postgres.Migrate(dbURL, "up"); err != nil {
		return cleanup, err
	}
	settings := map[string]string{
		"APP_ENV": "test", "LOG_LEVEL": "error", "DATABASE_URL": dbURL, "DB_MAX_CONNS": "40", "DB_MIGRATE_ON_START": "false",
		"REDIS_ADDR": redisProxy.addr(), "RABBITMQ_URL": "amqp://atlas:atlas@" + rabbitProxy.addr() + "/", "RABBITMQ_RETRY_DELAY": "1s",
		"JWT_SECRET": uuid.NewString() + uuid.NewString(), "MFA_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		"RATE_LIMIT_REQUESTS": "1000000", "OUTBOX_POLL_INTERVAL": "50ms", "DISPATCH_WATCHER_INTERVAL": "100ms", "SSE_POLL_INTERVAL": "1s", "SSE_HEARTBEAT_INTERVAL": "1s",
		"SSE_DEAD_AFTER": "3s", "SSE_RECONNECT_SPREAD": "1s", "NEARBY_MIN_CELL_COUNT": "2", "MAPBOX_ACCESS_TOKEN": "sk.e2e", "MAPBOX_BASE_URL": mapbox.server.URL,
		"MAPBOX_TIMEOUT": "300ms", "MAPBOX_BREAKER_FAILURES": "3", "MAPBOX_BREAKER_COOLDOWN": "200ms", "FCM_PROJECT_ID": "agora-e2e", "FCM_BASE_URL": fcm.server.URL,
		"FCM_CREDENTIALS_JSON": fcm.credentials, "HTTP_SHUTDOWN_TIMEOUT": "5s", "CASTBRICK_API_KEY": "e2e", "CASTBRICK_BASE_URL": sms.server.URL,
		"CALLS_BASE_URL": calls.server.URL, "CALLS_API_KEY": "e2e", "SHARE_BASE_URL": "https://agora.test/v", "APPYPAY_CLIENT_ID": "e2e", "APPYPAY_CLIENT_SECRET": "e2e", "APPYPAY_BASE_URL": appypay.server.URL, "APPYPAY_TOKEN_URL": appypay.server.URL + "/token", "APPYPAY_RESOURCE": "e2e", "APPYPAY_GPO_METHOD": "GPO_e2e", "APPYPAY_REF_METHOD": "REF_e2e", "PAYMENT_RECONCILE_INTERVAL": "100ms", "PAYMENT_GPO_CHECK_INTERVAL": "100ms", "PAYMENT_REFERENCE_CHECK_INTERVAL": "100ms", "OTP_RESEND_COOLDOWN": "0s", "AUTH_RATE_LIMIT_REQUESTS": "100000", "R2_ENDPOINT": storage.server.URL, "R2_ACCESS_KEY_ID": "e2e", "R2_SECRET_ACCESS_KEY": "e2e", "R2_BUCKET_NAME": storageBucket,
	}
	for key, value := range settings {
		if err := os.Setenv(key, value); err != nil {
			return cleanup, err
		}
	}
	dbCfg := config.DatabaseConfig{URL: dbURL, MaxConns: 5, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute}
	owner, err := postgres.OwnerConnection(ctx, dbCfg)
	if err != nil {
		return cleanup, err
	}
	closers = append(closers, owner.Close)
	direct := goredis.NewClient(&goredis.Options{Addr: redisEndpoint})
	closers = append(closers, func() { _ = direct.Close() })

	env = &stack{owner: owner, redisDirect: direct, redisProxy: redisProxy, rabbitProxy: rabbitProxy, mapbox: mapbox, appypay: appypay, calls: calls, sms: sms, fcm: fcm}
	if err := seedPlatform(ctx, owner); err != nil {
		return cleanup, err
	}
	api, err := startInstance(ctx)
	if err != nil {
		return cleanup, err
	}
	env.api = api
	closers = append(closers, func() { api.shutdown() })
	return cleanup, nil
}

func startContainer(ctx context.Context, image, port string, environment map[string]string, strategy wait.Strategy) (string, func(), error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: image, ExposedPorts: []string{port}, Env: environment, WaitingFor: strategy},
		Started:          true,
	})
	if err != nil {
		return "", func() {}, err
	}
	closer := func() { _ = container.Terminate(context.Background()) }
	mapped, err := container.MappedPort(ctx, "5672/tcp")
	if strings.HasPrefix(port, "6379") {
		mapped, err = container.MappedPort(ctx, "6379/tcp")
	}
	if err != nil {
		return "", closer, err
	}
	host, err := container.Host(ctx)
	if err != nil {
		return "", closer, err
	}
	return net.JoinHostPort(host, mapped.Port()), closer, nil
}

const storageBucket = "agora-documents"

type storedFile struct {
	content     []byte
	contentType string
}

type s3Stub struct {
	server *httptest.Server
	mu     sync.Mutex
	files  map[string]storedFile
}

func newS3Stub() *s3Stub {
	stub := &s3Stub{files: map[string]storedFile{}}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.serve))
	return stub
}

func (s *s3Stub) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("X-Amz-Signature") == "" && r.Header.Get("Authorization") == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, found := s.files[r.URL.Path]
	switch r.Method {
	case http.MethodPut:
		content, _ := io.ReadAll(r.Body)
		s.files[r.URL.Path] = storedFile{content: content, contentType: r.Header.Get("Content-Type")}
	case http.MethodDelete:
		delete(s.files, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodHead, http.MethodGet:
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", file.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(file.content)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(file.content)
		}
	}
}

type callStub struct {
	server *httptest.Server
	mu     sync.Mutex
	last   map[string]string
	down   atomic.Bool
}

func newCallStub() *callStub {
	stub := &callStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stub.down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		stub.mu.Lock()
		stub.last = body
		stub.mu.Unlock()
		_, _ = w.Write([]byte(`{"proxy_number":"+244900111222"}`))
	}))
	return stub
}

type smsStub struct {
	server *httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newSMSStub() *smsStub {
	stub := &smsStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.bodies = append(stub.bodies, string(body))
		stub.mu.Unlock()
		_, _ = w.Write([]byte(`{"messageId":"msg-1","status":"queued","recipientCount":1}`))
	}))
	return stub
}

var otpPattern = regexp.MustCompile(`\b\d{6}\b`)

func (s *smsStub) messagesTo(phone string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, body := range s.bodies {
		if strings.Contains(body, strings.TrimPrefix(phone, "+")) {
			out = append(out, body)
		}
	}
	return out
}

func (s *smsStub) lastCode(t *testing.T, phone string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.bodies) - 1; i >= 0; i-- {
		if strings.Contains(s.bodies[i], strings.TrimPrefix(phone, "+")) {
			if code := otpPattern.FindString(s.bodies[i]); code != "" {
				return code
			}
		}
	}
	t.Fatalf("no code sent to %s", phone)
	return ""
}

func startInstance(ctx context.Context) (*instance, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cfg.HTTP.Port = port
	connections, err := containers.Connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	container, err := containers.New(cfg, "e2e", connections)
	if err != nil {
		return nil, err
	}
	server, err := rest.NewServer(container, "/api/v1", func(api *gin.RouterGroup) { rest.SetupRoutes(api, container) })
	if err != nil {
		return nil, err
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- server.Start(runCtx)
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	for range 200 {
		res, err := http.Get(base + "/live")
		if err == nil {
			_ = res.Body.Close()
			return &instance{container: container, baseURL: base, stop: stop, done: done}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	return nil, errors.New("instance did not start")
}

func (i *instance) shutdown() {
	i.stop()
	<-i.done
	_ = i.container.Close()
}

func seedPlatform(ctx context.Context, owner *pgxpool.Pool) error {
	if _, err := owner.Exec(ctx, `INSERT INTO service_zones (country_code, name, geom, timezone, currency) VALUES ('AO', 'Luanda', ST_GeogFromText($1), 'Africa/Luanda', 'AOA')`, luandaWKT); err != nil {
		return err
	}
	_, err := owner.Exec(ctx, `
		INSERT INTO service_tariffs (service_id, currency, base_fee_cents, per_km_fee_cents, per_minute_fee_cents, minimum_fee_cents, commission_bps, free_wait_seconds)
		SELECT s.id, 'AOA', t.base, t.km, t.minute, t.minimum, 1000, 2
		FROM services s JOIN (VALUES ('taxi', 30000, 13000, 1500, 45000), ('towing', 2500000, 50000, 0, 2500000), ('maintenance', 500000, 0, 0, 500000), ('removals', 4500000, 0, 0, 4500000)) AS t (slug, base, km, minute, minimum) ON t.slug = s.slug`)
	return err
}

type tcpProxy struct {
	listener net.Listener
	upstream string
	blocked  atomic.Bool
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
}

func newTCPProxy(upstream string) (*tcpProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	proxy := &tcpProxy{listener: listener, upstream: upstream, conns: map[net.Conn]struct{}{}}
	go proxy.serve()
	return proxy, nil
}

func (p *tcpProxy) addr() string { return p.listener.Addr().String() }

func (p *tcpProxy) close() { _ = p.listener.Close(); p.setBlocked(true) }

func (p *tcpProxy) setBlocked(blocked bool) {
	p.blocked.Store(blocked)
	if !blocked {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = map[net.Conn]struct{}{}
}

func (p *tcpProxy) serve() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		if p.blocked.Load() {
			_ = client.Close()
			continue
		}
		server, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		p.conns[client], p.conns[server] = struct{}{}, struct{}{}
		p.mu.Unlock()
		go pipe(client, server)
		go pipe(server, client)
	}
}

func pipe(a, b net.Conn) {
	_, _ = io.Copy(a, b)
	_ = a.Close()
	_ = b.Close()
}

type mapboxStub struct {
	server *httptest.Server
	down   atomic.Bool
	calls  atomic.Int64
}

func newMapboxStub() *mapboxStub {
	stub := &mapboxStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		stub.calls.Add(1)
		if stub.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "routes": []map[string]any{{"distance": 8000.0, "duration": 1500.0, "geometry": "polyline"}}})
	}))
	return stub
}

type fcmStub struct {
	server      *httptest.Server
	credentials string
	mu          sync.Mutex
	messages    []fcmSeen
}

type fcmSeen struct {
	OfferID string
	Type    string
	At      time.Time
}

func newFCMStub() (*fcmStub, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	stub := &fcmStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "stub", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/v1/projects/agora-e2e/messages:send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Data map[string]string `json:"data"`
			} `json:"message"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		stub.mu.Lock()
		stub.messages = append(stub.messages, fcmSeen{OfferID: body.Message.Data["offer_id"], Type: body.Message.Data["type"], At: time.Now()})
		stub.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "projects/agora-e2e/messages/1"})
	})
	stub.server = httptest.NewServer(mux)
	credentials, err := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "e2e@agora.test", "private_key": string(encoded), "private_key_id": "k1", "token_uri": stub.server.URL + "/token", "project_id": "agora-e2e",
	})
	if err != nil {
		return nil, err
	}
	stub.credentials = string(credentials)
	return stub, nil
}

func (f *fcmStub) forOffer(offerID string) []fcmSeen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fcmSeen
	for _, message := range f.messages {
		if message.OfferID == offerID {
			out = append(out, message)
		}
	}
	return out
}

type person struct {
	id     uuid.UUID
	token  string
	client string
	api    string
}

func newPerson(t *testing.T, role enums.Role) *person {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	phone := fmt.Sprintf("+24492%07d", id.ID()%10_000_000)
	_, err := env.owner.Exec(ctx, `INSERT INTO users (id, name, phone, password, is_verified, status, role) VALUES ($1, 'Pessoa Teste', $2, 'x', true, 'active', $3)`, id, phone, role.String())
	require.NoError(t, err)
	client := customerClient
	if role == enums.RoleProvider {
		client = providerClient
	}
	token, _, err := env.api.container.JWT.Generate(services.AccessTokenClaims{UserID: id, SessionID: uuid.New(), Role: role, ClientID: client})
	require.NoError(t, err)
	return &person{id: id, token: token, client: client, api: env.api.baseURL}
}

func newCustomer(t *testing.T) *person { return newPerson(t, enums.RoleCustomer) }

func newProvider(t *testing.T, category string, at point) *person {
	t.Helper()
	provider := newApprovedProvider(t, category)
	first := positionAt(at)
	first["age_s"] = 120
	res := provider.post(t, "/api/v1/providers/me/online", map[string]any{"position": first})
	require.Equal(t, http.StatusOK, res.status, res.text)
	return provider
}

func newProviderOfService(t *testing.T, serviceID string, at point) *person {
	t.Helper()
	provider := newPerson(t, enums.RoleProvider)
	_, err := env.owner.Exec(context.Background(), `INSERT INTO providers (user_id, status, service_id) VALUES ($1, 'approved', $2)`, provider.id, serviceID)
	require.NoError(t, err)
	_, err = env.owner.Exec(context.Background(), `INSERT INTO vehicles (provider_id, brand, model, year, color, plate, seats) VALUES ($1, 'Toyota', 'Corolla', 2020, 'Branco', 'LD-45-21-AF', 7)`, provider.id)
	require.NoError(t, err)
	first := positionAt(at)
	first["age_s"] = 120
	res := provider.post(t, "/api/v1/providers/me/online", map[string]any{"position": first})
	require.Equal(t, http.StatusOK, res.status, res.text)
	return provider
}

func newApprovedProvider(t *testing.T, category string) *person {
	t.Helper()
	provider := newPerson(t, enums.RoleProvider)
	ctx := context.Background()
	_, err := env.owner.Exec(ctx, `
		WITH provider AS (
			INSERT INTO providers (user_id, status, service_id)
			SELECT $1, 'approved', s.id
			FROM services s JOIN service_categories c ON c.id = s.category_id
			WHERE c.slug = $2 ORDER BY s.display_order, s.created_at LIMIT 1
			RETURNING user_id
		)
		INSERT INTO vehicles (provider_id, brand, model, year, color, plate, seats)
		SELECT user_id, 'Toyota', 'Corolla', 2020, 'Branco', 'LD-45-21-AF', 5 FROM provider`, provider.id, category)
	require.NoError(t, err)
	return provider
}

func positionAt(at point) map[string]any {
	return map[string]any{"lat": at.Lat, "lng": at.Lng, "accuracy_m": 10, "age_s": 0}
}

type result struct {
	status int
	text   string
	body   map[string]any
	header http.Header
}

func (r result) code() string {
	if failure, ok := r.body["error"].(map[string]any); ok {
		code, _ := failure["code"].(string)
		return code
	}
	return ""
}

func (r result) str(key string) string {
	value, _ := r.body[key].(string)
	return value
}

func (r result) num(key string) int64 {
	value, _ := r.body[key].(float64)
	return int64(value)
}

func (p *person) do(t *testing.T, method, path string, payload any, headers map[string]string) result {
	t.Helper()
	res, err := p.attempt(method, path, payload, headers)
	require.NoError(t, err)
	return res
}

func (p *person) attempt(method, path string, payload any, headers map[string]string) (result, error) {
	var body io.Reader = http.NoBody
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return result{}, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, p.api+path, body)
	if err != nil {
		return result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Id", p.client)
	req.Header.Set("Authorization", "Bearer "+p.token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return result{}, err
	}
	defer func() { _ = res.Body.Close() }()
	text, err := io.ReadAll(res.Body)
	if err != nil {
		return result{}, err
	}
	out := result{status: res.StatusCode, text: string(text), body: map[string]any{}, header: res.Header}
	if len(text) > 0 {
		_ = json.Unmarshal(text, &out.body)
	}
	return out, nil
}

func (p *person) post(t *testing.T, path string, payload any) result {
	t.Helper()
	return p.do(t, http.MethodPost, path, payload, nil)
}

func (p *person) get(t *testing.T, path string) result {
	t.Helper()
	return p.do(t, http.MethodGet, path, nil, nil)
}

type sseEvent struct {
	ID   string
	Type string
	Data map[string]any
	Raw  string
}

type stream struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	events []sseEvent
	closed chan struct{}
	slow   time.Duration
}

func (p *person) listen(t *testing.T, lastEventID string) *stream {
	t.Helper()
	return p.listenSlow(t, lastEventID, 0)
}

func (p *person) listenSlow(t *testing.T, lastEventID string, delay time.Duration) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.api+"/events", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("X-Client-Id", p.client)
	req.Header.Set("Authorization", "Bearer "+p.token)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	s := &stream{cancel: cancel, closed: make(chan struct{}), slow: delay}
	go s.read(res.Body)
	t.Cleanup(s.stop)
	return s
}

func (s *stream) stop() {
	s.cancel()
	<-s.closed
}

func (s *stream) read(body io.ReadCloser) {
	defer close(s.closed)
	defer func() { _ = body.Close() }()
	reader := bufio.NewReader(body)
	current := sseEvent{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "":
			if current.Type != "" || current.Raw != "" {
				if s.slow > 0 {
					time.Sleep(s.slow)
				}
				s.mu.Lock()
				s.events = append(s.events, current)
				s.mu.Unlock()
			}
			current = sseEvent{}
		case strings.HasPrefix(line, "id: "):
			current.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			current.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			current.Raw = strings.TrimPrefix(line, "data: ")
			_ = json.Unmarshal([]byte(current.Raw), &current.Data)
		}
	}
}

func (s *stream) all() []sseEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sseEvent{}, s.events...)
}

func (s *stream) ofType(eventType string) []sseEvent {
	var out []sseEvent
	for _, event := range s.all() {
		if event.Type == eventType {
			out = append(out, event)
		}
	}
	return out
}

func (s *stream) lastID() string {
	id := ""
	for _, event := range s.all() {
		if event.ID != "" {
			id = event.ID
		}
	}
	return id
}

func (s *stream) waitFor(t *testing.T, eventType string, match func(sseEvent) bool) sseEvent {
	t.Helper()
	var found sseEvent
	require.Eventually(t, func() bool {
		for _, event := range s.ofType(eventType) {
			if match == nil || match(event) {
				found = event
				return true
			}
		}
		return false
	}, 15*time.Second, 20*time.Millisecond, "no %s event", eventType)
	return found
}

func (s *stream) waitForStatus(t *testing.T, status string) sseEvent {
	t.Helper()
	return s.waitFor(t, "request.status", func(event sseEvent) bool { return event.Data["status"] == status })
}

func estimateFor(t *testing.T, customer *person, category string) (estimateID, serviceID string) {
	t.Helper()
	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": category, "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	item := res.body["items"].([]any)[0].(map[string]any)
	return item["fare_estimate_id"].(string), item["service_id"].(string)
}

func requestTaxi(t *testing.T, customer *person) result {
	t.Helper()
	estimateID, serviceID := estimateFor(t, customer, "taxi")
	res := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{
		"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": map[string]any{"lat": pickup.Lat, "lng": pickup.Lng, "reference": "Junto à farmácia"},
		"destination": target,
	}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	return res
}

func requestCategory(t *testing.T, customer *person, category string, extra map[string]any) result {
	t.Helper()
	var serviceID string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT s.id FROM services s JOIN service_categories c ON c.id = s.category_id WHERE c.slug = $1 AND s.is_active ORDER BY s.display_order LIMIT 1`, category).Scan(&serviceID))
	payload := map[string]any{"service_id": serviceID, "pickup": pickup}
	for key, value := range extra {
		payload[key] = value
	}
	res := customer.do(t, http.MethodPost, "/api/v1/requests", payload, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	return res
}

func setPolicy(t *testing.T, category, assignments string) {
	t.Helper()
	ctx := context.Background()
	var restore []any
	rows, err := env.owner.Query(ctx, `SELECT to_jsonb(p) FROM dispatch_policies p JOIN service_categories c ON c.id = p.category_id WHERE c.slug = $1`, category)
	require.NoError(t, err)
	for rows.Next() {
		var snapshot string
		require.NoError(t, rows.Scan(&snapshot))
		restore = append(restore, snapshot)
	}
	rows.Close()
	require.Len(t, restore, 1)
	_, err = env.owner.Exec(ctx, `UPDATE dispatch_policies SET `+assignments+` WHERE category_id = (SELECT id FROM service_categories WHERE slug = $1)`, category)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := env.owner.Exec(context.Background(), `
			UPDATE dispatch_policies p SET
				offer_ttl_seconds = (s.j->>'offer_ttl_seconds')::int, search_ttl_seconds = (s.j->>'search_ttl_seconds')::int,
				ack_timeout_seconds = (s.j->>'ack_timeout_seconds')::int, ack_giveup_seconds = (s.j->>'ack_giveup_seconds')::int,
				search_lead_seconds = (s.j->>'search_lead_seconds')::int, booking_only = (s.j->>'booking_only')::boolean, offer_mode = s.j->>'offer_mode', pricing_mode = s.j->>'pricing_mode', ranking = s.j->>'ranking', max_stops = (s.j->>'max_stops')::smallint, schedule_slot_minutes = (s.j->>'schedule_slot_minutes')::smallint
			FROM (SELECT $1::jsonb AS j) s WHERE p.id = (s.j->>'id')::uuid`, restore[0])
		require.NoError(t, err)
	})
}

func reasonID(t *testing.T, admin *person, scope, code string) string {
	t.Helper()
	list := admin.get(t, "/api/v1/admin/rejection-reasons?scope="+scope)
	require.Equal(t, http.StatusOK, list.status, list.text)
	items, _ := list.body["items"].([]any)
	for _, item := range items {
		reason, _ := item.(map[string]any)
		if reason["code"] == code {
			id, _ := reason["id"].(string)
			return id
		}
	}
	require.Failf(t, "unknown rejection reason", "%s/%s", scope, code)
	return ""
}

func cancelReason(t *testing.T, actor *person, kind, category, code string) string {
	t.Helper()
	list := actor.get(t, "/api/v1/cancellation-reasons?actor="+kind+"&category="+category)
	require.Equal(t, http.StatusOK, list.status, list.text)
	for _, item := range list.body["items"].([]any) {
		reason := item.(map[string]any)
		if reason["code"] == code {
			return reason["id"].(string)
		}
	}
	require.Failf(t, "unknown reason", "%s/%s/%s", kind, category, code)
	return ""
}

func cancelAs(t *testing.T, actor *person, kind, requestID string) result {
	t.Helper()
	preview := actor.get(t, "/api/v1/requests/"+requestID+"/cancellation")
	require.Equal(t, http.StatusOK, preview.status, preview.text)
	reasons := preview.body["reasons"].([]any)
	require.NotEmpty(t, reasons, kind)
	return actor.post(t, "/api/v1/requests/"+requestID+"/cancel", map[string]any{"reason_id": reasons[0].(map[string]any)["id"], "expected_fee_cents": preview.num("fee_cents")})
}
