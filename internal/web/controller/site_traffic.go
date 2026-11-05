package controller

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func (a *ServerController) getSiteTrafficHistory(c *gin.Context) {
	bucket, ok := parseHistoryBucket(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	site := c.Query("site")
	if len(site) > 512 {
		c.JSON(400, gin.H{"success": false, "msg": "查询目标过长"})
		return
	}
	history, err := service.GetSiteTrafficHistory(ctx, bucket, site)
	if err != nil {
		logger.Warning("网站流量历史读取失败: ", err)
		c.JSON(503, gin.H{"success": false, "msg": "网站流量数据暂不可用，请稍后重试"})
		return
	}
	c.Header("Cache-Control", "no-store")
	jsonObj(c, history, nil)
}
