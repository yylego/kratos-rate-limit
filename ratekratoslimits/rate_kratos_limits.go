// Package ratekratoslimits: Redis-backed distributed rate limiting middleware
// Provides production-grade rate limiting with Redis persistence and context-based ID extraction
// Supports flexible rate limit configurations with distinct throttling options
// Integrates with route scope filtering
//
// ratekratoslimits: 基于 Redis 的分布式速率限制中间件
// 提供生产级别的速率限制，支持 Redis 持久化和基于上下文的键提取
// 支持灵活的速率限制配置，可实现按用户/按 IP 的限流能力
// 集成路由范围过滤
package ratekratoslimits

import (
	"context"
	"log/slog"
	"math"
	"strconv"

	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/ratelimit"
	"github.com/go-kratos/kratos/v3/middleware/selector"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-redis/redis_rate/v10"
	"github.com/yylego/kratos-auth/authkratos"
)

// Config holds the rate limit middleware configuration
// Combines route scope, the Redis limiter, the limit, and the function that reads the key from the context
//
// Config 保存限流中间件的配置
// 组合路由范围、Redis 限流器、限流规则，以及从 context 读取限流键的函数
type Config struct {
	routeScope *authkratos.RouteScope
	redisRate  *redis_rate.Limiter
	limit      redis_rate.Limit
	keyFromCtx func(ctx context.Context) (string, bool)
}

// NewConfig creates a rate limit config
// limit is a value such as redis_rate.PerMinute(10)
// keyFromCtx reads the key to count requests on, such as the account ID and the client IP
// When keyFromCtx gives no key (it returns false, a blank string also counts), the request is rejected as past the limit
// The count goes by the key alone, so routes in the scope share one quota; give each limit its own key prefix, such as "article-search:"+account
//
// NewConfig 创建限流配置
// limit 直接传值，比如 redis_rate.PerMinute(10)
// keyFromCtx 读出按哪个键计数，比如账号 ID 或客户端 IP
// keyFromCtx 读不到键（返回 false 或空串）时，请求按超限拒绝
// 计数只看键，范围里的几个路由共用一份额度；每个限流给键加自己的前缀，比如 "article-search:"+account
func NewConfig(
	routeScope *authkratos.RouteScope,
	redisRate *redis_rate.Limiter,
	limit redis_rate.Limit,
	keyFromCtx func(ctx context.Context) (string, bool),
) *Config {
	return &Config{
		routeScope: routeScope,
		redisRate:  redisRate,
		limit:      limit,
		keyFromCtx: keyFromCtx,
	}
}

// NewMiddleware creates the rate limit middleware: routes inside the scope count each request against the limit in Redis
// A request past the limit gets ratelimit.ErrLimitExceed (429) with a Retry-After reply header in seconds
// A request with no key gets 429 too, without Retry-After; when Redis fails, it gets 503
// Each match and each limit outcome goes to applog at debug level; a Redis failure goes at warn level
//
// NewMiddleware 创建限流中间件：范围内的路由每个请求都在 Redis 里按限流规则计数
// 超限时返回 ratelimit.ErrLimitExceed（429），并在响应头 Retry-After 写上还要等几秒
// 读不到键时也返回 429，不带 Retry-After；Redis 出错时返回 503
// 每次匹配和每次限流结果以 debug 级别写进 applog；Redis 出错以 warn 级别写入
func NewMiddleware(cfg *Config, applog *slog.Logger) middleware.Middleware {
	applog.Info(
		"rate-kratos-limits: new middleware",
		"side", cfg.routeScope.Side,
		"operations", len(cfg.routeScope.OperationSet),
		"rate", cfg.limit.String(),
	)
	applog.Debug("rate-kratos-limits: new middleware route-scope", "route-scope", cfg.routeScope)
	return selector.Server(middlewareFunc(cfg, applog)).Match(matchFunc(cfg, applog)).Build()
}

func matchFunc(cfg *Config, applog *slog.Logger) selector.MatchFunc {
	return func(ctx context.Context, operation string) bool {
		match := cfg.routeScope.Match(operation)
		applog.Debug("rate-kratos-limits: route match", "operation", operation, "side", cfg.routeScope.Side, "match", match)
		return match
	}
}

func middlewareFunc(cfg *Config, applog *slog.Logger) middleware.Middleware {
	return func(handleFunc middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			// Read the key to count on, such as the account ID and the client IP
			// 从上下文中读取计数用的键，通常是账号 ID 或客户端 IP
			uniqueKey, ok := cfg.keyFromCtx(ctx)
			if !ok || uniqueKey == "" {
				applog.Debug("rate-kratos-limits: reject requests, missing unique key from context")
				return nil, ratelimit.ErrLimitExceed
			}

			// The limiter is built around AllowN, so a pass means res.Allowed > 0
			// 底层包按 AllowN 设计，所以要看 res.Allowed > 0 才算通过
			res, err := cfg.redisRate.Allow(ctx, uniqueKey, cfg.limit)
			if err != nil {
				applog.Warn("rate-kratos-limits: redis is unavailable, reject requests", "key", uniqueKey, "reason", err)
				return nil, errors.ServiceUnavailable("RATE_LIMIT_UNAVAILABLE", "rate-kratos-limits: redis is unavailable").WithCause(err)
			}
			// With Allow, res.Allowed takes one of two values, 0 (reject) and 1 (pass); the check takes <= 0 so an odd value counts as a reject too
			// 用 Allow 时 res.Allowed 只会是 0 或 1；这里判 <= 0，底层返回意外的值也按拒绝处理
			if res.Allowed <= 0 {
				applog.Debug("rate-kratos-limits: reject requests", "key", uniqueKey, "allowed", res.Allowed, "retry-after", res.RetryAfter)
				// Retry-After (RFC 9110) tells the client how many seconds to wait, rounded up so a retry at that time passes
				// Retry-After（RFC 9110）告诉客户端要等几秒，向上取整，到点重试就能通过
				if tsp, ok := transport.FromServerContext(ctx); ok {
					tsp.ReplyHeader().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				}
				return nil, ratelimit.ErrLimitExceed
			}
			applog.Debug("rate-kratos-limits: accept requests", "key", uniqueKey, "allowed", res.Allowed, "remaining", res.Remaining)
			return handleFunc(ctx, req)
		}
	}
}
