package api

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

func (s *Server) aigcCreateImageHandler(c *gin.Context) {
	s.handleAIGCAsyncCreate(c, aigc.ContentKindImage)
}

func (s *Server) aigcGetImageHandler(c *gin.Context) {
	s.handleAIGCAsyncGet(c)
}

func (s *Server) aigcGetImageContentHandler(c *gin.Context) {
	s.handleAIGCAsyncGetContent(c)
}

func (s *Server) aigcCancelImageHandler(c *gin.Context) {
	s.handleAIGCAsyncCancel(c)
}
