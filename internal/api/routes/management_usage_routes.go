package routes

import (
	"github.com/gin-gonic/gin"
	managementhandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

func registerManagementUsageRoutes(group *gin.RouterGroup, h *managementhandlers.Handler) {
	group.GET("/usage", h.GetUsageStatistics)
	group.GET("/usage/export", h.ExportUsageStatistics)
	group.POST("/usage/import", h.ImportUsageStatistics)
	group.GET("/usage/logs", h.GetUsageLogs)
	group.DELETE("/usage/logs", h.DeleteUsageLogs)
	group.GET("/usage/logs/:id/content", h.GetLogContent)
	group.GET("/usage/logs/:id/egress", h.GetUsageLogEgress)
	group.GET("/usage/auth-file-group-trend", h.GetAuthFileGroupTrend)
	group.GET("/usage/auth-file-trend", h.GetAuthFileTrend)
	group.POST("/usage/auth-file-quota-snapshot", h.PostAuthFileQuotaSnapshot)
	group.GET("/usage/chart-data", h.GetUsageChartData)
	group.GET("/usage/entity-stats", h.GetEntityUsageStats)
}
