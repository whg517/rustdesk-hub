package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/whg517/rustdesk-hub/internal/hub"
)

type nativeUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Status      int    `json:"status"`
	IsAdmin     bool   `json:"is_admin"`
}

func clientUser(name string) nativeUser {
	return nativeUser{Name: name, DisplayName: name, Status: 1}
}

func (h *api) registerNative(g *echo.Group) {
	limit := middleware.BodyLimit(maxRequestBytes)
	g.GET("/login-options", func(c *echo.Context) error { return c.JSON(http.StatusOK, []string{}) }, limit)
	g.POST("/login", h.nativeLogin, h.audit(hub.Native, "login"))
	g.POST("/currentUser", h.nativeCurrentUser, h.audit(hub.Native, "session.read"), discardRequestBody)
	g.POST("/logout", h.nativeLogout, h.audit(hub.Native, "logout"), discardRequestBody)

	read := h.audit(hub.Native, "addressBook.read")
	write := h.audit(hub.Native, "addressBook.write")
	nativeBook := func(next bookHandler) echo.HandlerFunc {
		return h.bookEntry(hub.Native, func(c *echo.Context) string { return c.Param("guid") }, next)
	}

	g.POST("/ab/personal", h.nativePersonal, read, discardRequestBody)
	g.POST("/ab/settings", h.nativeSettings, read, discardRequestBody)
	g.POST("/ab/shared/profiles", h.nativeEmptyPage, read, discardRequestBody)
	g.POST("/ab/peers", h.bookEntry(hub.Native, func(c *echo.Context) string { return c.QueryParam("ab") }, h.listPeers), read, discardRequestBody)
	g.POST("/ab/tags/:guid", nativeBook(h.listTags), read, discardRequestBody)
	g.POST("/ab/peer/add/:guid", nativeBook(h.addPeer), write)
	g.PUT("/ab/peer/update/:guid", nativeBook(h.updatePeer), write)
	g.DELETE("/ab/peer/:guid", nativeBook(h.deletePeers), write)
	g.POST("/ab/tag/add/:guid", nativeBook(h.addTag), write)
	g.PUT("/ab/tag/rename/:guid", nativeBook(h.renameNativeTag), write)
	g.PUT("/ab/tag/update/:guid", nativeBook(h.updateNativeTagColor), write)
	g.DELETE("/ab/tag/:guid", nativeBook(h.deleteTags), write)

	for _, path := range []string{"/device-group/accessible", "/users", "/peers"} {
		g.GET(path, h.nativeEmptyPage, limit)
	}
	backgroundLimit := h.throttle(h.backgroundLimit)
	g.POST("/heartbeat", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, struct{}{})
	}, limit, backgroundLimit, discardRequestBody)
	for _, path := range []string{"/sysinfo", "/audit/conn", "/audit/file", "/audit/alarm"} {
		g.POST(path, func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		}, limit, backgroundLimit, discardRequestBody)
	}
}

func (h *api) nativeLogin(c *echo.Context) error {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Type     string `json:"type"`
	}
	// Parse the native JSON body independently of its Content-Type header.
	if err := readJSON(c, &request); err != nil {
		return err
	}
	if request.Type != "account" {
		return hub.InputError("此服务使用账号和密码登录")
	}
	if !h.loginLimit.allow(loginSource(c.Request(), h.behindProxy)) {
		return rateLimited(c)
	}
	login, err := h.store.Login(c.Request().Context(), request.Username, request.Password, hub.Native)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, struct {
		Type  string     `json:"type"`
		Token string     `json:"access_token"`
		User  nativeUser `json:"user"`
	}{Type: "access_token", Token: login.Token, User: clientUser(login.User.Name)})
}

func (h *api) nativeCurrentUser(c *echo.Context) error {
	user, err := h.store.CurrentUser(c.Request().Context(), bearer(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, clientUser(user.Name))
}

func (h *api) nativeLogout(c *echo.Context) error {
	if err := h.store.Logout(c.Request().Context(), bearer(c)); err != nil {
		return err
	}
	return c.NoContent(http.StatusOK)
}

func (h *api) nativePersonal(c *echo.Context) error {
	guid, err := h.store.PersonalGUID(c.Request().Context(), bearer(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"guid": guid})
}

func (h *api) nativeSettings(c *echo.Context) error {
	if _, err := h.store.CurrentUser(c.Request().Context(), bearer(c)); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int{"max_peer_one_ab": 0})
}

func (h *api) nativeEmptyPage(c *echo.Context) error {
	if _, err := h.store.CurrentUser(c.Request().Context(), bearer(c)); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, peerPageResponse{Peers: []peerResponse{}})
}
