package ratekratoslimits

import (
	"context"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/http"
	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/yylego/kratos-auth/authkratos"
)

// keyFromHeader counts requests on the X-User request field, with a prefix that keeps this limit apart
// keyFromHeader 按请求头 X-User 计数，加上前缀跟别的限流分开
func keyFromHeader(ctx context.Context) (string, bool) {
	tsp, ok := transport.FromServerContext(ctx)
	if !ok {
		return "", false
	}
	account := tsp.RequestHeader().Get("X-User")
	if account == "" {
		return "", false
	}
	return "demo-search:" + account, true
}

// newTestConfig gives a config that allows 2 requests each minute on /api.Demo/Search, with miniredis as the Redis
// newTestConfig 返回一个配置：/api.Demo/Search 每分钟放行 2 次，后面接内存 Redis
func newTestConfig(t *testing.T) (*Config, *miniredis.Miniredis) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	cfg := NewConfig(authkratos.NewInclude("/api.Demo/Search"), redis_rate.NewLimiter(client), redis_rate.PerMinute(2), keyFromHeader)
	return cfg, redisServer
}

// newTestServer starts a Kratos HTTP server with the rate limit middleware and two routes:
// /api.Demo/Search is inside the scope, /api.Demo/Health is outside it
//
// newTestServer 启动带限流中间件的 Kratos HTTP 服务，挂两个路由：
// /api.Demo/Search 在限流范围内，/api.Demo/Health 不在范围内
func newTestServer(t *testing.T, cfg *Config) *httptest.Server {
	srv := http.NewServer(http.Middleware(NewMiddleware(cfg, slog.Default())))
	route := srv.Route("/")
	for path, operation := range map[string]string{"/search": "/api.Demo/Search", "/health": "/api.Demo/Health"} {
		route.GET(path, func(ctx http.Context) error {
			http.SetOperation(ctx, operation)
			handle := ctx.Middleware(func(ctx context.Context, req any) (any, error) {
				return map[string]string{"status": "ok"}, nil
			})
			res, err := handle(ctx, nil)
			if err != nil {
				return err
			}
			return ctx.Result(200, res)
		})
	}
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)
	return server
}

// sendRequest sends a GET request as the given account and returns the status code, the body, and the Retry-After header
// sendRequest 以指定账号发一个 GET 请求，返回状态码、响应体和响应头 Retry-After
func sendRequest(t *testing.T, address string, account string) (int, string, string) {
	request, err := nethttp.NewRequest(nethttp.MethodGet, address, nil)
	require.NoError(t, err)
	if account != "" {
		request.Header.Set("X-User", account)
	}
	response, err := nethttp.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, response.Body.Close())
	}()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(body), response.Header.Get("Retry-After")
}

const limitExceedBody = `{"code":429,"reason":"RATELIMIT","message":"service unavailable due to rate limit exceeded"}`

func TestNewConfig(t *testing.T) {
	cfg, _ := newTestConfig(t)
	require.Equal(t, redis_rate.PerMinute(2), cfg.limit)
}

func TestConfig_WithExceedErk(t *testing.T) {
	cfg, _ := newTestConfig(t)
	cfg.WithExceedErk(func(ctx context.Context) *errors.Error {
		return errors.New(429, "TOO_MANY_REQUESTS", "slow down")
	})
	server := newTestServer(t, cfg)

	for range 2 {
		code, _, _ := sendRequest(t, server.URL+"/search", "alice")
		require.Equal(t, 200, code)
	}
	code, body, waitSeconds := sendRequest(t, server.URL+"/search", "alice")
	require.Equal(t, 429, code)
	require.Equal(t, `{"code":429,"reason":"TOO_MANY_REQUESTS","message":"slow down"}`, body)
	require.Equal(t, "30", waitSeconds)

	// A request with no key gets the same response, without Retry-After
	// 读不到键的请求也拿到同一个错误，不带 Retry-After
	code, body, waitSeconds = sendRequest(t, server.URL+"/search", "")
	require.Equal(t, 429, code)
	require.Equal(t, `{"code":429,"reason":"TOO_MANY_REQUESTS","message":"slow down"}`, body)
	require.Equal(t, "", waitSeconds)
}

