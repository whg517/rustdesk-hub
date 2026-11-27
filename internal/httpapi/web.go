package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

const (
	sessionCookie = "rustdesk_hub_session"
	csrfHeader    = "X-CSRF-Token"
)

type webSession struct {
	Username  string          `json:"username"`
	IsAdmin   bool            `json:"isAdmin"`
	APIURL    string          `json:"apiUrl"`
	CSRFToken string          `json:"csrfToken"`
	RustDesk  *rustdesk.Setup `json:"rustdesk,omitempty"`
}

func (h *api) registerWeb(g *echo.Group) {
	g.POST("/session", h.webLogin, h.audit(hub.Web, "login"))
	limit := middleware.BodyLimit(maxRequestBytes)
	g.GET("/session", h.webCurrentUser, h.audit(hub.Web, "session.read"), discardRequestBody)
	g.DELETE("/session", h.webLogout, h.audit(hub.Web, "logout"), discardRequestBody)
	g.PUT("/account/password", h.changePassword, h.audit(hub.Web, "password.change"))
	g.GET("/diagnostics", h.webDiagnostics, bindWebRead, limit)
	g.GET("/client-setup", h.getClientSetup, bindWebRead, limit, discardRequestBody)
	g.GET("/system-settings/rustdesk", h.getRustDeskSettings, bindWebRead, limit, discardRequestBody)
	g.PUT("/system-settings/rustdesk", h.putRustDeskSettings, h.audit(hub.Web, "settings.rustdesk"))
	g.GET("/users", h.listUsers, bindWebRead, limit, discardRequestBody)
	g.POST("/users", h.createUser, h.audit(hub.Web, "user.create"))
	g.PATCH("/users/:id", h.setUserStatus, h.audit(hub.Web, "user.status"))
	g.PUT("/users/:id/password", h.resetUserPassword, h.audit(hub.Web, "user.password"))

	read := h.audit(hub.Web, "addressBook.read")
	write := h.audit(hub.Web, "addressBook.write")
	webBook := func(next bookHandler) echo.HandlerFunc {
		return h.bookEntry(hub.Web, nil, next)
	}

	g.GET("/address-book", webBook(h.listPeers), read, bindWebRead, discardRequestBody)
	g.GET("/address-book/online-status", webBook(h.onlineStatus), bindWebRead, limit, discardRequestBody)
	g.POST("/address-book/peers", webBook(h.addPeer), write)
	g.PATCH("/address-book/peers", webBook(h.updatePeer), write)
	g.DELETE("/address-book/peers", webBook(h.deletePeers), write)
	g.POST("/address-book/tags", webBook(h.addTag), write)
	g.PATCH("/address-book/tags", webBook(h.updateWebTag), write)
	g.DELETE("/address-book/tags", webBook(h.deleteWebTag), write)
}

func browserCredential(c *echo.Context) hub.Credential {
	credential := hub.Credential{Source: hub.Web}
	if cookie, err := c.Cookie(sessionCookie); err == nil {
		credential.Token = cookie.Value
	}
	return credential
}

