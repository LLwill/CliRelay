// Package xai implements thinking configuration for xAI Grok Responses API models.
package xai

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking/provider/codex"
)

type Applier struct {
	codex.Applier
}

var _ thinking.ProviderApplier = (*Applier)(nil)

func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("xai", NewApplier())
}
