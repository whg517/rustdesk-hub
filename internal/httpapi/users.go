package httpapi

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/whg517/rustdesk-hub/internal/hub"
)

type webUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
	Disabled bool   `json:"disabled"`
}

func userResponse(user hub.User) webUser {
	return webUser{
		ID: strconv.FormatUint(uint64(user.ID), 10), Username: user.Name,
		IsAdmin: user.IsAdmin, Disabled: user.Disabled,
	}
}

func (h *api) listUsers(c *echo.Context) error {
	users, err := h.store.ListUsers(c.Request().Context(), browserCredential(c))
	if err != nil {
		return err
	}
	result := make([]webUser, 0, len(users))
	for _, user := range users {
		result = append(result, userResponse(user))
	}
	return c.JSON(http.StatusOK, result)
}

func (h *api) createUser(c *echo.Context) error {
	credential := browserCredential(c)
	if err := h.checkWebWrite(c, credential); err != nil {
		return err
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	user, err := h.store.CreateUser(c.Request().Context(), credential, request.Username, request.Password)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, userResponse(user))
}

func userID(c *echo.Context) (uint, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return 0, hub.InputError("用户 ID 无效")
	}
	return uint(id), nil
}

func (h *api) setUserStatus(c *echo.Context) error {
	credential := browserCredential(c)
	if err := h.checkWebWrite(c, credential); err != nil {
		return err
	}
	id, err := userID(c)
	if err != nil {
		return err
	}
	var request struct {
		Disabled *bool `json:"disabled"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	if request.Disabled == nil {
		return hub.InputError("请选择账号状态")
	}
	if err := h.store.SetUserDisabled(c.Request().Context(), credential, id, *request.Disabled); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *api) resetUserPassword(c *echo.Context) error {
	credential := browserCredential(c)
	if err := h.checkWebWrite(c, credential); err != nil {
		return err
	}
	id, err := userID(c)
	if err != nil {
		return err
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	if err := h.store.ResetUserPassword(c.Request().Context(), credential, id, request.Password); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
