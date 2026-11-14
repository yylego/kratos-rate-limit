<!-- TEMPLATE (EN) BEGIN: BADGES -->

[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/kratos-rate-limit/release.yml?branch=main&label=BUILD)](https://github.com/yylego/kratos-rate-limit/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/kratos-rate-limit)](https://pkg.go.dev/github.com/yylego/kratos-rate-limit)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/kratos-rate-limit/main.svg)](https://coveralls.io/github/yylego/kratos-rate-limit?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.26%2B-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/kratos-rate-limit.svg)](https://github.com/yylego/kratos-rate-limit/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/kratos-rate-limit)](https://goreportcard.com/report/github.com/yylego/kratos-rate-limit)

<!-- TEMPLATE (EN) CLOSE: BADGES -->

# kratos-rate-limit

Distributed rate limiting middleware backed with Redis persistence.

---

<!-- TEMPLATE (EN) BEGIN: LANGUAGE NAVIGATION -->

## CHINESE README

[中文说明](README.zh.md)

<!-- TEMPLATE (EN) CLOSE: LANGUAGE NAVIGATION -->

## Main Features

- 🚦 **Distributed Rate Limiting**: Redis-backed rate limiting across instances
- 🔑 **Context-Based ID**: Extract unique identification from request context
- 📊 **Configurable Limits**: Use `redis_rate.Limit` with fine-grained throttling
- 🗺️ **Route Scope**: INCLUDE/EXCLUDE mode to choose which routes get rate-limited
- 🪵 **Logs**: Each route match and limit outcome goes to the logger at debug level, a Redis failure at warn level

## Installation

```bash
go get github.com/yylego/kratos-rate-limit
```

## Usage

```go
import (
    "context"
    "log/slog"

    "github.com/go-kratos/kratos/v3/transport"
    "github.com/go-kratos/kratos/v3/transport/http"
    "github.com/go-redis/redis_rate/v10"
    "github.com/redis/go-redis/v9"
    "github.com/yylego/kratos-auth/authkratos"
    "github.com/yylego/kratos-rate-limit/ratekratoslimits"
)

func newHTTPServer(client *redis.Client, logger *slog.Logger) *http.Server {
    // Routes that get the limit, written as the operation names that protoc-gen-go-http generates
    routeScope := authkratos.NewInclude("/api.Article/Search")

    // Count each account on its own: 10 requests each minute
    limit := redis_rate.PerMinute(10)
    keyFromCtx := func(ctx context.Context) (string, bool) {
        tsp, ok := transport.FromServerContext(ctx)
        if !ok {
            return "", false
        }
        account := tsp.RequestHeader().Get("X-User")
        if account == "" {
            return "", false // a request with no key is rejected
        }
        // The prefix keeps this limit apart from other limits that count the same accounts in the same Redis
        return "article-search:" + account, true
    }

    cfg := ratekratoslimits.NewConfig(routeScope, redis_rate.NewLimiter(client), limit, keyFromCtx)
    return http.NewServer(http.Middleware(ratekratoslimits.NewMiddleware(cfg, logger)))
}
```

Requests to routes in the scope get `429 RATELIMIT` when past the limit, with a `Retry-After` reply header that gives the seconds to wait; a request with no key gets `429` too, and a Redis failure gets `503`. Routes outside the scope have no limit.

The count goes by the key alone: routes in one scope share one quota, and two limits that return the same key share one count in Redis. Give each limit its own key prefix in `keyFromCtx`, as the example does with `article-search:`.

---

<!-- TEMPLATE (EN) BEGIN: STANDARD PROJECT FOOTER -->
<!-- VERSION 2025-11-25 03:52:28.131064 +0000 UTC -->

## 📄 License

MIT License - see [LICENSE](LICENSE).

---

## 💬 Contact & Feedback

Contributions are welcome! Report bugs, suggest features, and contribute code:

- 🐛 **Mistake reports?** Open an issue on GitHub with reproduction steps
- 💡 **Fresh ideas?** Create an issue to discuss
- 📖 **Documentation confusing?** Report it so we can improve
- 🚀 **Need new features?** Share the use cases to help us understand requirements
- ⚡ **Performance issue?** Help us optimize through reporting slow operations
- 🔧 **Configuration problem?** Ask questions about complex setups
- 📢 **Follow project progress?** Watch the repo to get new releases and features
- 🌟 **Success stories?** Share how this package improved the workflow
- 💬 **Feedback?** We welcome suggestions and comments

---

## 🔧 Development

New code contributions, follow this process:

1. **Fork**: Fork the repo on GitHub (using the webpage UI).
2. **Clone**: Clone the forked project (`git clone https://github.com/yourname/repo-name.git`).
3. **Navigate**: Navigate to the cloned project (`cd repo-name`)
4. **Branch**: Create a feature branch (`git checkout -b feature/xxx`).
5. **Code**: Implement the changes with comprehensive tests
6. **Testing**: (Golang project) Ensure tests pass (`go test ./...`) and follow Go code style conventions
7. **Documentation**: Update documentation to support client-facing changes
8. **Stage**: Stage changes (`git add .`)
9. **Commit**: Commit changes (`git commit -m "Add feature xxx"`) ensuring backward compatible code
10. **Push**: Push to the branch (`git push origin feature/xxx`).
11. **PR**: Open a merge request on GitHub (on the GitHub webpage) with detailed description.

Please ensure tests pass and include relevant documentation updates.

---

## 🌟 Support

Welcome to contribute to this project via submitting merge requests and reporting issues.

**Project Support:**

- ⭐ **Give GitHub stars** if this project helps you
- 🤝 **Share with teammates** and (golang) programming friends
- 📝 **Write tech blogs** about development tools and workflows - we provide content writing support
- 🌟 **Join the ecosystem** - committed to supporting open source and the (golang) development scene

**Have Fun Coding with this package!** 🎉🎉🎉

<!-- TEMPLATE (EN) CLOSE: STANDARD PROJECT FOOTER -->
