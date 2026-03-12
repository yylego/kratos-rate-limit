[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/kratos-rate-limit/release.yml?branch=main&label=BUILD)](https://github.com/yylego/kratos-rate-limit/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/kratos-rate-limit)](https://pkg.go.dev/github.com/yylego/kratos-rate-limit)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/kratos-rate-limit/main.svg)](https://coveralls.io/github/yylego/kratos-rate-limit?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.25+-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/kratos-rate-limit.svg)](https://github.com/yylego/kratos-rate-limit/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/kratos-rate-limit)](https://goreportcard.com/report/github.com/yylego/kratos-rate-limit)

# kratos-rate-limit

Distributed rate limiting middleware backed with Redis persistence.

---

<!-- TEMPLATE (EN) BEGIN: LANGUAGE NAVIGATION -->

## CHINESE README

[中文说明](README.zh.md)

<!-- TEMPLATE (EN) END: LANGUAGE NAVIGATION -->

## Main Features

🚦 **Distributed Rate Limiting**: Redis-backed rate limiting across instances
🔑 **Context-Based ID**: Extract unique identification from request context
📊 **Configurable Limits**: Use `redis_rate.Limit` with fine-grained throttling
🗺️ **Route Scope**: INCLUDE/EXCLUDE mode to choose which routes get rate-limited
📡 **APM Tracing**: Span hook integration via `authkratos.SpanHook`

## Installation

```bash
go get github.com/yylego/kratos-rate-limit
```

## Usage

```go
import "github.com/yylego/kratos-rate-limit/ratekratoslimits"

cfg := ratekratoslimits.NewConfig(
    routeScope,
    redisRateLimiter,
    &redis_rate.Limit{Rate: 10, Burst: 20, Period: time.Minute},
    func(ctx context.Context) (string, bool) {
        // extract unique identification from context
        return getUserID(ctx), true
    },
).WithDebugMode(true)
mw := ratekratoslimits.NewMiddleware(cfg, logger)
```

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

<!-- TEMPLATE (EN) END: STANDARD PROJECT FOOTER -->

---

## GitHub Stars

[![Stargazers](https://starchart.cc/yylego/kratos-rate-limit.svg?variant=adaptive)](https://starchart.cc/yylego/kratos-rate-limit)
