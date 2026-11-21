// Package ratekratoslimits: Redis-backed distributed rate limiting middleware
// Provides production-grade rate limiting with Redis persistence and context-based ID extraction
// Counts on the key read from the context, such as the account ID and the client IP
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

// NewErkFunc builds the error the middleware responds with
// It runs on each request that hits the case, so it is the place to write a log when one is wanted
//
// NewErkFunc 构造中间件要返回的错误
// 每个走到这一步的请求都会调用它，想记日志就写在这里面
type NewErkFunc func(ctx context.Context) *errors.Error

// Config holds the rate limit middleware configuration
// Combines route scope, the Redis limiter, the limit, and the function that reads the key from the context
//
// Config 保存限流中间件的配置
// 组合路由范围、Redis 限流器、限流规则，以及从 context 读取限流键的函数
type Config struct {
	routeScope *authkratos.RouteScope                               // Routes that get the limit // 要限流的路由范围
	redisRate  *redis_rate.Limiter                                  // Counts requests in Redis // 在 Redis 里计数的限流器
	limit      redis_rate.Limit                                     // Requests allowed in each period // 每个周期放行多少请求
	keyFromCtx func(ctx context.Context) (string, bool)             // Reads the key to count requests on // 读出按哪个键计数
	exceedErk  NewErkFunc                                           // Builds the response when a request is past the limit, unset means ratelimit.ErrLimitExceed // 超限时构造返回的错误，不设置就用 ratelimit.ErrLimitExceed
	outageErk  func(ctx context.Context, cause error) *errors.Error // Builds the response when Redis fails, unset means the default 503 // Redis 出错时构造返回的错误，不设置就用默认的 503
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

// WithExceedErk sets the function that builds the response when a request is past the limit, a request with no key included
// Without it the middleware responds ratelimit.ErrLimitExceed (429)
// The Retry-After header stays the same with it: set on a request past the limit, absent on a request with no key
//
// WithExceedErk 设置请求超限时用来构造错误的函数，读不到键的请求也走它
// 不设置时中间件返回 ratelimit.ErrLimitExceed（429）
// 设置后响应头 Retry-After 跟原来一样：超限的请求照常写入，读不到键的请求不带
func (c *Config) WithExceedErk(newErk NewErkFunc) *Config {
	c.exceedErk = newErk
	return c
}

// WithOutageErk sets the function that builds the response when Redis fails
// The function gets the Redis error as cause, so it is the place to write a log, and to attach it with WithCause(cause) when wanted
// Without it the middleware responds 503 RATE_LIMIT_UNAVAILABLE with the Redis error attached as the cause
//
// WithOutageErk 设置 Redis 出错时用来构造错误的函数
// 函数能拿到 Redis 的错误 cause，想记日志就写在里面，想把它挂到返回的错误上就调用 WithCause(cause)
// 不设置时中间件返回 503 RATE_LIMIT_UNAVAILABLE，Redis 的错误挂在 cause 上
func (c *Config) WithOutageErk(newErk func(ctx context.Context, cause error) *errors.Error) *Config {
	c.outageErk = newErk
	return c
}

// NewMiddleware creates the rate limit middleware: routes inside the scope count each request against the limit in Redis
// A request past the limit gets ratelimit.ErrLimitExceed (429) with a Retry-After reply header in seconds
// A request with no key gets 429 too, without Retry-After; when Redis fails, it gets 503
// WithExceedErk and WithOutageErk change these two responses
// Each match and each limit outcome goes to applog at debug level; a Redis failure goes at warn level
//
// NewMiddleware 创建限流中间件：范围内的路由每个请求都在 Redis 里按限流规则计数
// 超限时返回 ratelimit.ErrLimitExceed（429），并在响应头 Retry-After 写上还要等几秒
// 读不到键时也返回 429，不带 Retry-After；Redis 出错时返回 503
// 这两种返回可以用 WithExceedErk、WithOutageErk 换成别的错误
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
				applog.Debug("rate-kratos-limits: request rejected, no key from context")
				return nil, newExceedErk(ctx, cfg)
			}

			// The limiter is built around AllowN, so a pass means res.Allowed > 0
			// 底层包按 AllowN 设计，所以要看 res.Allowed > 0 才算通过
			res, err := cfg.redisRate.Allow(ctx, uniqueKey, cfg.limit)
			if err != nil {
				applog.Warn("rate-kratos-limits: request rejected, redis is unavailable", "key", uniqueKey, "reason", err)
				if cfg.outageErk != nil {
					return nil, cfg.outageErk(ctx, err)
				}
				return nil, errors.ServiceUnavailable("RATE_LIMIT_UNAVAILABLE", "rate-kratos-limits: redis is unavailable").WithCause(err)
			}
			// With Allow, res.Allowed takes one of two values, 0 (reject) and 1 (pass); the check takes <= 0 so an odd value counts as a reject too
			// 用 Allow 时 res.Allowed 只会是 0 或 1；这里判 <= 0，底层返回意外的值也按拒绝处理
			if res.Allowed <= 0 {
				applog.Debug("rate-kratos-limits: request rejected, past the limit", "key", uniqueKey, "allowed", res.Allowed, "retry-after", res.RetryAfter)
				// Retry-After (RFC 9110) tells the client how many seconds to wait, rounded up so a retry at that time passes
				// Retry-After（RFC 9110）告诉客户端要等几秒，向上取整，到点重试就能通过
				if tsp, ok := transport.FromServerContext(ctx); ok {
					tsp.ReplyHeader().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				}
				return nil, newExceedErk(ctx, cfg)
			}
			applog.Debug("rate-kratos-limits: request accepted", "key", uniqueKey, "allowed", res.Allowed, "remaining", res.Remaining)
			return handleFunc(ctx, req)
		}
	}
}

// newExceedErk gives the error of a request past the limit: the one from WithExceedErk when set, ratelimit.ErrLimitExceed otherwise
// newExceedErk 返回超限时的错误：设置了 WithExceedErk 就用它构造，否则用 ratelimit.ErrLimitExceed
func newExceedErk(ctx context.Context, cfg *Config) *errors.Error {
	if cfg.exceedErk != nil {
		return cfg.exceedErk(ctx)
	}
	return ratelimit.ErrLimitExceed
}
