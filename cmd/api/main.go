package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	AppName        string
	Env            string
	HTTPAddr       string
	DatabaseURL    string
	DBMaxConns     int32
	DBMinConns     int32
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	NATSURL        string
	SubjectPrefix  string
	CacheTTL       time.Duration
	SessionSecret  []byte
	SessionTTL     time.Duration
	CookieSecure   bool
	SetupToken     string
	TrustedOrigins []string
	S3Enabled      bool
	S3Bucket       string
}

type App struct {
	cfg      Config
	db       *pgxpool.Pool
	cache    *redis.Client
	nc       *nats.Conn
	js       nats.JetStreamContext
	requests atomic.Uint64
	s3       *minio.Client
}

type Item struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type ItemInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Event struct {
	Type      string    `json:"type"`
	Item      Item      `json:"item"`
	Timestamp time.Time `json:"timestamp"`
}
type Claims struct {
	Username string `json:"u"`
	Expires  int64  `json:"e"`
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func parseInt(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}
func loadConfig() (Config, error) {
	secret := env("SESSION_SECRET", "")
	if len(secret) < 32 {
		return Config{}, errors.New("SESSION_SECRET must be at least 32 characters")
	}
	ttl, err := time.ParseDuration(env("SESSION_TTL", "12h"))
	if err != nil || ttl < time.Minute {
		return Config{}, errors.New("SESSION_TTL must be a duration of at least 1m")
	}
	cacheTTL, err := time.ParseDuration(env("CACHE_TTL", "60s"))
	if err != nil || cacheTTL <= 0 {
		return Config{}, errors.New("CACHE_TTL must be a positive duration")
	}
	origins := strings.Split(env("TRUSTED_ORIGINS", "http://localhost:3000"), ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	return Config{
		AppName: env("APP_NAME", "Fast CRUD"), Env: env("APP_ENV", "production"),
		HTTPAddr:    env("HTTP_ADDR", ":3000"),
		DatabaseURL: env("DATABASE_URL", ""),
		DBMaxConns:  int32(parseInt("DB_MAX_CONNS", 20)), DBMinConns: int32(parseInt("DB_MIN_CONNS", 2)),
		RedisAddr: env("REDIS_ADDR", "localhost:6379"), RedisPassword: os.Getenv("REDIS_PASSWORD"), RedisDB: parseInt("REDIS_DB", 0),
		NATSURL: env("NATS_URL", nats.DefaultURL), SubjectPrefix: env("NATS_SUBJECT_PREFIX", "items"),
		CacheTTL: cacheTTL, SessionSecret: []byte(secret), SessionTTL: ttl,
		CookieSecure: strings.EqualFold(env("COOKIE_SECURE", "false"), "true"),
		SetupToken:   env("SETUP_TOKEN", ""), TrustedOrigins: origins,
		S3Enabled: strings.EqualFold(env("S3_ENABLED", "false"), "true"), S3Bucket: env("S3_BUCKET", "fast-crud-files"),
	}, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (a *App) sign(payload string) string {
	mac := hmac.New(sha256.New, a.cfg.SessionSecret)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (a *App) verifySigned(value string) (string, bool) {
	i := strings.LastIndexByte(value, '.')
	if i < 1 {
		return "", false
	}
	payload, sig := value[:i], value[i+1:]
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, a.cfg.SessionSecret)
	mac.Write([]byte(payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return "", false
	}
	return payload, true
}
func (a *App) issueSession(username string) string {
	payload, _ := json.Marshal(Claims{Username: username, Expires: time.Now().Add(a.cfg.SessionTTL).Unix()})
	return a.sign(base64.RawURLEncoding.EncodeToString(payload))
}
func (a *App) session(c *fiber.Ctx) (Claims, bool) {
	raw := c.Cookies("session")
	payload, ok := a.verifySigned(raw)
	if !ok {
		return Claims{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, false
	}
	var claims Claims
	if json.Unmarshal(b, &claims) != nil || claims.Username == "" || time.Now().Unix() >= claims.Expires {
		return Claims{}, false
	}
	return claims, true
}
func (a *App) setCookie(c *fiber.Ctx, name, value string, httpOnly bool, maxAge int) {
	c.Cookie(&fiber.Cookie{Name: name, Value: value, Path: "/", HTTPOnly: httpOnly, Secure: a.cfg.CookieSecure, SameSite: "Strict", MaxAge: maxAge})
}
func (a *App) csrfOK(c *fiber.Ctx) bool {
	cookie := c.Cookies("csrf")
	header := c.Get("X-CSRF-Token")
	if cookie == "" || header == "" || len(cookie) != len(header) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}
func (a *App) ensureCSRF(c *fiber.Ctx) error {
	if c.Cookies("csrf") == "" {
		t, err := randomToken(32)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		a.setCookie(c, "csrf", t, false, int((12 * time.Hour).Seconds()))
	}
	return c.Next()
}
func (a *App) requireAuth(c *fiber.Ctx) error {
	claims, ok := a.session(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "authentication required")
	}
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead && c.Method() != fiber.MethodOptions && !a.csrfOK(c) {
		return fiber.NewError(fiber.StatusForbidden, "invalid CSRF token")
	}
	c.Locals("username", claims.Username)
	return c.Next()
}
func (a *App) setupNeeded(ctx context.Context) (bool, error) {
	var count int
	err := a.db.QueryRow(ctx, `SELECT count(*) FROM app_users`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}
func (a *App) publish(eventType string, item Item) {
	body, err := json.Marshal(Event{Type: eventType, Item: item, Timestamp: time.Now().UTC()})
	if err != nil {
		log.Printf("marshal event: %v", err)
		return
	}
	go a.dispatchNotifications(body)
	if a.js == nil || a.nc == nil || a.nc.IsClosed() {
		log.Printf("durable JetStream publisher unavailable; event was not published: %s", eventType)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgID, err := randomToken(18)
	if err != nil {
		log.Printf("create NATS message id: %v", err)
		return
	}
	if _, err = a.js.Publish(a.cfg.SubjectPrefix+"."+eventType, body, nats.MsgId(msgID), nats.Context(ctx)); err != nil {
		log.Printf("durable JetStream publish failed for %s: %v", eventType, err)
	}
}
func (a *App) audit(ctx context.Context, c *fiber.Ctx, action, resource string, resourceID string, details string) {
	if strings.EqualFold(env("AUDIT_ENABLED", "true"), "false") {
		return
	}
	username, _ := c.Locals("username").(string)
	ip := ""
	ua := ""
	if !strings.EqualFold(env("AUDIT_INCLUDE_CLIENT_IP", "true"), "false") {
		ip = c.IP()
	}
	if !strings.EqualFold(env("AUDIT_INCLUDE_USER_AGENT", "true"), "false") {
		ua = c.Get("User-Agent")
	}
	_, err := a.db.Exec(ctx, `INSERT INTO audit_logs(actor,action,resource,resource_id,client_ip,user_agent,details,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now())`, username, action, resource, resourceID, ip, ua, details)
	if err != nil {
		log.Printf("audit insert failed action=%s resource=%s: %v", action, resource, err)
	}
}

func (a *App) invalidate(id int64) {
	if a.cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := a.cache.Del(ctx, fmt.Sprintf("item:%d", id)).Err(); err != nil {
		log.Printf("redis invalidate: %v", err)
	}
}
func validateInput(in ItemInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if len(in.Name) > 200 {
		return errors.New("name must be 200 characters or fewer")
	}
	if len(in.Description) > 10000 {
		return errors.New("description must be 10000 characters or fewer")
	}
	return nil
}
func (a *App) create(c *fiber.Ctx) error {
	var in ItemInput
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(400, "invalid JSON body")
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := validateInput(in); err != nil {
		return fiber.NewError(400, err.Error())
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()
	var it Item
	err := a.db.QueryRow(ctx, `INSERT INTO items(name,description) VALUES($1,$2) RETURNING id,name,description,created_at,updated_at`, in.Name, in.Description).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		log.Printf("create item: %v", err)
		return fiber.ErrInternalServerError
	}
	a.invalidate(it.ID)
	a.audit(ctx, c, "create", "item", strconv.FormatInt(it.ID, 10), "item created")
	a.publish("created", it)
	return c.Status(201).JSON(it)
}
func (a *App) list(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(c.Query("limit", "25"))
	if err != nil {
		limit = 25
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	offset, err := strconv.Atoi(c.Query("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()
	rows, err := a.db.Query(ctx, `SELECT id,name,description,created_at,updated_at FROM items ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	defer rows.Close()
	items := make([]Item, 0, limit)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return fiber.ErrInternalServerError
		}
		items = append(items, it)
	}
	if rows.Err() != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"data": items, "limit": limit, "offset": offset})
}
func (a *App) get(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id < 1 {
		return fiber.NewError(400, "id must be a positive integer")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
	defer cancel()
	if a.cache != nil {
		cacheCtx, cc := context.WithTimeout(context.Background(), 150*time.Millisecond)
		raw, e := a.cache.Get(cacheCtx, fmt.Sprintf("item:%d", id)).Bytes()
		cc()
		if e == nil {
			var it Item
			if json.Unmarshal(raw, &it) == nil {
				return c.JSON(it)
			}
		} else if e != redis.Nil {
			log.Printf("redis get: %v", e)
		}
	}
	var it Item
	err = a.db.QueryRow(ctx, `SELECT id,name,description,created_at,updated_at FROM items WHERE id=$1`, id).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if a.cache != nil {
		if raw, e := json.Marshal(it); e == nil {
			cacheCtx, cc := context.WithTimeout(context.Background(), 150*time.Millisecond)
			if e := a.cache.Set(cacheCtx, fmt.Sprintf("item:%d", id), raw, a.cfg.CacheTTL).Err(); e != nil {
				log.Printf("redis set: %v", e)
			}
			cc()
		}
	}
	return c.JSON(it)
}
func (a *App) update(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id < 1 {
		return fiber.NewError(400, "id must be a positive integer")
	}
	var in ItemInput
	if c.BodyParser(&in) != nil {
		return fiber.NewError(400, "invalid JSON body")
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := validateInput(in); err != nil {
		return fiber.NewError(400, err.Error())
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()
	var it Item
	err = a.db.QueryRow(ctx, `UPDATE items SET name=$1,description=$2,updated_at=now() WHERE id=$3 RETURNING id,name,description,created_at,updated_at`, in.Name, in.Description, id).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	a.invalidate(id)
	a.audit(ctx, c, "update", "item", strconv.FormatInt(id, 10), "item updated")
	a.publish("updated", it)
	return c.JSON(it)
}
func (a *App) delete(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id < 1 {
		return fiber.NewError(400, "id must be a positive integer")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()
	var it Item
	err = a.db.QueryRow(ctx, `DELETE FROM items WHERE id=$1 RETURNING id,name,description,created_at,updated_at`, id).Scan(&it.ID, &it.Name, &it.Description, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	a.invalidate(id)
	a.audit(ctx, c, "delete", "item", strconv.FormatInt(id, 10), "item deleted")
	a.publish("deleted", it)
	return c.SendStatus(204)
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("parse DATABASE_URL: %v", err)
	}
	pc.MaxConns = cfg.DBMaxConns
	pc.MinConns = cfg.DBMinConns
	db, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		log.Fatalf("connect PostgreSQL: %v", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		log.Fatalf("ping PostgreSQL: %v", err)
	}
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS app_users (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			actor TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			resource_id TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			details TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
		CREATE TABLE IF NOT EXISTS items (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 200),
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS idx_items_created_at ON items(created_at DESC);`)
	if err != nil {
		log.Fatalf("initialize schema: %v", err)
	}

	cache := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	if err := cache.Ping(ctx).Err(); err != nil {
		log.Printf("Redis unavailable; cache disabled: %v", err)
		cache = nil
	} else {
		defer cache.Close()
	}
	natsOpts := []nats.Option{nats.Name("fast-crud-api"), nats.Timeout(3 * time.Second)}
	if user := os.Getenv("NATS_USERNAME"); user != "" {
		natsOpts = append(natsOpts, nats.UserInfo(user, os.Getenv("NATS_PASSWORD")))
	}
	if token := os.Getenv("NATS_TOKEN"); token != "" {
		natsOpts = append(natsOpts, nats.Token(token))
	}
	nc, err := nats.Connect(cfg.NATSURL, natsOpts...)
	var js nats.JetStreamContext
	if err != nil {
		log.Printf("NATS unavailable; durable event publishing disabled: %v", err)
		nc = nil
	} else {
		defer nc.Close()
		js, err = nc.JetStream()
		if err != nil {
			log.Printf("JetStream unavailable: %v", err)
			js = nil
		} else {
			streamName := env("NATS_STREAM", "FASTCRUD_EVENTS")
			maxAge, parseErr := time.ParseDuration(env("NATS_MAX_AGE", "168h"))
			if parseErr != nil || maxAge <= 0 {
				maxAge = 168 * time.Hour
			}
			replicas := parseInt("NATS_REPLICAS", 1)
			if replicas < 1 {
				replicas = 1
			}
			if replicas > 5 {
				replicas = 5
			}
			_, streamErr := js.StreamInfo(streamName)
			if streamErr != nil {
				_, streamErr = js.AddStream(&nats.StreamConfig{
					Name:      streamName,
					Subjects:  []string{cfg.SubjectPrefix + ".>"},
					Storage:   nats.FileStorage,
					Retention: nats.LimitsPolicy,
					MaxAge:    maxAge,
					Replicas:  replicas,
					Discard:   nats.DiscardOld,
				})
			}
			if streamErr != nil {
				log.Printf("JetStream stream setup failed: %v", streamErr)
				js = nil
			}
		}
	}
	a := &App{cfg: cfg, db: db, cache: cache, nc: nc, js: js}
	if cfg.S3Enabled {
		endpoint := env("S3_ENDPOINT", "s3.amazonaws.com")
		accessKey, secretKey := env("S3_ACCESS_KEY", ""), env("S3_SECRET_KEY", "")
		if accessKey == "" || secretKey == "" || cfg.S3Bucket == "" {
			log.Fatal("S3_ENABLED=true requires S3_ACCESS_KEY, S3_SECRET_KEY, and S3_BUCKET")
		}
		client, s3err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: strings.EqualFold(env("S3_USE_SSL", "true"), "true"), Region: env("S3_REGION", "us-east-1")})
		if s3err != nil {
			log.Fatalf("configure S3 client: %v", s3err)
		}
		a.s3 = client
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
		exists, bucketErr := client.BucketExists(checkCtx, cfg.S3Bucket)
		checkCancel()
		if bucketErr != nil {
			log.Fatalf("check S3 bucket %q: %v (verify endpoint, credentials, and bucket permissions)", cfg.S3Bucket, bucketErr)
		}
		if !exists {
			log.Fatalf("S3 bucket %q does not exist; create it before starting the application", cfg.S3Bucket)
		}
	}

	app := fiber.New(fiber.Config{
		AppName: cfg.AppName, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
		BodyLimit: 11 * 1024 * 1024, DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
			}
			msg := "internal server error"
			if code < 500 {
				msg = err.Error()
			}
			return c.Status(code).JSON(fiber.Map{"error": msg})
		},
	})
	app.Use(helmet.New())
	if strings.EqualFold(env("CSP_ENABLED", "true"), "true") {
		frameAncestors := env("CSP_FRAME_ANCESTORS", "'none'")
		if frameAncestors == "none" {
			frameAncestors = "'none'"
		}
		csp := "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors " + frameAncestors +
			"; form-action 'self'; script-src 'self' https://code.jquery.com https://cdn.jsdelivr.net https://cdn.datatables.net; style-src 'self' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com https://cdn.datatables.net; img-src 'self' data: blob:; font-src 'self' data: https://cdnjs.cloudflare.com; connect-src 'self'"
		if strings.EqualFold(cfg.Env, "production") {
			csp += "; upgrade-insecure-requests"
		}
		if reportURI := strings.TrimSpace(os.Getenv("CSP_REPORT_URI")); reportURI != "" {
			csp += "; report-uri " + reportURI
		}
		header := "Content-Security-Policy"
		if strings.EqualFold(env("CSP_REPORT_ONLY", "false"), "true") {
			header = "Content-Security-Policy-Report-Only"
		}
		app.Use(func(c *fiber.Ctx) error {
			c.Set(header, csp)
			c.Set("Referrer-Policy", env("REFERRER_POLICY", "no-referrer"))
			c.Set("Permissions-Policy", env("PERMISSIONS_POLICY", "camera=(), microphone=(), geolocation=()"))
			if strings.EqualFold(env("HSTS_ENABLED", "false"), "true") && cfg.CookieSecure {
				c.Set("Strict-Transport-Security", "max-age="+env("HSTS_MAX_AGE", "31536000")+"; includeSubDomains")
			}
			return c.Next()
		})
	}
	app.Use(limiter.New(limiter.Config{Max: 120, Expiration: time.Minute, LimitReached: func(c *fiber.Ctx) error { return fiber.NewError(429, "rate limit exceeded") }}))
	app.Use(func(c *fiber.Ctx) error {
		a.requests.Add(1)
		origin := c.Get("Origin")
		if origin != "" {
			allowed := false
			for _, o := range cfg.TrustedOrigins {
				if origin == o {
					allowed = true
					break
				}
			}
			if !allowed {
				return fiber.NewError(403, "origin not allowed")
			}
			c.Set("Access-Control-Allow-Origin", origin)
			c.Set("Vary", "Origin")
			c.Set("Access-Control-Allow-Credentials", "true")
			c.Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Setup-Token")
			c.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if c.Method() == fiber.MethodOptions {
			return c.SendStatus(204)
		}
		return c.Next()
	})
	app.Use(func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api/") || c.Path() == "/" || c.Path() == "/login" || c.Path() == "/setup" {
			c.Set("Cache-Control", "no-store")
			c.Set("X-Robots-Tag", "noindex, nofollow")
		}
		return c.Next()
	})
	app.Use(a.ensureCSRF)

	app.Get("/health/live", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/health/ready", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "not_ready", "dependency": "postgres"})
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})
	app.Get("/metrics", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; version=0.0.4")
		return c.SendString(fmt.Sprintf("# HELP fast_crud_http_requests_total Total HTTP requests since process start\n# TYPE fast_crud_http_requests_total counter\nfast_crud_http_requests_total %d\n", a.requests.Load()))
	})

	app.Get("/api/setup/status", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), time.Second)
		defer cancel()
		needed, err := a.setupNeeded(ctx)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		return c.JSON(fiber.Map{"setup_required": needed})
	})
	app.Post("/api/setup/complete", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Second)
		defer cancel()
		needed, err := a.setupNeeded(ctx)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		if !needed {
			return fiber.NewError(409, "setup is already complete")
		}
		if cfg.SetupToken == "" || subtle.ConstantTimeCompare([]byte(c.Get("X-Setup-Token")), []byte(cfg.SetupToken)) != 1 {
			return fiber.NewError(403, "invalid setup token")
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
			AppName  string `json:"app_name"`
		}
		if err := c.BodyParser(&in); err != nil {
			return fiber.NewError(400, "invalid JSON body")
		}
		in.Username = strings.TrimSpace(in.Username)
		if len(in.Username) < 3 || len(in.Username) > 80 || strings.ContainsAny(in.Username, " \t\r\n") {
			return fiber.NewError(400, "username must be 3-80 characters without spaces")
		}
		if len(in.Password) < 12 || len(in.Password) > 72 {
			return fiber.NewError(400, "password must be 12-72 bytes")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		defer tx.Rollback(ctx)
		// Lock the users table to prevent two concurrent setup requests from creating separate first users.
		if _, err = tx.Exec(ctx, `LOCK TABLE app_users IN EXCLUSIVE MODE`); err != nil {
			return fiber.ErrInternalServerError
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM app_users`).Scan(&count); err != nil {
			return fiber.ErrInternalServerError
		}
		if count > 0 {
			return fiber.NewError(409, "setup is already complete")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO app_users(username,password_hash) VALUES($1,$2)`, in.Username, string(hash)); err != nil {
			return fiber.NewError(409, "could not create administrator; username may already exist")
		}
		name := strings.TrimSpace(in.AppName)
		if name == "" {
			name = cfg.AppName
		}
		if len(name) > 100 {
			return fiber.NewError(400, "app name must be 100 characters or fewer")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value) VALUES('app_name',$1) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, name); err != nil {
			return fiber.ErrInternalServerError
		}
		if err = tx.Commit(ctx); err != nil {
			return fiber.ErrInternalServerError
		}
		a.audit(ctx, c, "bootstrap_admin", "user", in.Username, "initial administrator created")
		return c.JSON(fiber.Map{"status": "setup_complete", "login_url": "/login"})
	})
	app.Post("/api/auth/login", func(c *fiber.Ctx) error {
		if !a.csrfOK(c) {
			return fiber.NewError(403, "invalid CSRF token")
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if c.BodyParser(&in) != nil {
			return fiber.NewError(400, "invalid JSON body")
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer cancel()
		var hash string
		err := db.QueryRow(ctx, `SELECT password_hash FROM app_users WHERE username=$1`, strings.TrimSpace(in.Username)).Scan(&hash)
		if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
			return fiber.NewError(401, "invalid username or password")
		}
		a.setCookie(c, "session", a.issueSession(strings.TrimSpace(in.Username)), true, int(cfg.SessionTTL.Seconds()))
		c.Locals("username", strings.TrimSpace(in.Username))
		a.audit(ctx, c, "login", "auth", strings.TrimSpace(in.Username), "local login succeeded")
		return c.JSON(fiber.Map{"status": "authenticated"})
	})
	auth := app.Group("/api", a.requireAuth)
	auth.Post("/auth/logout", func(c *fiber.Ctx) error {
		a.setCookie(c, "session", "", true, -1)
		return c.JSON(fiber.Map{"status": "logged_out"})
	})
	auth.Get("/auth/me", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"username": c.Locals("username")}) })
	v1 := auth.Group("/v1")
	v1.Get("/items", a.list)
	v1.Post("/items", a.create)
	v1.Get("/items/:id", a.get)
	v1.Put("/items/:id", a.update)
	v1.Delete("/items/:id", a.delete)
	if a.s3 != nil {
		v1.Post("/files/upload", a.uploadFile)
		v1.Get("/files", a.listFiles)
		v1.Get("/files/download", a.downloadFile)
		v1.Delete("/files", a.deleteFile)
	}
	v1.Get("/files/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"enabled": a.s3 != nil, "bucket": func() string {
			if a.s3 != nil {
				return cfg.S3Bucket
			}
			return ""
		}()})
	})
	auth.Get("/integrations/status", a.integrationStatus)
	auth.Post("/integrations/email/test", a.testEmail)
	auth.Post("/integrations/webhook/test", a.testWebhook)

	// Serve the browser application. API requests are not redirected to HTML.
	app.Static("/assets", "./web/assets")
	app.Get("/login", func(c *fiber.Ctx) error { return c.SendFile("./web/login.html") })
	app.Get("/setup", func(c *fiber.Ctx) error { return c.SendFile("./web/setup.html") })
	app.Get("/", func(c *fiber.Ctx) error {
		if _, ok := a.session(c); !ok {
			return c.Redirect("/login", 302)
		}
		return c.SendFile("./web/index.html")
	})
	app.Get("/*", func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api/") {
			return fiber.ErrNotFound
		}
		return c.SendFile("./web/404.html")
	})
	log.Printf("listening on %s", cfg.HTTPAddr)
	log.Fatal(app.Listen(cfg.HTTPAddr))
}
