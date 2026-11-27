package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"golang.org/x/time/rate"

	"github.com/whg517/rustdesk-hub/internal/hub"
)

const (
	webPrefix       = "/api/v1"
	maxRequestBytes = 1 << 20
)

type Config struct {
	PublicURL   string
	WebFS       fs.FS
	QueryOnline func(context.Context, string, []string) ([]bool, error)
	BehindProxy bool
}

type api struct {
	store           *hub.Store
	queryOnline     func(context.Context, string, []string) ([]bool, error)
	publicURL       string
	secureCookies   bool
	behindProxy     bool
	loginLimit      *keyedRateLimit
	passwordLimit   *keyedRateLimit
	backgroundLimit *rate.Limiter
}

func New(store *hub.Store, config Config) *echo.Echo {
	h := &api{
		store:           store,
		queryOnline:     config.QueryOnline,
		publicURL:       config.PublicURL,
		secureCookies:   strings.HasPrefix(config.PublicURL, "https://"),
		behindProxy:     config.BehindProxy,
		loginLimit:      newKeyedRateLimit(),
		passwordLimit:   newKeyedRateLimit(),
		backgroundLimit: rate.NewLimiter(20, 40),
	}
	if h.queryOnline == nil {
		h.queryOnline = queryOnlineServer
	}
	e := echo.New()
	e.HTTPErrorHandler = handleError
	e.Use(responseHeaders, middleware.Recover())
	e.GET("/healthz", func(c *echo.Context) error {
		if err := h.store.Ping(c.Request().Context()); err != nil {
			return apiError{http.StatusServiceUnavailable, "Server.DatabaseUnavailable", "数据库暂不可用"}
		}
		return c.NoContent(http.StatusOK)
	})

	h.registerNative(e.Group("/api"))
	h.registerWeb(e.Group(webPrefix))
	if config.WebFS != nil {
		e.FileFS("/", "index.html", config.WebFS)
		e.StaticFS("/assets", echo.MustSubFS(config.WebFS, "assets"), cacheWebAsset)
	}
	return e
}

func cacheWebAsset(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		header := c.Response().Header()
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
		if err := next(c); err != nil {
			header.Del("Cache-Control")
			return err
		}
		return nil
	}
}

func responseHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		header := c.Response().Header()
		header.Set("X-Request-ID", rand.Text())
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		path := c.Request().URL.Path
		if path == "/" || path == "/healthz" || strings.HasPrefix(path, "/api/") {
			header.Set("Cache-Control", "no-store")
		}
		return next(c)
	}
}

type apiError struct {
	status  int
	code    string
	message string
}

func (e apiError) Error() string { return e.message }

func describeError(err error) apiError {
	var typed apiError
	var input hub.InputError
	switch {
	case errors.As(err, &typed):
		return typed
	case errors.As(err, &input):
		return apiError{http.StatusBadRequest, "Request.Invalid", string(input)}
	case errors.Is(err, hub.ErrCredentials):
		return apiError{http.StatusUnauthorized, "Account.InvalidCredentials", "账号或密码错误"}
	case errors.Is(err, hub.ErrSession):
		return apiError{http.StatusUnauthorized, "Session.Invalid", "会话无效或已过期，请重新登录"}
	case errors.Is(err, hub.ErrAdminRequired):
		return apiError{http.StatusForbidden, "Account.AdminRequired", "此操作需要管理员权限"}
	case errors.Is(err, hub.ErrUserExists):
		return apiError{http.StatusConflict, "Account.Exists", "该账号名已存在"}
	case errors.Is(err, hub.ErrUserNotFound):
		return apiError{http.StatusNotFound, "Account.NotFound", "账号不存在，请刷新后核对"}
	case errors.Is(err, hub.ErrNotFound):
		return apiError{http.StatusNotFound, "AddressBook.NotFound", "通讯录或条目不存在，请刷新后核对"}
	case errors.Is(err, hub.ErrTagExists):
		return apiError{http.StatusConflict, "AddressBook.TagExists", "该标签名称已存在"}
	case errors.Is(err, hub.ErrCredentialSync):
		return apiError{http.StatusUnprocessableEntity, "AddressBook.CredentialSyncDisabled", "设备凭据同步未启用"}
	}
	switch echo.StatusCode(err) {
	case http.StatusRequestEntityTooLarge:
		return apiError{http.StatusRequestEntityTooLarge, "Request.TooLarge", "请求体过大"}
	case http.StatusNotFound:
		return apiError{http.StatusNotFound, "Request.NotFound", "请求的资源不存在"}
	case http.StatusMethodNotAllowed:
		return apiError{http.StatusMethodNotAllowed, "Request.MethodNotAllowed", "此接口不支持该请求方法"}
	default:
		return apiError{http.StatusInternalServerError, "Server.Internal", "服务暂不可用，请稍后重试"}
	}
}

func handleError(c *echo.Context, err error) {
	if response, _ := echo.UnwrapResponse(c.Response()); response != nil && response.Committed {
		return
	}
	e := describeError(err)
	requestID := c.Response().Header().Get("X-Request-ID")
	if e.status >= 500 {
		slog.Error("request failed", "requestId", requestID, "route", c.Path(), "errorType", e.code)
	}
	if strings.HasPrefix(c.Request().URL.Path, webPrefix+"/") {
		_ = c.JSON(e.status, map[string]string{"code": e.code, "message": e.message, "requestId": requestID})
		return
	}
	_ = c.JSON(e.status, map[string]string{"error": e.message})
}

func readJSON(c *echo.Context, destination any) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return requestBodyError(err)
	}
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) || json.Unmarshal(body, destination) != nil {
		return hub.InputError("请求体须为有效的 JSON")
	}
	return nil
}

func requestBodyError(err error) error {
	if echo.StatusCode(err) != 0 {
		return err
	}
	return hub.InputError("无法读取请求体")
}

func discardRequestBody(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if _, err := io.Copy(io.Discard, c.Request().Body); err != nil {
			return requestBodyError(err)
		}
		return next(c)
	}
}

func bearer(c *echo.Context) hub.Credential {
	fields := strings.Fields(c.Request().Header.Get("Authorization"))
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return hub.Credential{Token: fields[1], Source: hub.Native}
	}
	return hub.Credential{Source: hub.Native}
}

func (h *api) throttle(limit *rate.Limiter) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !limit.Allow() {
				return rateLimited(c)
			}
			return next(c)
		}
	}
}

func (h *api) audit(source hub.Source, operation string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		limited := middleware.BodyLimit(maxRequestBytes)(next)
		return func(c *echo.Context) error {
			err := limited(c)
			status := http.StatusOK
			var code string
			if err != nil {
				reason := describeError(err)
				status, code = reason.status, reason.code
			} else if response, _ := echo.UnwrapResponse(c.Response()); response != nil {
				status = response.Status
			}

			result := hub.Diagnostic{
				Time: time.Now().UTC(), Source: source, Operation: operation, Status: status, ErrorCode: code,
				RequestID: c.Response().Header().Get("X-Request-ID"),
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request().Context()), time.Second)
			defer cancel()
			if recordErr := h.store.RecordDiagnostic(ctx, result); recordErr != nil {
				slog.Warn("diagnostic write failed", "requestId", result.RequestID)
			}
			return err
		}
	}
}
