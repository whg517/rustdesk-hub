package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

func (h *api) clientSetup(ctx context.Context) (*rustdesk.Setup, error) {
	settings, err := h.store.RustDeskSettings(ctx)
	if err != nil {
		return nil, err
	}
	return settings.ClientSetup(h.publicURL)
}

func (h *api) getClientSetup(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := h.store.CurrentUser(ctx, browserCredential(c)); err != nil {
		return err
	}
	setup, err := h.clientSetup(ctx)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, struct {
		RustDesk *rustdesk.Setup `json:"rustdesk,omitempty"`
	}{RustDesk: setup})
}

func (h *api) getRustDeskSettings(c *echo.Context) error {
	settings, err := h.store.AdminRustDeskSettings(c.Request().Context(), browserCredential(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, settings)
}

func (h *api) putRustDeskSettings(c *echo.Context) error {
	credential := browserCredential(c)
	if err := h.checkWebWrite(c, credential); err != nil {
		return err
	}
	var request hub.RustDeskSettings
	if err := readJSON(c, &request); err != nil {
		return err
	}
	settings, err := h.store.SaveRustDeskSettings(c.Request().Context(), credential, request)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, settings)
}
