package main

import (
	"net/http"
	"sync"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/pkg/errors"
)

// Plugin implements the interface expected by the Mattermost server.
type Plugin struct {
	plugin.MattermostPlugin

	configurationLock sync.RWMutex
	configuration     *Configuration
}

func (p *Plugin) OnActivate() error {
	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		p.API.LogWarn("Plugin configuration invalid, some features may be unavailable", "error", err.Error())
	}

	if err := p.registerCommands(); err != nil {
		return errors.Wrap(err, "failed to register commands")
	}

	return nil
}

func (p *Plugin) OnDeactivate() error {
	return nil
}

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/action/diff":
		p.handleDiffAction(w, r)
	default:
		http.NotFound(w, r)
	}
}
