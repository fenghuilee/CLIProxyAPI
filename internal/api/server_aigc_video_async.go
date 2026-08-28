package api

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

func (s *Server) aigcCreateVideoHandler(c *gin.Context) {
	s.handleAIGCAsyncCreate(c, aigc.ContentKindVideo)
}

func (s *Server) aigcGetVideoHandler(c *gin.Context) {
	s.handleAIGCAsyncGet(c)
}

func (s *Server) aigcGetVideoContentHandler(c *gin.Context) {
	s.handleAIGCAsyncGetContent(c)
}

func (s *Server) aigcCancelVideoHandler(c *gin.Context) {
	s.handleAIGCAsyncCancel(c)
}