func csrfToken(token string) string {
	digest := sha256.Sum256([]byte("rustdesk-hub:csrf:" + token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func matchesCSRF(token, proof string) bool {
	return subtle.ConstantTimeCompare([]byte(proof), []byte(csrfToken(token))) == 1
}

// The initial session response gives each page the proof used by subsequent reads.
func bindWebRead(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		credential := browserCredential(c)
		if credential.Token == "" {
			return hub.ErrSession
		}
		proof := c.Request().Header.Get(csrfHeader)
		if proof == "" {
			return apiError{http.StatusConflict, "Session.ReloadRequired", "请重新加载页面后继续"}
		}
		if !matchesCSRF(credential.Token, proof) {
			return apiError{http.StatusUnauthorized, "Session.Changed", "浏览器中的账号已切换，正在重新加载"}
		}
		return next(c)
	}
}

func (h *api) checkOrigin(c *echo.Context) error {
	origin, err := CanonicalOrigin(c.Request().Header.Get("Origin"))
	if err != nil || origin != h.publicURL {
		return apiError{http.StatusForbidden, "Request.OriginRejected", "请使用配置的管理端地址提交操作"}
	}
	return nil
}

func (h *api) checkWebWrite(c *echo.Context, credential hub.Credential) error {
	if err := h.checkOrigin(c); err != nil {
		return err
	}
	if credential.Token == "" {
		return hub.ErrSession
	}
	if !matchesCSRF(credential.Token, c.Request().Header.Get(csrfHeader)) {
		return apiError{http.StatusForbidden, "Request.CSRFRejected", "请求校验失败，请刷新页面后重试"}
	}
	return nil
}

func (h *api) sessionResponse(ctx context.Context, user hub.User, token string) (webSession, error) {
	setup, err := h.clientSetup(ctx)
	if err != nil {
		return webSession{}, err
	}
	return webSession{
		Username: user.Name, IsAdmin: user.IsAdmin, APIURL: h.publicURL,
		CSRFToken: csrfToken(token), RustDesk: setup,
	}, nil
}

func (h *api) setSession(c *echo.Context, token string, expiry time.Time) {
	cookie := &http.Cookie{
		Name: sessionCookie, Value: token, Expires: expiry, Path: webPrefix,
		Secure: h.secureCookies, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
	if token == "" {
		cookie.MaxAge = -1
	}
	c.SetCookie(cookie)
}

func (h *api) webLogin(c *echo.Context) error {
	if err := h.checkOrigin(c); err != nil {
		return err
	}
	mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return apiError{http.StatusUnsupportedMediaType, "Request.UnsupportedMediaType", "请发送 JSON 登录请求"}
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	if !h.loginLimit.allow(loginSource(c.Request(), h.behindProxy)) {
		return rateLimited(c)
	}
	ctx := c.Request().Context()
	login, err := h.store.Login(ctx, request.Username, request.Password, hub.Web)
	if err != nil {
		return err
	}
	session, err := h.sessionResponse(ctx, login.User, login.Token)
	if err != nil {
		return err
	}
	h.setSession(c, login.Token, login.ExpiresAt)
	return c.JSON(http.StatusOK, session)
}

func (h *api) webCurrentUser(c *echo.Context) error {
	credential := browserCredential(c)
	ctx := c.Request().Context()
	user, err := h.store.CurrentUser(ctx, credential)
	if err != nil {
		return err
	}
	session, err := h.sessionResponse(ctx, user, credential.Token)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}

func (h *api) webLogout(c *echo.Context) error {
	credential := browserCredential(c)
	if err := h.checkWebWrite(c, credential); err != nil {
		return err
	}
	if err := h.store.Logout(c.Request().Context(), credential); err != nil {
		return err
	}
	h.setSession(c, "", time.Unix(0, 0))
	return c.NoContent(http.StatusNoContent)
}

func (h *api) changePassword(c *echo.Context) error {
	credential := browserCredential(c)
	if err := h.checkWebWrite(c, credential); err != nil {
		return err
	}
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	ctx := c.Request().Context()
	user, err := h.store.CurrentUser(ctx, credential)
	if err != nil {
		return err
	}
	if !h.passwordLimit.allow(strconv.FormatUint(uint64(user.ID), 10)) {
		return rateLimited(c)
	}
	if err := h.store.ChangePassword(ctx, credential, request.CurrentPassword, request.NewPassword); err != nil {
		return err
	}
	h.setSession(c, "", time.Unix(0, 0))
	return c.NoContent(http.StatusNoContent)
}

func (h *api) webDiagnostics(c *echo.Context) error {
	rows, err := h.store.RecentDiagnostics(c.Request().Context(), browserCredential(c))
	if err != nil {
		return err
	}
	type entry struct {
		Time      time.Time  `json:"time"`
		Source    hub.Source `json:"source"`
		Operation string     `json:"operation"`
		Status    int        `json:"status"`
		ErrorCode string     `json:"errorCode,omitempty"`
		RequestID string     `json:"requestId"`
	}
	results := make([]entry, 0, len(rows))
	for _, row := range rows {
		results = append(results, entry{
			Time: row.Time, Source: row.Source, Operation: row.Operation, Status: row.Status, ErrorCode: row.ErrorCode, RequestID: row.RequestID,
		})
	}
	return c.JSON(http.StatusOK, results)
}