func TestConfig_WithOutageErk(t *testing.T) {
	cfg, redisServer := newTestConfig(t)
	// The function runs on the goroutine of the request, so the cause goes back to the test in a chan
	// 函数跑在处理请求的协程里，所以 cause 经 chan 交回测试
	causes := make(chan error, 1)
	cfg.WithOutageErk(func(ctx context.Context, cause error) *errors.Error {
		causes <- cause
		return errors.ServiceUnavailable("RATE_LIMIT_OFFLINE", "rate limit redis is offline").WithCause(cause)
	})
	server := newTestServer(t, cfg)
	redisServer.Close()

	code, body, _ := sendRequest(t, server.URL+"/search", "alice")
	require.Equal(t, 503, code)
	require.Equal(t, `{"code":503,"reason":"RATE_LIMIT_OFFLINE","message":"rate limit redis is offline"}`, body)
	require.Error(t, <-causes)
}

func TestNewMiddleware(t *testing.T) {
	t.Run("each-account-gets-its-own-limit", func(t *testing.T) {
		cfg, _ := newTestConfig(t)
		server := newTestServer(t, cfg)

		for range 2 {
			code, body, waitSeconds := sendRequest(t, server.URL+"/search", "alice")
			require.Equal(t, 200, code)
			require.Equal(t, `{"status":"ok"}`, body)
			require.Equal(t, "", waitSeconds)
		}
		// 2 requests each minute means one comes back each 30 seconds
		// 每分钟 2 次，即每 30 秒回来一次额度
		code, body, waitSeconds := sendRequest(t, server.URL+"/search", "alice")
		require.Equal(t, 429, code)
		require.Equal(t, limitExceedBody, body)
		require.Equal(t, "30", waitSeconds)

		// A second account has its own quota
		// 换一个账号，额度另算
		code, _, _ = sendRequest(t, server.URL+"/search", "bob")
		require.Equal(t, 200, code)
	})

	t.Run("no-key-is-rejected", func(t *testing.T) {
		cfg, _ := newTestConfig(t)
		server := newTestServer(t, cfg)

		code, body, waitSeconds := sendRequest(t, server.URL+"/search", "")
		require.Equal(t, 429, code)
		require.Equal(t, limitExceedBody, body)
		require.Equal(t, "", waitSeconds)
	})

	t.Run("route-outside-the-scope-has-no-limit", func(t *testing.T) {
		cfg, _ := newTestConfig(t)
		server := newTestServer(t, cfg)

		for range 5 {
			code, _, _ := sendRequest(t, server.URL+"/health", "alice")
			require.Equal(t, 200, code)
		}
	})

	t.Run("redis-down-gives-503", func(t *testing.T) {
		cfg, redisServer := newTestConfig(t)
		server := newTestServer(t, cfg)
		redisServer.Close()

		code, body, _ := sendRequest(t, server.URL+"/search", "alice")
		require.Equal(t, 503, code)
		require.Equal(t, `{"code":503,"reason":"RATE_LIMIT_UNAVAILABLE","message":"rate-kratos-limits: redis is unavailable"}`, body)
	})

	t.Run("quota-comes-back-once-the-period-ends", func(t *testing.T) {
		cfg, redisServer := newTestConfig(t)
		server := newTestServer(t, cfg)

		for range 2 {
			code, _, _ := sendRequest(t, server.URL+"/search", "alice")
			require.Equal(t, 200, code)
		}
		code, _, _ := sendRequest(t, server.URL+"/search", "alice")
		require.Equal(t, 429, code)

		redisServer.FastForward(time.Minute)
		code, _, _ = sendRequest(t, server.URL+"/search", "alice")
		require.Equal(t, 200, code)
	})
}
