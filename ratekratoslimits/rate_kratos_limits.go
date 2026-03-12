// Package ratekratoslimits: Redis-backed distributed rate limiting middleware
// Provides production-grade rate limiting with Redis persistence and context-based ID extraction
// Supports flexible rate limit configurations with distinct throttling options
// Integrates with route scope filtering and span tracing
//
// ratekratoslimits: 基于 Redis 的分布式速率限制中间件
// 提供生产级别的速率限制，支持 Redis 持久化和基于上下文的键提取
// 支持灵活的速率限制配置，可实现按用户/按 IP 的限流能力
// 集成路由范围过滤和 span 追踪
package ratekratoslimits

import (
	"context"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/ratelimit"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-redis/redis_rate/v10"
	"github.com/yylego/kratos-auth/authkratos"
	"github.com/yylego/neatjson/neatjsons"
)

type Config struct {
	routeScope *authkratos.RouteScope
	redisCache *redis_rate.Limiter
	redisLimit *redis_rate.Limit
	keyFromCtx func(ctx context.Context) (string, bool)
	spanHooks  []authkratos.NewSpanHookFunc
	debugMode  bool
}

func NewConfig(
	routeScope *authkratos.RouteScope,
	redisCache *redis_rate.Limiter,
	redisLimit *redis_rate.Limit,
	keyFromCtx func(ctx context.Context) (string, bool),
) *Config {
	return &Config{
		routeScope: routeScope,
		redisCache: redisCache,
		redisLimit: redisLimit,
		keyFromCtx: keyFromCtx,
		spanHooks:  nil,
		debugMode:  false,
	}
}

func (c *Config) WithDebugMode(debugMode bool) *Config {
	c.debugMode = debugMode
	return c
}

// WithNewSpanHook appends a span hook creation callback
//
// WithNewSpanHook 追加一个 span 钩子创建回调函数
func (c *Config) WithNewSpanHook(fn authkratos.NewSpanHookFunc) *Config {
	c.spanHooks = append(c.spanHooks, fn)
	return c
}

func NewMiddleware(cfg *Config, logger log.Logger) middleware.Middleware {
	slog := log.NewHelper(logger)
	slog.Infof(
		"rate-kratos-limits: new middleware side=%v operations=%d rate=%v debug-mode=%v",
		cfg.routeScope.Side,
		len(cfg.routeScope.OperationSet),
		cfg.redisLimit.String(),
		authkratos.BooleanToNum(cfg.debugMode),
	)
	if cfg.debugMode {
		slog.Debugf("rate-kratos-limits: new middleware route-scope: %s", neatjsons.S(cfg.routeScope))
	}
	return selector.Server(middlewareFunc(cfg, logger)).Match(matchFunc(cfg, logger)).Build()
}

func matchFunc(cfg *Config, logger log.Logger) selector.MatchFunc {
	slog := log.NewHelper(logger)

	return func(ctx context.Context, operation string) bool {
		defer authkratos.RunSpanHooks(ctx, cfg.spanHooks, "rate-kratos-limits-match")()

		match := cfg.routeScope.Match(operation)
		if cfg.debugMode {
			if match {
				slog.Debugf("rate-kratos-limits: operation=%s side=%v match=%d next -> check-rate-limit", operation, cfg.routeScope.Side, authkratos.BooleanToNum(match))
			} else {
				slog.Debugf("rate-kratos-limits: operation=%s side=%v match=%d skip -- check-rate-limit", operation, cfg.routeScope.Side, authkratos.BooleanToNum(match))
			}
		}
		return match
	}
}

func middlewareFunc(cfg *Config, logger log.Logger) middleware.Middleware {
	slog := log.NewHelper(logger)

	return func(handleFunc middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (resp interface{}, err error) {
			defer authkratos.RunSpanHooks(ctx, cfg.spanHooks, "rate-kratos-limits")()

			// 这里就是从上下文中获取唯一键
			// 通常是用户的 PK UK ID 或者 IP 地址等信息
			uniqueKey, ok := cfg.keyFromCtx(ctx)
			if !ok {
				if cfg.debugMode {
					slog.Debugf("rate-kratos-limits: reject requests key=unknown missing unique key from context")
				}
				return nil, ratelimit.ErrLimitExceed
			}

			if uniqueKey == "" {
				if cfg.debugMode {
					slog.Debugf("rate-kratos-limits: reject requests key=nothing missing unique key from context")
				}
				return nil, ratelimit.ErrLimitExceed
			}

			// 这块底层包在设计时有 AllowN 的设计
			// 这使得该函数的返回值，还得转换转换 res.Allowed > 0 时才算是通过
			res, err := cfg.redisCache.Allow(ctx, uniqueKey, *cfg.redisLimit)
			if err != nil {
				if cfg.debugMode {
					slog.Debugf("rate-kratos-limits: redis is unavailable key=%s err=%v reject requests", uniqueKey, err)
				}
				return nil, errors.ServiceUnavailable("unavailable", "rate-kratos-limits: redis is unavailable").WithCause(err)
			}
			// 当然在这种场景里 res.Allowed 的返回值只能是0或1两个值
			// 但在写逻辑时把范围放宽些，避免底层不按预期返回
			if res.Allowed <= 0 {
				if cfg.debugMode {
					slog.Debugf("rate-kratos-limits: reject requests key=%s allowed=%v remaining=%v", uniqueKey, res.Allowed, res.Remaining)
				}
				return nil, ratelimit.ErrLimitExceed
			}
			if cfg.debugMode {
				slog.Debugf("rate-kratos-limits: accept requests key=%s allowed=%v remaining=%v", uniqueKey, res.Allowed, res.Remaining)
			}
			return handleFunc(ctx, req)
		}
	}
}
