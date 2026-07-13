package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	managementhandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

func registerIdentityAuthRoutes(engine *gin.Engine, h *managementhandlers.Handler, availability gin.HandlerFunc) {
	middlewares := []gin.HandlerFunc{managementSecurityHeaders()}
	if availability != nil {
		middlewares = append([]gin.HandlerFunc{availability}, middlewares...)
	}
	middlewares = append(middlewares, bodyutil.LimitBodyMiddleware(bodyutil.ManagementBodyLimit))
	auth := engine.Group("/v0/auth")
	auth.Use(middlewares...)
	auth.POST("/login", h.PostLogin)
	auth.POST("/logout", h.PostLogout)
	auth.GET("/me", h.GetMe)
	auth.PUT("/password", h.PutPassword)
}

func registerManagementIdentityRoutes(group *gin.RouterGroup, h *managementhandlers.Handler) {
	group.GET("/tenants", h.GetTenants)
	group.POST("/tenants", h.PostTenant)
	group.PATCH("/tenants/:id", h.PatchTenant)
	group.DELETE("/tenants/:id", h.DeleteTenant)

	group.GET("/users", h.GetUsers)
	group.POST("/users", h.PostUser)
	group.PATCH("/users/:id", h.PatchUser)
	group.DELETE("/users/:id", h.DeleteUser)
	group.PUT("/users/:id/roles", h.PutUserRoles)
	group.POST("/users/:id/reset-password", h.PostUserResetPassword)

	group.GET("/menus", h.GetMenus)
	group.POST("/menus", h.PostMenu)
	group.PATCH("/menus/:code", h.PatchMenu)
	group.DELETE("/menus/:code", h.DeleteMenu)
	group.GET("/permissions", h.GetPermissions)
	group.GET("/roles", h.GetRoles)
	group.POST("/roles", h.PostRole)
	group.PUT("/roles/:id/permissions", h.PutRolePermissions)
	group.PUT("/roles/:id/users", h.PutRoleUsers)
	group.DELETE("/roles/:id", h.DeleteRole)
	group.GET("/audit-logs", h.GetAuditLogs)
	group.GET("/audit-logs/:id", h.GetAuditLog)
	group.DELETE("/audit-logs/:id", h.DeleteAuditLog)
}
